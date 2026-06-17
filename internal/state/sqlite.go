package state

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/oklog/ulid/v2"
)

// initSQL is the schema migration applied on store creation.
// This mirrors migrations/001_init.sql kept in the repository root for
// external tooling (e.g. CLI migration commands).
const initSQL = `
CREATE TABLE IF NOT EXISTS requirements (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stories (
    id TEXT PRIMARY KEY,
    req_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    complexity INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    agent_id TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
    merged_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    runtime TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'idle',
    current_story_id TEXT NOT NULL DEFAULT '',
    session_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS story_deps (
    story_id TEXT NOT NULL,
    depends_on_id TEXT NOT NULL,
    PRIMARY KEY (story_id, depends_on_id)
);

CREATE TABLE IF NOT EXISTS escalations (
    id TEXT PRIMARY KEY,
    story_id TEXT NOT NULL DEFAULT '',
    from_agent TEXT NOT NULL,
    reason TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    resolution TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS agent_scores (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    story_id TEXT NOT NULL,
    quality INTEGER NOT NULL DEFAULT 0,
    reliability INTEGER NOT NULL DEFAULT 0,
    duration_s INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS story_databases (
    story_id          TEXT NOT NULL,
    db_id             TEXT NOT NULL,
    db_name           TEXT NOT NULL,
    provider          TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,
    template          TEXT NOT NULL DEFAULT '',
    conn_string_hash  TEXT NOT NULL DEFAULT '',
    error             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at        TIMESTAMP,
    duration_seconds  REAL DEFAULT 0,
    bytes_used        INTEGER DEFAULT 0,
    PRIMARY KEY (story_id, db_id)
);
CREATE INDEX IF NOT EXISTS idx_story_databases_status ON story_databases(status);
`

