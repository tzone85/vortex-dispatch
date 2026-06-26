// Package engine implements the VXD orchestration pipeline:
// Requirement -> Plan -> Dispatch -> Execute -> Review -> QA -> Merge -> Cleanup.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/tzone85/vortex-dispatch/internal/agent"
	"github.com/tzone85/vortex-dispatch/internal/config"
	vxdgit "github.com/tzone85/vortex-dispatch/internal/git"
	"github.com/tzone85/vortex-dispatch/internal/graph"
	"github.com/tzone85/vortex-dispatch/internal/llm"
	"github.com/tzone85/vortex-dispatch/internal/repolearn"
	"github.com/tzone85/vortex-dispatch/internal/sanitize"
	"github.com/tzone85/vortex-dispatch/internal/state"
)

// PlannedStory represents a story decomposed from a requirement by the Tech Lead.
type PlannedStory struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	AcceptanceCriteria FlexibleString `json:"acceptance_criteria"`
	Complexity         int            `json:"complexity"`
	DependsOn          []string       `json:"depends_on"`
	OwnedFiles         []string       `json:"owned_files"`
	WaveHint           string         `json:"wave_hint"`
}

// PlanResult holds the output of a planning session: stories and their
// dependency graph.
type PlanResult struct {
	RequirementID string
	Stories       []PlannedStory
	Graph         *graph.DAG
}

// Planner decomposes a requirement into implementable stories via the
// Tech Lead LLM and emits corresponding domain events.
type Planner struct {
	llmClient  llm.Client
	config     config.Config
	eventStore state.EventStore
	projStore  state.ProjectionStore
	projectDir string // path to project state dir (for loading RepoProfile)
}

// SetProjectDir sets the project state directory for loading RepoProfile.
func (p *Planner) SetProjectDir(dir string) {
	p.projectDir = dir
}

// NewPlanner creates a Planner wired to the given LLM client, configuration,
// event store, and projection store.
func NewPlanner(client llm.Client, cfg config.Config, es state.EventStore, ps state.ProjectionStore) *Planner {
	return &Planner{
		llmClient:  client,
		config:     cfg,
		eventStore: es,
		projStore:  ps,
	}
}

// Plan takes a requirement and produces decomposed stories with a dependency
// graph. It emits REQ_SUBMITTED, STORY_CREATED (per story), and REQ_PLANNED
// events.
func (p *Planner) Plan(ctx context.Context, reqID, requirement, repoPath string) (PlanResult, error) {
	return p.plan(ctx, reqID, requirement, repoPath, true)
}

// PlanEphemeral runs the same decomposition as Plan but persists nothing —
// no REQ_SUBMITTED, STORY_CREATED, or REQ_PLANNED events reach the event store
// or projection. It backs `vxd estimate`, which is a read-only quote: it must
// be re-runnable any number of times (a user correcting a typo'd requirement)
// without polluting project state or colliding on stories.id.
func (p *Planner) PlanEphemeral(ctx context.Context, reqID, requirement, repoPath string) (PlanResult, error) {
	return p.plan(ctx, reqID, requirement, repoPath, false)
}