// SQLiteStore implements ProjectionStore using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens a SQLite database and applies the schema migration.
func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err := db.Exec(initSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply migration: %w", err)
	}

	// Enable WAL mode for concurrent read/write access. The monitor's
	// pipeline goroutines write concurrently; without WAL, SQLite uses
	// journal mode which returns "database is locked" under contention.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("[sqlite] enable WAL mode: %v (continuing in default journal mode)", err)
	}

	// SCHEMA EVOLUTION:
	//
	// The base CREATE TABLE statements at the top of this file run with
	// IF NOT EXISTS, so they're effectively a no-op on an existing DB.
	// Every column added AFTER an initial release MUST also appear in
	// the `migrations` slice below as an idempotent `ALTER TABLE ADD
	// COLUMN` statement so existing installs pick it up on next open.
	//
	// tryMigrate distinguishes the expected "duplicate column" errors
	// (benign — column already exists from a prior run) from real
	// failures (disk full, locked DB, perms) and logs the latter at
	// WARNING. This is intentionally lightweight rather than a full
	// version-tracking migration runner: the column-add pattern composes
	// naturally with SQLite, and version-table tracking would be more
	// machinery than the schema needs today.
	migrations := []string{
		`ALTER TABLE stories ADD COLUMN acceptance_criteria TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE stories ADD COLUMN owned_files TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE stories ADD COLUMN wave_hint TEXT NOT NULL DEFAULT 'parallel'`,
		`ALTER TABLE requirements ADD COLUMN repo_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE stories ADD COLUMN wave INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE stories ADD COLUMN pr_number INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE stories ADD COLUMN merged_at TIMESTAMP`,
		`ALTER TABLE stories ADD COLUMN escalation_tier INTEGER DEFAULT 0`,
		`ALTER TABLE stories ADD COLUMN split_depth INTEGER DEFAULT 0`,
		`ALTER TABLE escalations ADD COLUMN from_tier INTEGER DEFAULT 0`,
		`ALTER TABLE escalations ADD COLUMN to_tier INTEGER DEFAULT 0`,
		`ALTER TABLE requirements ADD COLUMN review_mode TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE requirements ADD COLUMN estimated_hours_low REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE requirements ADD COLUMN estimated_hours_high REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE requirements ADD COLUMN estimated_cost_low REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE requirements ADD COLUMN estimated_cost_high REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE requirements ADD COLUMN recovered_at TIMESTAMP`,
	}
	for _, m := range migrations {
		tryMigrate(db, m)
	}

	indexStatements := []string{
		`CREATE INDEX IF NOT EXISTS idx_stories_req_id ON stories(req_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stories_status ON stories(status)`,
		`CREATE INDEX IF NOT EXISTS idx_story_deps_story_id ON story_deps(story_id)`,
		`CREATE INDEX IF NOT EXISTS idx_escalations_story_id ON escalations(story_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_scores_agent_id ON agent_scores(agent_id)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return nil, fmt.Errorf("create index: %w", err)
		}
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Project applies a domain event to the projection tables, updating the
// materialized state accordingly.
func (s *SQLiteStore) Project(evt Event) error {
	payload := s.decodePayload(evt)

	switch evt.Type {
	case EventReqSubmitted:
		return s.projectReqSubmitted(payload)
	case EventReqAnalyzed:
		return s.updateReqStatus(payload, "analyzed")
	case EventReqPlanned:
		return s.updateReqStatus(payload, "planned")
	case EventReqPaused:
		return s.updateReqStatus(payload, "paused")
	case EventReqResumed:
		return s.updateReqStatus(payload, "planned")
	case EventReqCompleted:
		return s.updateReqStatus(payload, "completed")
	case EventReqEstimated:
		return s.projectReqEstimated(payload)
	case EventPlanRejected:
		return s.updateReqStatusByReqID(payload, "plan_rejected")
	case EventReviewModeSet:
		return s.projectReviewModeSet(payload)
	case EventRecoveryCompleted:
		return s.projectRecoveryCompleted(evt)

	case EventStoryCreated:
		return s.projectStoryCreated(payload)
	case EventStoryEstimated:
		return s.updateStoryStatus(evt.StoryID, "estimated")
	case EventStoryAssigned:
		return s.projectStoryAssigned(evt.StoryID, payload)
	case EventStoryStarted:
		return s.guardedStartStory(evt.StoryID)
	case EventStoryProgress:
		return nil // progress events are informational only
	case EventStoryCompleted:
		return s.updateStoryStatus(evt.StoryID, "review")
	case EventStoryReviewRequested:
		return s.updateStoryStatus(evt.StoryID, "review")
	case EventStoryReviewPassed:
		return s.updateStoryStatus(evt.StoryID, "qa")
	case EventStoryReviewFailed:
		return s.updateStoryStatus(evt.StoryID, "draft")
	case EventStoryQAStarted:
		return s.updateStoryStatus(evt.StoryID, "qa")
	case EventStoryQAPassed:
		return s.updateStoryStatus(evt.StoryID, "pr_submitted")
	case EventStoryQAFailed:
		return s.updateStoryStatus(evt.StoryID, "draft")
	case EventStoryPRCreated:
		return s.projectStoryPRCreated(evt.StoryID, payload)
	case EventStoryMerged:
		if _, err := s.db.Exec(
			`UPDATE stories SET status = 'merged', merged_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			evt.Timestamp, evt.StoryID,
		); err != nil {
			return err
		}
		return nil

	case EventStoryAwaitingApproval:
		return s.updateStoryStatus(evt.StoryID, "awaiting_approval")
	case EventStoryApproved:
		return s.updateStoryApproved(evt.StoryID)
	case EventStoryRejected:
		return s.updateStoryStatus(evt.StoryID, "draft")
	case EventStoryReset:
		return s.updateStoryStatus(evt.StoryID, "draft")

	case EventStoryEscalated:
		return s.projectStoryEscalated(evt, payload)
	case EventStoryRewritten:
		return s.projectStoryRewritten(evt.StoryID, payload)
	case EventStorySplit:
		return s.updateStoryStatus(evt.StoryID, "split")

	case EventStoryDBCreated:
		return s.projectStoryDBCreated(evt, payload)
	case EventStoryDBFailed:
		return s.projectStoryDBFailed(evt, payload)
	case EventStoryDBDeleted:
		return s.projectStoryDBDeleted(evt, payload)

	case EventStorySLABreached:
		return nil // observational only — no projection change needed

	case EventAgentSpawned:
		return s.projectAgentSpawned(evt, payload)
	case EventAgentTerminated:
		return s.projectAgentTerminated(evt, payload)
	case EventAgentCheckpoint, EventAgentResumed, EventAgentStuck, EventPlanApproved:
		return nil // informational — no projection change

	case EventBranchDeleted:
		return nil // informational — no projection change needed
	case EventGCCompleted:
		return nil // informational — no projection change needed
	case EventWorktreePruned:
		return nil // informational — no projection change needed
	case EventSupervisorCheck:
		return nil // informational — no projection change needed
	case EventSupervisorDriftDetected:
		return nil // informational — no projection change needed

	case EventStoryConflictBinary, EventStoryConflictBinaryRemoved, EventStoryConflictEscalated:
		// Conflict resolution events are informational; they are read from the
		// event log for diagnostics but require no projection change.
		return nil

	case EventStoryIntegrationFailed:
		// Integration build failures are persisted in the event log for diagnostics.
		// No projection change needed — the story status is already "merged".
		return nil

	case EventReqPlanningStarted:
		// Planning heartbeat — informational only, no projection change.
		return nil

	case EventPipelineStalled:
		// Pipeline-stall signal — read from the event log by external monitors
		// (e.g. Hermes cron). Observational only, no projection change. Explicit
		// case keeps it off the default-WARNING branch.
		return nil

	case EventBaselineMeasured,
		EventExperimentProposed,
		EventExperimentRunning,
		EventExperimentMeasured,
		EventExperimentTiebroken,
		EventExperimentTripwired,
		EventExperimentKept,
		EventExperimentDiscarded,
		EventExperimentFailed,
		EventCoordinatorPanic,
		EventProgrammdEvolved:
		// Autoresearch events are read directly from the event log by the
		// HypothesisBank/BayesSampler; no SQLite projection is needed in v1.
		// Explicit case ensures we don't trip the default-WARNING branch.
		return nil

	default:
		log.Printf("[projector] WARNING: unhandled event type %q (story=%s)", evt.Type, evt.StoryID)
		return nil
	}
}

// GetStory returns a single story by ID.
func (s *SQLiteStore) GetStory(id string) (Story, error) {
	var story Story
	var ownedFilesJSON string
	var mergedAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, req_id, title, description, acceptance_criteria, complexity, status, agent_id, branch, pr_url, pr_number, owned_files, wave_hint, wave, escalation_tier, split_depth, created_at, merged_at
		 FROM stories WHERE id = ?`,
		id,
	).Scan(
		&story.ID, &story.ReqID, &story.Title, &story.Description,
		&story.AcceptanceCriteria, &story.Complexity, &story.Status, &story.AgentID, &story.Branch,
		&story.PRUrl, &story.PRNumber, &ownedFilesJSON, &story.WaveHint, &story.Wave,
		&story.EscalationTier, &story.SplitDepth, &story.CreatedAt, &mergedAt,
	)
	if err != nil {
		return Story{}, fmt.Errorf("get story %s: %w", id, err)
	}
	if mergedAt.Valid {
		story.MergedAt = mergedAt.Time
	}
	if ownedFilesJSON != "" {
		if uerr := json.Unmarshal([]byte(ownedFilesJSON), &story.OwnedFiles); uerr != nil {
			// Corrupt owned_files leaves the dispatcher blind to file
			// ownership → potential races between parallel stories. Surface
			// the row + JSON so operators can rebuild from events.jsonl.
			log.Printf("[projector] unmarshal owned_files for story %s: %v (raw=%q)", id, uerr, ownedFilesJSON)
		}
	}
	if story.OwnedFiles == nil {
		story.OwnedFiles = []string{}
	}
	return story, nil
}

// ListStories returns stories matching the given filter.
func (s *SQLiteStore) ListStories(filter StoryFilter) ([]Story, error) {
	query := `SELECT id, req_id, title, description, acceptance_criteria, complexity, status, agent_id, branch, pr_url, pr_number, owned_files, wave_hint, wave, escalation_tier, split_depth, created_at, merged_at FROM stories`
	var conditions []string
	var args []any

	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.ReqID != "" {
		conditions = append(conditions, "req_id = ?")
		args = append(args, filter.ReqID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	defer rows.Close()

	var stories []Story
	for rows.Next() {
		var story Story
		var ownedFilesJSON string
		var mergedAt sql.NullTime
		if err := rows.Scan(
			&story.ID, &story.ReqID, &story.Title, &story.Description,
			&story.AcceptanceCriteria, &story.Complexity, &story.Status, &story.AgentID, &story.Branch,
			&story.PRUrl, &story.PRNumber, &ownedFilesJSON, &story.WaveHint, &story.Wave,
			&story.EscalationTier, &story.SplitDepth, &story.CreatedAt, &mergedAt,
		); err != nil {
			return nil, fmt.Errorf("scan story: %w", err)
		}
		if mergedAt.Valid {
			story.MergedAt = mergedAt.Time
		}
		if ownedFilesJSON != "" {
			if uerr := json.Unmarshal([]byte(ownedFilesJSON), &story.OwnedFiles); uerr != nil {
				log.Printf("[projector] unmarshal owned_files for story %s: %v (raw=%q)", story.ID, uerr, ownedFilesJSON)
			}
		}
		if story.OwnedFiles == nil {
			story.OwnedFiles = []string{}
		}
		stories = append(stories, story)
	}
	return stories, rows.Err()
}

// AgentFilter specifies criteria for filtering agents.
type AgentFilter struct {
	Status string
}

// ListAgents returns agents matching the given filter, ordered by creation time.
func (s *SQLiteStore) ListAgents(filter AgentFilter) ([]Agent, error) {
	query := `SELECT id, type, model, runtime, status, current_story_id, session_name, created_at FROM agents`
	var args []any

	if filter.Status != "" {
		query += " WHERE status = ?"
		args = append(args, filter.Status)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(
			&a.ID, &a.Type, &a.Model, &a.Runtime,
			&a.Status, &a.CurrentStoryID, &a.SessionName, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// Escalation represents a recorded escalation between agent roles.
type Escalation struct {
	ID         string `json:"id"`
	StoryID    string `json:"story_id"`
	FromAgent  string `json:"from_agent"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
	FromTier   int    `json:"from_tier"`
	ToTier     int    `json:"to_tier"`
	CreatedAt  string `json:"created_at"`
}