// plan is the shared implementation. When persist is false every event-store
// and projection write is skipped, making the call a pure decomposition.
func (p *Planner) plan(ctx context.Context, reqID, requirement, repoPath string, persist bool) (PlanResult, error) {
	// Validate requirement before sending to LLM
	if sanitize.DetectPromptInjection(requirement) {
		return PlanResult{}, fmt.Errorf("requirement rejected: prompt injection detected")
	}
	if sanitize.ScanForSecrets(requirement) {
		return PlanResult{}, fmt.Errorf("requirement rejected: embedded secret detected — remove credentials before submitting")
	}

	// Emit requirement submitted
	if persist {
		reqPayload := map[string]any{
			"id":          reqID,
			"title":       requirement,
			"description": requirement,
			"repo_path":   repoPath,
		}
		if err := p.emitAndProject(state.EventReqSubmitted, "system", "", reqPayload); err != nil {
			return PlanResult{}, fmt.Errorf("emit req submitted: %w", err)
		}
	}

	// Scan repo for tech stack — prefer RepoProfile if available
	var techStackStr string
	var profileContext string
	if p.projectDir != "" {
		if profile, err := repolearn.LoadProfile(p.projectDir); err == nil && profile.TechStack.PrimaryLanguage != "" {
			techStackStr = fmt.Sprintf("%s (%s)", profile.TechStack.PrimaryLanguage, profile.TechStack.PrimaryBuildTool)
			profileContext = profile.Summary()
		}
	}
	if techStackStr == "" {
		stack := vxdgit.ScanRepo(repoPath)
		techStackStr = fmt.Sprintf("%s (%s)", stack.Language, stack.BuildTool)
	}

	// Build Tech Lead prompt
	promptCtx := agent.PromptContext{
		RepoPath:           repoPath,
		TechStack:          techStackStr,
		IsExistingCodebase: detectExistingCodebase(repoPath),
	}
	systemPrompt := agent.SystemPrompt(agent.RoleTechLead, promptCtx)

	userMessage := fmt.Sprintf(`Decompose this requirement into atomic, implementable stories:

Requirement: %s

Respond with a JSON array of stories. Each story must have:
- id: short identifier (e.g., "s-001")
- title: brief title
- description: what to implement, including exact file paths (e.g., "Create src/models/user.js")
- acceptance_criteria: how to verify it's done
- complexity: Fibonacci score (1, 2, 3, 5, 8, 13)
- depends_on: array of story IDs this depends on (empty if none)
- owned_files: array of exact file paths this story will create or modify (e.g., ["src/models/user.go", "src/models/user_test.go"])
- wave_hint: either "sequential" or "parallel" — use "sequential" for stories that modify shared config, lock files, or core infrastructure

IMPORTANT:
- Every story MUST produce code changes (new files or modifications to existing files). Do NOT create read-only "assessment" or "analysis" stories — agents that produce no code changes will be marked as failed.
- For existing codebases: skip scaffolding. The first story should write actual code.
- For new projects: the first story (s-001) should create the directory structure and initial files.
- All stories MUST reference specific file paths in their descriptions.
- Distribute work across different files to minimize merge conflicts between parallel agents.
- Each file path MUST appear in exactly ONE story's owned_files — no overlapping file ownership between stories.
- Use explicit relative paths from the project root (e.g., "src/api/handler.go", not just "handler.go").
- Keep story complexity at or below %d.
- For simple requirements (1-2 files), prefer fewer stories (1-2) over many small ones.

ENGINEERING STANDARDS — every code story's description AND acceptance_criteria MUST bake these in (this project is a software factory; output must be production-grade):
- Input validation: validate all inputs at system boundaries; reject malformed input with clear, specific errors; never trust external data (user input, files, API responses).
- Security: for any web/HTML/API/templating surface, prevent XSS (escape/encode all output, sanitize HTML) and injection (parameterized queries, no string-built SQL/commands); never reflect unsanitized input into responses or markup. State the specific protection in the acceptance criteria.
- SOLID + clean architecture: single-responsibility units, depend on interfaces not concretions, keep core/domain logic free of I/O (hexagonal core/shell split); files focused and small.
- Proper wiring: everything built must be reachable from a real entry point (CLI command, HTTP route, public export) — no dead/unreferenced code. Acceptance criteria must assert the wiring (e.g. "route registered and returns 200", "command appears in --help").
- Assembled-app verification (HTTP/API surfaces): a story delivering an endpoint is NOT done when unit tests pass with injected/overridden/mocked dependencies — that hides the most common production failure (route declared but never registered, a dependency stub never overridden by the composition root, two endpoints holding separate stores). Such a story's acceptance criteria MUST include an integration test that boots the REAL app via its production entry point / app factory with NO dependency overrides, asserts the endpoint is registered at its exact spec path (responds, not 404), and asserts data written through one endpoint is visible through another (shared store, not per-route state).
- Error handling: handle errors explicitly at every level; no silent failures; user-facing messages must not leak internals/stack traces.
- Tests: every code story includes tests covering the happy path AND at least one failure/edge path.
- Docs as you go: stories that add a public surface document it (in docs/ or the relevant module doc) so the final README/training can link real content.

CRITICAL RULES:
- Respond ONLY with the JSON array. No prose, no explanations, no questions.
- Do NOT ask clarification questions. Make reasonable assumptions and proceed.
- Do NOT wrap the JSON in markdown code fences.
- If you are unsure about something, make a decision and document it in the story description.
- Your response must start with [ and end with ].`, requirement, p.config.Planning.MaxStoryComplexity)

	// Append repo profile context if available from learning system
	if profileContext != "" {
		userMessage += fmt.Sprintf(`

Repository Profile (pre-analysed):
%s
Use this profile to inform your story decomposition. Reference the correct
build/test/lint commands in acceptance criteria. Account for the detected
architecture and conventions when planning stories.`, profileContext)
	}

	// Emit planning-started heartbeat so the operator sees progress while the
	// Tech Lead LLM call runs (typically 2-3 minutes for complex requirements).
	if persist {
		planningStarted := state.NewEvent(state.EventReqPlanningStarted, "tech-lead", "", map[string]any{
			"req_id": reqID,
			"model":  p.config.Models.TechLead.Model,
		})
		// Best-effort — failure here must not abort planning, but log it: an
		// unwritable store here means the real planning Append later will fail too,
		// so an early signal helps diagnose. Matches the post-bulletproofing
		// convention of never dropping an Append/Project error without a log.
		if err := p.eventStore.Append(planningStarted); err != nil {
			log.Printf("[planner] append planning-started heartbeat for %s: %v", reqID, err)
		}
		if err := p.projStore.Project(planningStarted); err != nil {
			log.Printf("[planner] project planning-started heartbeat for %s: %v", reqID, err)
		}
	}

	// Call Tech Lead
	resp, err := p.llmClient.Complete(ctx, llm.CompletionRequest{
		Model:     p.config.Models.TechLead.Model,
		MaxTokens: p.config.Models.TechLead.MaxTokens,
		System:    systemPrompt,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: userMessage}},
	})
	if err != nil {
		return PlanResult{}, fmt.Errorf("tech lead planning: %w", err)
	}

	// Parse stories from response (strip markdown fences if present)
	var stories []PlannedStory
	cleaned := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(cleaned), &stories); err != nil {
		return PlanResult{}, fmt.Errorf("parse stories: %w (response: %s)", err, resp.Content)
	}

	// A plan with zero stories is always a failure: the Tech Lead returned an
	// empty array (or a comment-only response). Without this guard the pipeline
	// would emit REQ_PLANNED with no STORY_CREATED events and strand the
	// requirement in "planned" forever after a paid LLM call.
	if len(stories) == 0 {
		return PlanResult{}, fmt.Errorf("tech lead returned no stories for requirement %q", reqID)
	}

	// Reject content-free stories at the LLM boundary — an empty id or title is
	// corruption that would otherwise dispatch an agent against nothing.
	for _, s := range stories {
		if strings.TrimSpace(s.ID) == "" {
			return PlanResult{}, fmt.Errorf("tech lead returned a story with an empty id")
		}
		if strings.TrimSpace(s.Title) == "" {
			return PlanResult{}, fmt.Errorf("tech lead returned story %q with an empty title", s.ID)
		}
	}

	// Make story IDs globally unique by prefixing with a short, collision-
	// resistant namespace derived from the req ID. LLMs always generate generic
	// IDs like "s-001" which collide across requirements.
	prefix := storyIDPrefix(reqID)
	idMap := make(map[string]string, len(stories))
	for i, s := range stories {
		// Reject duplicate story IDs — LLM hallucination would silently drop stories.
		if _, exists := idMap[s.ID]; exists {
			return PlanResult{}, fmt.Errorf("LLM returned duplicate story ID: %s", s.ID)
		}
		newID := prefix + "-" + s.ID
		idMap[s.ID] = newID
		stories[i].ID = newID
	}
	// Validate depends_on references before remapping — a nonexistent reference
	// creates a dangling DAG edge that makes the story permanently undispatchable.
	for _, s := range stories {
		for _, dep := range s.DependsOn {
			if _, ok := idMap[dep]; !ok {
				return PlanResult{}, fmt.Errorf("story %s has depends_on reference to nonexistent story %s", s.ID, dep)
			}
		}
	}
	for i, s := range stories {
		for j, dep := range s.DependsOn {
			if newDep, ok := idMap[dep]; ok {
				stories[i].DependsOn[j] = newDep
			}
		}
		// Normalize invalid wave_hint values to a safe default.
		switch s.WaveHint {
		case "", "sequential", "parallel":
			// valid — keep as-is
		default:
			stories[i].WaveHint = "parallel"
		}
	}

	// Validate story complexity
	for _, s := range stories {
		if p.config.Planning.MaxStoryComplexity > 0 && s.Complexity > p.config.Planning.MaxStoryComplexity {
			return PlanResult{}, fmt.Errorf("story %s complexity %d exceeds max %d", s.ID, s.Complexity, p.config.Planning.MaxStoryComplexity)
		}
	}

	// Resolve overlapping file ownership by adding dependency edges.
	// When two stories claim the same file, the second one gets a
	// dependency on the first so they run sequentially (not in parallel).
	fileOwner := make(map[string]string)
	for i := range stories {
		for _, f := range stories[i].OwnedFiles {
			if owner, exists := fileOwner[f]; exists && owner != stories[i].ID {
				// Add dependency: this story depends on the file owner.
				hasDep := false
				for _, d := range stories[i].DependsOn {
					if d == owner {
						hasDep = true
						break
					}
				}
				if !hasDep {
					stories[i].DependsOn = append(stories[i].DependsOn, owner)
					log.Printf("[planner] auto-sequenced %s after %s (shared file: %s)", stories[i].ID, owner, f)
				}
			}
			fileOwner[f] = stories[i].ID
		}
	}

	// Integration story: a final code story that depends on every other code
	// story, wires all independently-built components into the application
	// entry point, reconciles interface mismatches with adapters, and writes a
	// smoke test that BOOTS the app and asserts the documented surface actually
	// responds. This closes the systemic gap where per-story unit tests pass
	// (against mocks) but the whole never composes — unwired handlers, no auth,
	// incompatible interfaces. The smoke test runs in QA / the pre-merge gate,
	// so a non-functional whole fails the build instead of "completing".
	if persist && p.config.Planning.EmitIntegrationStory && len(stories) > 0 {
		deps := make([]string, 0, len(stories))
		for _, s := range stories {
			deps = append(deps, s.ID)
		}
		stories = append(stories, buildIntegrationStory(prefix, requirement, deps))
	}

	// README Scribe: append a final story that documents what was built. It
	// depends on every other story so it runs last (after all code is merged),
	// owns README.md, and is greenfield-aware. Skipped for ephemeral estimates
	// and when planning.emit_scribe_story is disabled.
	if persist && p.config.Planning.EmitScribeStory && len(stories) > 0 {
		deps := make([]string, 0, len(stories))
		for _, s := range stories {
			deps = append(deps, s.ID)
		}
		stories = append(stories, buildScribeStory(prefix, requirement, deps))
	}

	// Build dependency graph
	dag := graph.New()
	for _, s := range stories {
		dag.AddNode(s.ID)
	}
	for _, s := range stories {
		for _, dep := range s.DependsOn {
			dag.AddEdge(s.ID, dep)
		}
	}

	// Validate no cycles
	if _, err := dag.TopologicalSort(); err != nil {
		return PlanResult{}, fmt.Errorf("dependency cycle: %w", err)
	}

	// Emit events for each story (skipped for ephemeral estimate planning).
	if persist {
		for _, s := range stories {
			storyPayload := map[string]any{
				"id":                  s.ID,
				"req_id":              reqID,
				"title":               s.Title,
				"description":         s.Description,
				"acceptance_criteria": string(s.AcceptanceCriteria),
				"complexity":          s.Complexity,
				"depends_on":          s.DependsOn,
				"owned_files":         s.OwnedFiles,
				"wave_hint":           s.WaveHint,
			}
			if err := p.emitAndProject(state.EventStoryCreated, "tech-lead", s.ID, storyPayload); err != nil {
				return PlanResult{}, fmt.Errorf("emit story created %s: %w", s.ID, err)
			}
		}

		// Emit requirement planned
		if err := p.emitAndProject(state.EventReqPlanned, "tech-lead", "", map[string]any{
			"id": reqID,
		}); err != nil {
			return PlanResult{}, fmt.Errorf("emit req planned: %w", err)
		}
	}

	return PlanResult{
		RequirementID: reqID,
		Stories:       stories,
		Graph:         dag,
	}, nil
}