// ListEscalations returns all escalations ordered by creation time descending.
func (s *SQLiteStore) ListEscalations() ([]Escalation, error) {
	rows, err := s.db.Query(
		`SELECT id, story_id, from_agent, reason, status, resolution, from_tier, to_tier, created_at
		 FROM escalations ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list escalations: %w", err)
	}
	defer rows.Close()

	var escalations []Escalation
	for rows.Next() {
		var e Escalation
		if err := rows.Scan(
			&e.ID, &e.StoryID, &e.FromAgent, &e.Reason,
			&e.Status, &e.Resolution, &e.FromTier, &e.ToTier, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan escalation: %w", err)
		}
		escalations = append(escalations, e)
	}
	return escalations, rows.Err()
}

// StoryDep represents a dependency edge between stories.
type StoryDep struct {
	StoryID     string
	DependsOnID string
}

// ListStoryDeps returns all dependency edges for stories belonging to the given requirement.
func (s *SQLiteStore) ListStoryDeps(reqID string) ([]StoryDep, error) {
	rows, err := s.db.Query(
		`SELECT sd.story_id, sd.depends_on_id
		 FROM story_deps sd
		 JOIN stories s ON sd.story_id = s.id
		 WHERE s.req_id = ?`, reqID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []StoryDep
	for rows.Next() {
		var d StoryDep
		if err := rows.Scan(&d.StoryID, &d.DependsOnID); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}

// tryMigrate runs an idempotent ALTER TABLE statement and distinguishes
// "duplicate column" errors (expected on a previously-migrated DB) from
// real failures (disk full, locked DB, perms). Real failures are logged
// at WARNING so operators see the gap before the projection diverges.
func tryMigrate(db *sql.DB, stmt string) {
	if _, err := db.Exec(stmt); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "duplicate column") {
			return // benign — column already added by a previous run
		}
		log.Printf("[sqlite] migration %q failed: %v", stmt, err)
	}
}

// --- private helpers ---

func (s *SQLiteStore) decodePayload(evt Event) map[string]any {
	if evt.Payload == nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(evt.Payload, &m); err != nil {
		// A corrupt payload silently fell through as an empty map; downstream
		// projections then read empty strings for required fields like
		// req_id / title and wrote partial rows. Log the corruption so
		// operators see the event type + story ID instead of just an
		// empty projection.
		log.Printf("[projector] decode payload for %s/%s: %v", evt.Type, evt.StoryID, err)
		return map[string]any{}
	}
	return m
}

func (s *SQLiteStore) projectReqSubmitted(payload map[string]any) error {
	// INSERT OR IGNORE makes duplicate REQ_SUBMITTED events idempotent:
	// if a requirement with the same id already exists (e.g., from a replay
	// or a double-emit bug), the second event is silently ignored rather than
	// returning a unique-constraint error that would surface as a projection
	// failure.
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO requirements (id, title, description, status, repo_path) VALUES (?, ?, ?, 'pending', ?)`,
		payloadStr(payload, "id"),
		payloadStr(payload, "title"),
		payloadStr(payload, "description"),
		payloadStr(payload, "repo_path"),
	)
	return err
}

func (s *SQLiteStore) updateReqStatus(payload map[string]any, status string) error {
	id := payloadStr(payload, "id")
	_, err := s.db.Exec(
		`UPDATE requirements SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, id,
	)
	return err
}

func (s *SQLiteStore) projectStoryCreated(payload map[string]any) error {
	complexity := payloadInt(payload, "complexity")
	storyID := payloadStr(payload, "id")

	ownedFilesJSON := "[]"
	if of, ok := payload["owned_files"]; ok {
		if b, err := json.Marshal(of); err == nil {
			ownedFilesJSON = string(b)
		}
	}

	waveHint := payloadStr(payload, "wave_hint")
	if waveHint == "" {
		waveHint = "parallel"
	}

	splitDepth := payloadInt(payload, "split_depth")

	_, err := s.db.Exec(
		`INSERT INTO stories (id, req_id, title, description, acceptance_criteria, complexity, status, owned_files, wave_hint, split_depth)
		 VALUES (?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?)`,
		storyID,
		payloadStr(payload, "req_id"),
		payloadStr(payload, "title"),
		payloadStr(payload, "description"),
		payloadStr(payload, "acceptance_criteria"),
		complexity,
		ownedFilesJSON,
		waveHint,
		splitDepth,
	)
	if err != nil {
		return err
	}

	// Populate story_deps table
	if deps, ok := payload["depends_on"]; ok {
		if depSlice, ok := deps.([]any); ok {
			for _, dep := range depSlice {
				if depStr, ok := dep.(string); ok && depStr != "" {
					_, err := s.db.Exec(
						`INSERT OR IGNORE INTO story_deps (story_id, depends_on_id) VALUES (?, ?)`,
						storyID, depStr,
					)
					if err != nil {
						return fmt.Errorf("insert story dep %s -> %s: %w", storyID, depStr, err)
					}
				}
			}
		}
	}
	return nil
}

func (s *SQLiteStore) projectStoryAssigned(storyID string, payload map[string]any) error {
	agentID := payloadStr(payload, "agent_id")
	branch := payloadStr(payload, "branch")
	wave := payloadInt(payload, "wave")
	_, err := s.db.Exec(
		`UPDATE stories SET status = 'assigned', agent_id = ?, branch = ?, wave = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		agentID, branch, wave, storyID,
	)
	return err
}