// RePlan takes a single failing story and its failure context, calls the LLM
// to decompose it into smaller replacement stories, emits STORY_CREATED
// events for each replacement, and returns them. Unlike Plan, it does NOT
// emit REQ_SUBMITTED or REQ_PLANNED events — the caller is responsible for
// emitting STORY_SPLIT and mutating the DAG.
func (p *Planner) RePlan(ctx context.Context, storyID, reqID, failureContext string) ([]PlannedStory, error) {
	// Look up the original requirement and story to give the re-planner full context.
	// Without this, the LLM hallucinates sub-stories unrelated to the project.
	var reqTitle, storyTitle, storyDesc string
	if req, err := p.projStore.GetRequirement(reqID); err == nil {
		reqTitle = req.Title
	}
	if story, err := p.projStore.GetStory(storyID); err == nil {
		storyTitle = story.Title
		storyDesc = story.Description
	}

	prompt := fmt.Sprintf(`A story has failed multiple times and needs re-planning.

Original Requirement: %s
Story ID: %s
Story Title: %s
Story Description: %s
Failure Context:
%s

CRITICAL CONSTRAINTS:
- Sub-stories MUST be directly related to the original requirement and story above.
- Do NOT introduce new features, tools, or concepts not mentioned in the requirement.
- Every sub-story MUST produce code changes (no read-only analysis stories).
- Sub-stories should be smaller pieces of the SAME work, not different work.

Decompose this into 2-3 smaller, more focused replacement stories.
Return a JSON array of story objects with fields: id, title, description, acceptance_criteria, complexity, owned_files.
Each story ID should use the parent ID as prefix with a letter suffix (e.g., "%s-a", "%s-b").
Keep complexity at or below 5.

Respond ONLY with the JSON array, no other text.`, reqTitle, storyID, storyTitle, storyDesc, failureContext, storyID, storyID)

	model := p.config.Models.TechLead
	resp, err := p.llmClient.Complete(ctx, llm.CompletionRequest{
		Model:     model.Model,
		MaxTokens: model.MaxTokens,
		System:    "You are a technical lead re-planning a failed story. Break it into smaller pieces of the SAME work. Stay strictly within the scope of the original requirement. Do NOT introduce unrelated features.",
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("replan LLM call: %w", err)
	}

	var stories []PlannedStory
	cleaned := extractJSON(resp.Content)
	if err := json.Unmarshal([]byte(cleaned), &stories); err != nil {
		return nil, fmt.Errorf("parse replan stories: %w (response: %s)", err, resp.Content)
	}

	// Validate sub-stories: reject empty results and warn about suspicious ones.
	if len(stories) == 0 {
		return nil, fmt.Errorf("replan produced no sub-stories")
	}

	// Filter out sub-stories with no owned_files AND no description
	// (likely hallucinated) or that fail lexical grounding against the
	// parent (also hallucinated — different topic entirely).
	parentAnchors := ExtractLexicalAnchors(reqTitle, storyTitle, storyDesc)
	var valid []PlannedStory
	for _, s := range stories {
		if len(s.OwnedFiles) == 0 && s.Description == "" {
			log.Printf("[replan] skipping empty sub-story %s: no owned_files or description", s.ID)
			continue
		}
		if !HasLexicalGrounding(parentAnchors, s.Title, s.Description) {
			log.Printf("[replan] skipping ungrounded sub-story %s: %q shares no meaningful tokens with parent context", s.ID, s.Title)
			continue
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("replan produced no valid sub-stories (all filtered — possible LLM hallucination)")
	}

	return valid, nil
}

// emitAndProject appends an event to the event store and projects it.
// storyIDPrefix returns a short, collision-resistant namespace prefix derived
// from the requirement ID. LLM-generated story IDs ("s-001") collide across
// requirements, so each is namespaced with this prefix.
//
// Short IDs (≤8 chars, e.g. test fixtures "r-001") are returned verbatim for
// readability. Longer IDs are hashed: blindly truncating to the first 8 chars
// dropped all the distinguishing entropy — a ULID's leading chars are only its
// millisecond-timestamp high bits (so two requirements within ~256ms collided),
// and every estimate reqID ("est-YYYYMMDD-...") truncated to a constant
// "est-2026", making each estimate after the first crash on stories.id.
func storyIDPrefix(reqID string) string {
	if len(reqID) <= 8 {
		return reqID
	}
	sum := sha256.Sum256([]byte(reqID))
	return hex.EncodeToString(sum[:])[:8]
}

// scribeStorySuffix is the stable, un-prefixed id of the README-scribe story.
const scribeStorySuffix = "scribe-readme"

// buildScribeStory constructs the final documentation story. It depends on every
// other story (deps are already prefixed), owns README.md, and instructs the
// agent to be greenfield-aware and confine edits on an existing README to the
// vxd:scribe markers so hand-written content is never clobbered.
// buildIntegrationStory constructs the final integration story. It runs after
// every code story (depends on all of them) and is responsible for making the
// independently-built components actually compose into a working application.
func buildIntegrationStory(prefix, requirement string, deps []string) PlannedStory {
	desc := fmt.Sprintf(`Integrate everything the other stories built into ONE working application for this requirement: %s

The other stories each built and unit-tested a component in isolation (often against mocks). Your job is to make the WHOLE thing actually run end-to-end. This is the most important story — a build that passes unit tests but does not run is a failure.

Do ALL of the following:
- Wire every component into the application entry point (e.g. main.go / app.py / server / index.ts / CLI root). Every handler, route, command, page, and middleware that a story built MUST be reachable from the real entry point. Audit for dangling wires: a feature whose unit test passes but that is never registered in the entry point is a bug.
- Apply cross-cutting middleware that stories built but could not wire themselves (authentication, logging, CORS, rate limiting) at the entry point. If an auth/API-key middleware exists, it MUST actually guard the documented protected routes.
- Reconcile interface mismatches between components with thin adapters. Independently-built stories often declare slightly different interfaces (e.g. one returns []Conflict, another *ValidationResult); add the adapter so the real implementation — not a mock — is wired in. Do NOT leave a production path depending on a test-only mock.
- Add a SMOKE TEST that boots the application and exercises the documented surface end-to-end: for a server, start it and assert each documented endpoint responds with the expected status (NOT 404) and that protected routes reject missing credentials; for a CLI, run the documented commands and assert real output; for a UI, render the primary flow. The smoke test must FAIL if a feature is unreachable or unwired.
- Fix any wiring/compile/type errors this integration surfaces so the full app builds and all tests (including your smoke test) pass.

Make reasonable, conventional choices for any route paths or wiring details the requirement leaves unspecified, and note them briefly in code comments.`, requirement)

	return PlannedStory{
		ID:                 prefix + "-integrate",
		Title:              "Integrate all components into a working app + end-to-end smoke test",
		Description:        desc,
		AcceptanceCriteria: FlexibleString("Every documented feature is reachable from the real application entry point (no dangling/unwired handlers, commands, or pages); cross-cutting middleware (auth etc.) actually guards the documented routes; interface mismatches between components are bridged with adapters so the production path uses real implementations, not mocks; a smoke test boots the app and asserts the documented surface responds end-to-end (protected routes reject missing credentials, documented endpoints do not 404) and that smoke test passes along with the full build/test suite."),
		Complexity:         5,
		DependsOn:          deps,
		// No declared owned_files: integration legitimately touches the entry
		// point (owned by the skeleton story) and adds adapter/smoke-test files.
		// It depends on every story, so it runs last with no parallel conflict;
		// the edge-aware overlap check permits the sequenced entry-point edit.
		OwnedFiles: []string{},
		WaveHint:   "sequential",
	}
}

func buildScribeStory(prefix, requirement string, deps []string) PlannedStory {
	desc := fmt.Sprintf(`Document the project to software-factory standard for what this requirement delivered: %s

Write for a reader who is new to the project. Deliver the COMPLETE software-factory documentation set:
- README.md: explain what it is, how to install/run it, and how to use it — accurate to what was actually built and merged (do not invent features). It is the entry point: link to docs/ (the docs index), the training guide, and the Architecture Decision Records.
- docs/training.md: a "Getting Started" step-by-step hands-on walkthrough that takes a new user from zero to a working result, with copy-pasteable commands and expected output.
- docs/architecture.svg: an architecture diagram authored as a real rendered SVG file (valid <svg>…</svg> XML). NOT Mermaid, NOT a code fence, NOT a .mmd file — an actual .svg.
- docs/sequence.svg: a sequence diagram of the primary user flow, also as a real rendered SVG file (valid <svg>…</svg> XML). NOT Mermaid.
- docs/adr/0001-*.md … : Architecture Decision Records for the significant, hard-to-reverse decisions (persistence, layering, offline-vs-network, auth, a key algorithm, an error-handling contract, etc.), each grounded in the real code with Status/Context/Decision/Consequences. Add docs/adr/README.md as an index.
- docs/README.md: a documentation index linking the README, training guide, both SVGs, and the ADRs.
- Reference both SVGs from the README (e.g. via ![Architecture](docs/architecture.svg)) and link docs/ + docs/adr/.
- Greenfield-aware: if README.md is empty or a bare stub, author a complete README. If it already has substantial hand-written content, edit ONLY inside the markers `+"`<!-- vxd:scribe:start -->`"+` ... `+"`<!-- vxd:scribe:end -->`"+` (create that block at the end if absent) — never rewrite or delete existing prose outside the markers. The docs/ files are new and may be authored freely.`, requirement)

	return PlannedStory{
		ID:                 prefix + "-" + scribeStorySuffix,
		Title:              "Document the project: README + training + SVG diagrams + ADRs + docs index",
		Description:        desc,
		AcceptanceCriteria: FlexibleString("README.md accurately documents the delivered functionality with install/run/usage instructions and links docs/, the training guide, and the ADRs; docs/training.md is a step-by-step Getting-Started tutorial with copy-pasteable commands; docs/architecture.svg and docs/sequence.svg exist as valid rendered SVG (<svg> XML, NOT Mermaid/code-fence/.mmd) and are referenced from the README; docs/adr/ contains Architecture Decision Records (Status/Context/Decision/Consequences) plus an index, grounded in the real code; docs/README.md is a documentation index; on a pre-existing README, edits are confined to the vxd:scribe markers and existing content outside them is unchanged."),
		Complexity:         5,
		DependsOn:          deps,
		OwnedFiles:         []string{"README.md", "docs/architecture.svg", "docs/sequence.svg", "docs/training.md", "docs/adr", "docs/README.md"},
		WaveHint:           "sequential",
	}
}

func (p *Planner) emitAndProject(eventType state.EventType, agentID, storyID string, payload map[string]any) error {
	evt := state.NewEvent(eventType, agentID, storyID, payload)
	if err := p.eventStore.Append(evt); err != nil {
		return err
	}
	return p.projStore.Project(evt)
}