func (s *SQLiteStore) projectStoryPRCreated(storyID string, payload map[string]any) error {
	prNumber := payloadInt(payload, "pr_number")
	prURL := payloadStr(payload, "pr_url")
	_, err := s.db.Exec(
		`UPDATE stories SET status = 'pr_submitted', pr_url = ?, pr_number = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		prURL, prNumber, storyID,
	)
	return err
}

func (s *SQLiteStore) updateStoryStatus(storyID, status string) error {
	_, err := s.db.Exec(
		`UPDATE stories SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status, storyID,
	)
	return err
}

// guardedStartStory transitions a story to "in_progress" only when its current
// status is not already terminal. Emitting STORY_STARTED for a story that is
// already merged, awaiting approval, split, or pr_submitted indicates a bug in
// the dispatcher (e.g., the auto-resume loop fixed in PR #40). Rejecting such
// transitions here surfaces the bug instead of silently corrupting state.
func (s *SQLiteStore) guardedStartStory(storyID string) error {
	var currentStatus string
	err := s.db.QueryRow(`SELECT status FROM stories WHERE id = ?`, storyID).Scan(&currentStatus)
	if err != nil {
		// Story not found yet — allow the transition so that
		// STORY_STARTED events replayed before STORY_CREATED don't error.
		return s.updateStoryStatus(storyID, "in_progress")
	}
	if IsStoryComplete(currentStatus) {
		log.Printf("[projection] rejecting STORY_STARTED for %s: current status is terminal (%s) — likely a dispatcher bug", storyID, currentStatus)
		return nil // no state regression; silently drop the transition
	}
	return s.updateStoryStatus(storyID, "in_progress")
}

func (s *SQLiteStore) updateStoryApproved(storyID string) error {
	_, err := s.db.Exec(
		`UPDATE stories SET status = 'approved', updated_at = CURRENT_TIMESTAMP WHERE id = ? AND status != 'merged'`,
		storyID,
	)
	return err
}

// BackfillAcceptanceCriteria updates stories that have an empty
// acceptance_criteria by extracting it from STORY_CREATED events.
// This handles databases created before the column was added.
func (s *SQLiteStore) BackfillAcceptanceCriteria(events []Event) {
	for _, evt := range events {
		if evt.Type != EventStoryCreated || evt.Payload == nil {
			continue
		}
		payload := s.decodePayload(evt)
		ac := payloadStr(payload, "acceptance_criteria")
		storyID := payloadStr(payload, "id")
		if ac != "" && storyID != "" {
			if _, err := s.db.Exec(
				`UPDATE stories SET acceptance_criteria = ? WHERE id = ? AND acceptance_criteria = ''`,
				ac, storyID,
			); err != nil {
				log.Printf("[sqlite] backfill acceptance_criteria for %s: %v", storyID, err)
			}
		}
	}
}

// ArchiveRequirement sets a requirement's status to "archived".
func (s *SQLiteStore) ArchiveRequirement(reqID string) error {
	_, err := s.db.Exec(
		`UPDATE requirements SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		reqID,
	)
	return err
}

// ArchiveStoriesByReq sets all stories for a given requirement to "archived".
func (s *SQLiteStore) ArchiveStoriesByReq(reqID string) error {
	_, err := s.db.Exec(
		`UPDATE stories SET status = 'archived', updated_at = CURRENT_TIMESTAMP WHERE req_id = ?`,
		reqID,
	)
	return err
}

func (s *SQLiteStore) projectStoryEscalated(evt Event, payload map[string]any) error {
	fromTier := payloadInt(payload, "from_tier")
	toTier := payloadInt(payload, "to_tier")
	reason := payloadStr(payload, "reason")

	if _, err := s.db.Exec(
		`UPDATE stories SET escalation_tier = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		toTier, evt.StoryID,
	); err != nil {
		return fmt.Errorf("update story escalation_tier: %w", err)
	}

	id := ulid.MustNew(ulid.Timestamp(evt.Timestamp), rand.Reader)
	_, err := s.db.Exec(
		`INSERT INTO escalations (id, story_id, from_agent, reason, status, from_tier, to_tier, created_at)
		 VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
		id.String(), evt.StoryID, evt.AgentID, reason, fromTier, toTier, evt.Timestamp,
	)
	return err
}

func (s *SQLiteStore) projectStoryRewritten(storyID string, payload map[string]any) error {
	changes := payloadMap(payload, "changes")

	if title, ok := changes["title"].(string); ok && title != "" {
		if _, err := s.db.Exec(`UPDATE stories SET title = ? WHERE id = ?`, title, storyID); err != nil {
			return fmt.Errorf("update story title: %w", err)
		}
	}
	if desc, ok := changes["description"].(string); ok && desc != "" {
		if _, err := s.db.Exec(`UPDATE stories SET description = ? WHERE id = ?`, desc, storyID); err != nil {
			return fmt.Errorf("update story description: %w", err)
		}
	}
	if ac, ok := changes["acceptance_criteria"].(string); ok && ac != "" {
		if _, err := s.db.Exec(`UPDATE stories SET acceptance_criteria = ? WHERE id = ?`, ac, storyID); err != nil {
			return fmt.Errorf("update story acceptance_criteria: %w", err)
		}
	}
	if complexity, ok := changes["complexity"]; ok {
		if c, ok := complexity.(float64); ok {
			if _, err := s.db.Exec(`UPDATE stories SET complexity = ? WHERE id = ?`, int(c), storyID); err != nil {
				return fmt.Errorf("update story complexity: %w", err)
			}
		} else if c, ok := complexity.(int); ok {
			if _, err := s.db.Exec(`UPDATE stories SET complexity = ? WHERE id = ?`, c, storyID); err != nil {
				return fmt.Errorf("update story complexity: %w", err)
			}
		}
	}

	_, err := s.db.Exec(
		`UPDATE stories SET escalation_tier = 0, status = 'draft', updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		storyID,
	)
	return err
}

// projectAgentSpawned inserts or updates an agent row when a new agent session is spawned.
func (s *SQLiteStore) projectAgentSpawned(evt Event, payload map[string]any) error {
	agentID := evt.AgentID
	if agentID == "" {
		agentID = payloadStr(payload, "agent_id")
	}
	role := payloadStr(payload, "role")
	sessionName := payloadStr(payload, "session_name")
	runtimeName := payloadStr(payload, "runtime")

	_, err := s.db.Exec(
		`INSERT INTO agents (id, type, model, runtime, status, current_story_id, session_name, created_at)
		 VALUES (?, ?, '', ?, 'active', ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET status='active', current_story_id=?, session_name=?, runtime=?`,
		agentID, role, runtimeName, evt.StoryID, sessionName, evt.Timestamp,
		evt.StoryID, sessionName, runtimeName,
	)
	return err
}

// projectAgentTerminated marks an agent as terminated.
func (s *SQLiteStore) projectAgentTerminated(evt Event, payload map[string]any) error {
	agentID := evt.AgentID
	if agentID == "" {
		agentID = payloadStr(payload, "agent_id")
	}
	_, err := s.db.Exec(
		`UPDATE agents SET status = 'terminated' WHERE id = ?`,
		agentID,
	)
	return err
}

func (s *SQLiteStore) projectStoryDBCreated(evt Event, payload map[string]any) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO story_databases
		 (story_id, db_id, db_name, provider, status, template, conn_string_hash, created_at)
		 VALUES (?, ?, ?, ?, 'created', ?, ?, ?)`,
		evt.StoryID,
		payloadStr(payload, "db_id"),
		payloadStr(payload, "db_name"),
		payloadStr(payload, "provider"),
		payloadStr(payload, "template"),
		payloadStr(payload, "conn_string_hash"),
		evt.Timestamp,
	)
	return err
}

func (s *SQLiteStore) projectStoryDBFailed(evt Event, payload map[string]any) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO story_databases
		 (story_id, db_id, db_name, provider, status, error, created_at)
		 VALUES (?, ?, ?, ?, 'failed', ?, ?)`,
		evt.StoryID,
		payloadStr(payload, "db_id"),
		payloadStr(payload, "db_name"),
		payloadStr(payload, "provider"),
		payloadStr(payload, "error"),
		evt.Timestamp,
	)
	return err
}

func (s *SQLiteStore) projectStoryDBDeleted(evt Event, payload map[string]any) error {
	status := payloadStr(payload, "status")
	if status == "" {
		status = "deleted"
	}
	dur := payloadFloat(payload, "duration_seconds")
	bytes := payloadInt(payload, "bytes_used")
	_, err := s.db.Exec(
		`UPDATE story_databases
		 SET status = ?, deleted_at = ?, duration_seconds = ?, bytes_used = ?
		 WHERE story_id = ? AND db_id = ?`,
		status, evt.Timestamp, dur, bytes,
		evt.StoryID, payloadStr(payload, "db_id"),
	)
	return err
}

// StoryDBMetrics holds aggregated devdb stats for a single requirement.
type StoryDBMetrics struct {
	ReqID            string
	TotalDBs         int
	DeletedDBs       int
	RetainedDBs      int
	FailedDBs        int
	ActiveDBs        int
	TotalDurationSec float64 // sum of duration_seconds for deleted/retained DBs
	Provider         string  // last seen provider name (informational)
}

// StoryDBMetricsByReq returns aggregated DB metrics for the given requirement.
// It joins story_databases via the stories table to filter by req_id.
func (s *SQLiteStore) StoryDBMetricsByReq(reqID string) (StoryDBMetrics, error) {
	m := StoryDBMetrics{ReqID: reqID}
	rows, err := s.db.Query(`
		SELECT sd.status, COALESCE(sd.duration_seconds, 0), COALESCE(sd.provider, '')
		FROM story_databases sd
		JOIN stories st ON st.id = sd.story_id
		WHERE st.req_id = ?
	`, reqID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var status, provider string
		var dur float64
		if err := rows.Scan(&status, &dur, &provider); err != nil {
			return m, err
		}
		m.TotalDBs++
		switch status {
		case "deleted":
			m.DeletedDBs++
			m.TotalDurationSec += dur
		case "retained":
			m.RetainedDBs++
			m.TotalDurationSec += dur
		case "failed":
			m.FailedDBs++
		case "created":
			m.ActiveDBs++
		}
		if provider != "" {
			m.Provider = provider
		}
	}
	return m, rows.Err()
}

// StoryDBStatusByReq returns a map of story_id -> current db status for the
// given requirement. Story IDs missing from the map have no DB. Status values:
// "created" (active), "failed", "deleted", "retained".
func (s *SQLiteStore) StoryDBStatusByReq(reqID string) (map[string]string, error) {
	out := make(map[string]string)
	rows, err := s.db.Query(`
		SELECT sd.story_id, sd.status
		FROM story_databases sd
		JOIN stories st ON st.id = sd.story_id
		WHERE st.req_id = ?
	`, reqID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid, st string
		if err := rows.Scan(&sid, &st); err != nil {
			return nil, err
		}
		// If multiple rows per story (rare), the latest non-deleted wins.
		if existing, ok := out[sid]; ok && existing != "deleted" {
			continue
		}
		out[sid] = st
	}
	return out, rows.Err()
}

// StoryDBStatusAll returns the same map but unscoped to a requirement.
func (s *SQLiteStore) StoryDBStatusAll() (map[string]string, error) {
	out := make(map[string]string)
	rows, err := s.db.Query(`SELECT story_id, status FROM story_databases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid, st string
		if err := rows.Scan(&sid, &st); err != nil {
			return nil, err
		}
		if existing, ok := out[sid]; ok && existing != "deleted" {
			continue
		}
		out[sid] = st
	}
	return out, rows.Err()
}

// --- payload extraction helpers ---

func payloadStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func payloadInt(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func payloadMap(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return map[string]any{}
	}
	sub, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return sub
}
