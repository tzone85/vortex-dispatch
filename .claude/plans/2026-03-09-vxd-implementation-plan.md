# VXD (Vortex Dispatch) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go CLI that orchestrates autonomous AI agents through a full SDLC pipeline — from requirement decomposition to merged PRs.

**Architecture:** Event-sourced core with Dolt/SQLite projections. Hybrid execution: API calls for planning/review, CLI sessions (Claude Code/Codex/Gemini) in tmux+worktrees for implementation. Full agile hierarchy with complexity-based routing.

**Tech Stack:** Go 1.23+, Cobra (CLI), Bubbletea (TUI), SQLite + Dolt (state), tmux (sessions), GitHub API (PRs)

---

## Phase 1: Project Scaffolding (Foundation)

### Task 1.1: Initialize Go Module and Directory Structure

**Files:**
- Create: `cmd/vxd/main.go`
- Create: `go.mod`
- Create: `Makefile`
- Create: `LICENSE`
- Create: `.gitignore`
- Create: `vxd.config.example.yaml`

**Step 1: Initialize Go module**

Run: `go mod init github.com/tzone85/vortex-dispatch`

**Step 2: Create directory skeleton**

Run:
```bash
mkdir -p cmd/vxd
mkdir -p internal/{cli,engine,agent,runtime,state,graph,git,tmux,llm,config,dashboard,web}
mkdir -p migrations formulas scripts testdata
```

**Step 3: Write minimal main.go**

```go
// cmd/vxd/main.go
package main

import (
	"fmt"
	"os"

	"github.com/tzone85/vortex-dispatch/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

**Step 4: Write root CLI command**

```go
// internal/cli/root.go
package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "vxd",
	Short: "Vortex Dispatch — AI agent orchestrator",
	Long:  "VXD orchestrates autonomous AI agents through the full software development lifecycle.",
}

func Execute() error {
	return rootCmd.Execute()
}
```

**Step 5: Install cobra dependency and verify build**

Run: `go get github.com/spf13/cobra@latest && go build ./cmd/vxd/`
Expected: Binary compiles with no errors

**Step 6: Create Makefile**

```makefile
.PHONY: build test lint clean

BINARY=vxd
VERSION?=0.1.0

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/vxd/

test:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY) coverage.out

install: build
	mv $(BINARY) $(GOPATH)/bin/
```

**Step 7: Create .gitignore**

```
vxd
*.out
coverage.out
.vxd/
*.db
node_modules/
dist/
.DS_Store
.env
```

**Step 8: Create LICENSE (Apache 2.0)**

Use standard Apache 2.0 text with `Copyright 2026 tzone85`.

**Step 9: Create example config**

```yaml
# vxd.config.example.yaml
version: "1.0"

workspace:
  state_dir: ~/.vxd
  backend: dolt    # or "sqlite"
  log_level: info
  log_retention_days: 30

models:
  tech_lead:
    provider: anthropic
    model: claude-opus-4-20250514
    max_tokens: 16000
  senior:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  intermediate:
    provider: anthropic
    model: claude-haiku-4-5-20251001
    max_tokens: 4000
  junior:
    provider: openai
    model: gpt-4o-mini
    max_tokens: 4000
  qa:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 8000
  supervisor:
    provider: anthropic
    model: claude-sonnet-4-20250514
    max_tokens: 4000

routing:
  junior_max_complexity: 3
  intermediate_max_complexity: 5
  max_retries_before_escalation: 2
  max_qa_failures_before_escalation: 3

monitor:
  poll_interval_ms: 10000
  stuck_threshold_s: 120
  context_freshness_tokens: 150000

cleanup:
  worktree_prune: immediate
  branch_retention_days: 7
  log_archive: dolt

merge:
  auto_merge: true
  base_branch: main
  pr_template: |
    ## Story: {story_id}
    {description}
    ### Acceptance Criteria
    {acceptance_criteria}

runtimes:
  claude-code:
    command: claude
    args: ["--dangerously-skip-permissions"]
    models: ["opus-4", "sonnet-4", "haiku-4"]
    detection:
      idle_pattern: "^\\$\\s*$"
      permission_pattern: "\\[Y/n\\]"
      plan_mode_pattern: "Plan mode"
  codex:
    command: codex
    args: ["--approval-mode", "full-auto"]
    models: ["o3", "o4-mini"]
    detection:
      idle_pattern: "Codex>"
      permission_pattern: "approve|deny"
  gemini:
    command: gemini
    args: ["--sandbox"]
    models: ["gemini-2.5-pro", "gemini-2.5-flash"]
    detection:
      idle_pattern: "gemini>"
      permission_pattern: "Allow|Deny"
```

**Step 10: Commit**

```bash
git add -A
git commit -m "chore: scaffold VXD project structure with Go module, CLI entry point, and config example"
```

---

## Phase 2: Event Store & State Layer

### Task 2.1: Event Type Definitions

**Files:**
- Create: `internal/state/events.go`
- Test: `internal/state/events_test.go`

**Step 1: Write the failing test**

```go
// internal/state/events_test.go
package state_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestNewEvent(t *testing.T) {
	evt := state.NewEvent(state.EventStoryCreated, "agent-1", "story-1", map[string]any{
		"title":      "Add auth",
		"complexity": 5,
	})

	if evt.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if evt.Type != state.EventStoryCreated {
		t.Fatalf("expected type %s, got %s", state.EventStoryCreated, evt.Type)
	}
	if evt.AgentID != "agent-1" {
		t.Fatalf("expected agent-1, got %s", evt.AgentID)
	}
	if evt.StoryID != "story-1" {
		t.Fatalf("expected story-1, got %s", evt.StoryID)
	}
	if time.Since(evt.Timestamp) > time.Second {
		t.Fatal("timestamp should be recent")
	}

	var payload map[string]any
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["title"] != "Add auth" {
		t.Fatalf("expected title 'Add auth', got %v", payload["title"])
	}
}

func TestEventTypeConstants(t *testing.T) {
	types := []state.EventType{
		state.EventReqSubmitted,
		state.EventReqPlanned,
		state.EventReqCompleted,
		state.EventStoryCreated,
		state.EventStoryEstimated,
		state.EventStoryAssigned,
		state.EventStoryStarted,
		state.EventStoryCompleted,
		state.EventStoryMerged,
		state.EventAgentSpawned,
		state.EventAgentTerminated,
	}
	seen := make(map[state.EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Fatalf("duplicate event type: %s", et)
		}
		seen[et] = true
		if et == "" {
			t.Fatal("empty event type")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -v`
Expected: FAIL — package not found

**Step 3: Write minimal implementation**

```go
// internal/state/events.go
package state

import (
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
	"math/rand"
)

type EventType string

const (
	// Requirement events
	EventReqSubmitted EventType = "REQ_SUBMITTED"
	EventReqAnalyzed  EventType = "REQ_ANALYZED"
	EventReqPlanned   EventType = "REQ_PLANNED"
	EventReqCompleted EventType = "REQ_COMPLETED"

	// Story events
	EventStoryCreated         EventType = "STORY_CREATED"
	EventStoryEstimated       EventType = "STORY_ESTIMATED"
	EventStoryAssigned        EventType = "STORY_ASSIGNED"
	EventStoryStarted         EventType = "STORY_STARTED"
	EventStoryProgress        EventType = "STORY_PROGRESS"
	EventStoryCompleted       EventType = "STORY_COMPLETED"
	EventStoryReviewRequested EventType = "STORY_REVIEW_REQUESTED"
	EventStoryReviewPassed    EventType = "STORY_REVIEW_PASSED"
	EventStoryReviewFailed    EventType = "STORY_REVIEW_FAILED"
	EventStoryQAStarted       EventType = "STORY_QA_STARTED"
	EventStoryQAPassed        EventType = "STORY_QA_PASSED"
	EventStoryQAFailed        EventType = "STORY_QA_FAILED"
	EventStoryPRCreated       EventType = "STORY_PR_CREATED"
	EventStoryMerged          EventType = "STORY_MERGED"

	// Agent events
	EventAgentSpawned    EventType = "AGENT_SPAWNED"
	EventAgentCheckpoint EventType = "AGENT_CHECKPOINT"
	EventAgentResumed    EventType = "AGENT_RESUMED"
	EventAgentStuck      EventType = "AGENT_STUCK"
	EventAgentTerminated EventType = "AGENT_TERMINATED"

	// Escalation events
	EventEscalationCreated  EventType = "ESCALATION_CREATED"
	EventEscalationResolved EventType = "ESCALATION_RESOLVED"

	// Supervisor events
	EventSupervisorCheck         EventType = "SUPERVISOR_CHECK"
	EventSupervisorReprioritize  EventType = "SUPERVISOR_REPRIORITIZE"
	EventSupervisorDriftDetected EventType = "SUPERVISOR_DRIFT_DETECTED"

	// Cleanup events
	EventWorktreePruned EventType = "WORKTREE_PRUNED"
	EventBranchDeleted  EventType = "BRANCH_DELETED"
	EventGCCompleted    EventType = "GC_COMPLETED"
)

type Event struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"`
	StoryID   string    `json:"story_id,omitempty"`
	Payload   []byte    `json:"payload,omitempty"`
}

func NewEvent(eventType EventType, agentID, storyID string, data map[string]any) Event {
	var payload []byte
	if data != nil {
		payload, _ = json.Marshal(data)
	}

	entropy := rand.New(rand.NewSource(time.Now().UnixNano()))
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)

	return Event{
		ID:        id.String(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		AgentID:   agentID,
		StoryID:   storyID,
		Payload:   payload,
	}
}
```

**Step 4: Install dependency and run test**

Run: `go get github.com/oklog/ulid/v2@latest && go test ./internal/state/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/state/events.go internal/state/events_test.go go.mod go.sum
git commit -m "feat(state): add event type definitions and Event constructor with ULID IDs"
```

---

### Task 2.2: Event Store Interface and File-Based Implementation

**Files:**
- Create: `internal/state/store.go`
- Create: `internal/state/filestore.go`
- Test: `internal/state/filestore_test.go`

**Step 1: Write the failing test**

```go
// internal/state/filestore_test.go
package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestFileStore_AppendAndList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	store, err := state.NewFileStore(path)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	defer store.Close()

	evt1 := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{"title": "Add auth"})
	evt2 := state.NewEvent(state.EventStoryCreated, "tech-lead", "s-001", map[string]any{"title": "OAuth middleware"})

	if err := store.Append(evt1); err != nil {
		t.Fatalf("append evt1: %v", err)
	}
	if err := store.Append(evt2); err != nil {
		t.Fatalf("append evt2: %v", err)
	}

	events, err := store.List(state.EventFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != state.EventReqSubmitted {
		t.Fatalf("expected REQ_SUBMITTED, got %s", events[0].Type)
	}
	if events[1].Type != state.EventStoryCreated {
		t.Fatalf("expected STORY_CREATED, got %s", events[1].Type)
	}
}

func TestFileStore_FilterByType(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	store.Append(state.NewEvent(state.EventReqSubmitted, "system", "", nil))
	store.Append(state.NewEvent(state.EventStoryCreated, "tl", "s-1", nil))
	store.Append(state.NewEvent(state.EventStoryCreated, "tl", "s-2", nil))

	events, _ := store.List(state.EventFilter{Type: state.EventStoryCreated})
	if len(events) != 2 {
		t.Fatalf("expected 2 story events, got %d", len(events))
	}
}

func TestFileStore_FilterByStoryID(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	store.Append(state.NewEvent(state.EventStoryStarted, "jr-1", "s-1", nil))
	store.Append(state.NewEvent(state.EventStoryStarted, "jr-2", "s-2", nil))
	store.Append(state.NewEvent(state.EventStoryCompleted, "jr-1", "s-1", nil))

	events, _ := store.List(state.EventFilter{StoryID: "s-1"})
	if len(events) != 2 {
		t.Fatalf("expected 2 events for s-1, got %d", len(events))
	}
}

func TestFileStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	store1, _ := state.NewFileStore(path)
	store1.Append(state.NewEvent(state.EventReqSubmitted, "system", "", nil))
	store1.Close()

	store2, _ := state.NewFileStore(path)
	defer store2.Close()
	events, _ := store2.List(state.EventFilter{})
	if len(events) != 1 {
		t.Fatalf("expected 1 event after reopen, got %d", len(events))
	}
}

func TestFileStore_Count(t *testing.T) {
	dir := t.TempDir()
	store, _ := state.NewFileStore(filepath.Join(dir, "events.jsonl"))
	defer store.Close()

	store.Append(state.NewEvent(state.EventReqSubmitted, "system", "", nil))
	store.Append(state.NewEvent(state.EventStoryCreated, "tl", "s-1", nil))

	count, _ := store.Count(state.EventFilter{})
	if count != 2 {
		t.Fatalf("expected count 2, got %d", count)
	}

	count, _ = store.Count(state.EventFilter{Type: state.EventReqSubmitted})
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -v -run FileStore`
Expected: FAIL

**Step 3: Write the store interface**

```go
// internal/state/store.go
package state

type EventFilter struct {
	Type    EventType
	AgentID string
	StoryID string
	Limit   int
}

type EventStore interface {
	Append(event Event) error
	List(filter EventFilter) ([]Event, error)
	Count(filter EventFilter) (int, error)
	Close() error
}
```

**Step 4: Write the file-based implementation**

```go
// internal/state/filestore.go
package state

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
)

type FileStore struct {
	path   string
	file   *os.File
	mu     sync.RWMutex
}

func NewFileStore(path string) (*FileStore, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &FileStore{path: path, file: f}, nil
}

func (fs *FileStore) Append(event Event) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fs.file.Write(append(data, '\n'))
	return err
}

func (fs *FileStore) List(filter EventFilter) ([]Event, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.readAndFilter(filter)
}

func (fs *FileStore) Count(filter EventFilter) (int, error) {
	events, err := fs.List(filter)
	if err != nil {
		return 0, err
	}
	return len(events), nil
}

func (fs *FileStore) Close() error {
	return fs.file.Close()
}

func (fs *FileStore) readAndFilter(filter EventFilter) ([]Event, error) {
	f, err := os.Open(fs.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if filter.Type != "" && evt.Type != filter.Type {
			continue
		}
		if filter.AgentID != "" && evt.AgentID != filter.AgentID {
			continue
		}
		if filter.StoryID != "" && evt.StoryID != filter.StoryID {
			continue
		}
		events = append(events, evt)
		if filter.Limit > 0 && len(events) >= filter.Limit {
			break
		}
	}
	return events, scanner.Err()
}
```

**Step 5: Run tests**

Run: `go test ./internal/state/ -v -run FileStore`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/state/
git commit -m "feat(state): add EventStore interface and file-based append-only implementation"
```

---

### Task 2.3: SQLite Projection Store

**Files:**
- Create: `internal/state/sqlite.go`
- Create: `migrations/001_init.sql`
- Test: `internal/state/sqlite_test.go`

**Step 1: Write the failing test**

```go
// internal/state/sqlite_test.go
package state_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/state"
)

func TestSQLiteStore_ProjectRequirement(t *testing.T) {
	db, err := state.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	defer db.Close()

	evt := state.NewEvent(state.EventReqSubmitted, "system", "", map[string]any{
		"id":          "r-001",
		"title":       "Add OAuth2",
		"description": "Implement OAuth2 across all services",
	})

	if err := db.Project(evt); err != nil {
		t.Fatalf("project: %v", err)
	}

	req, err := db.GetRequirement("r-001")
	if err != nil {
		t.Fatalf("get requirement: %v", err)
	}
	if req.Title != "Add OAuth2" {
		t.Fatalf("expected 'Add OAuth2', got %s", req.Title)
	}
	if req.Status != "pending" {
		t.Fatalf("expected 'pending', got %s", req.Status)
	}
}

func TestSQLiteStore_ProjectStory(t *testing.T) {
	db, _ := state.NewSQLiteStore(":memory:")
	defer db.Close()

	evt := state.NewEvent(state.EventStoryCreated, "tech-lead", "s-001", map[string]any{
		"id":          "s-001",
		"req_id":      "r-001",
		"title":       "OAuth middleware",
		"description": "Create Express middleware for OAuth2 token validation",
		"complexity":  5,
	})

	if err := db.Project(evt); err != nil {
		t.Fatalf("project: %v", err)
	}

	story, err := db.GetStory("s-001")
	if err != nil {
		t.Fatalf("get story: %v", err)
	}
	if story.Title != "OAuth middleware" {
		t.Fatalf("expected 'OAuth middleware', got %s", story.Title)
	}
	if story.Complexity != 5 {
		t.Fatalf("expected complexity 5, got %d", story.Complexity)
	}
	if story.Status != "draft" {
		t.Fatalf("expected 'draft', got %s", story.Status)
	}
}

func TestSQLiteStore_StoryStatusTransition(t *testing.T) {
	db, _ := state.NewSQLiteStore(":memory:")
	defer db.Close()

	db.Project(state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "task", "description": "desc", "complexity": 3,
	}))
	db.Project(state.NewEvent(state.EventStoryAssigned, "tl", "s-001", map[string]any{
		"agent_id": "jr-1",
	}))
	db.Project(state.NewEvent(state.EventStoryStarted, "jr-1", "s-001", nil))

	story, _ := db.GetStory("s-001")
	if story.Status != "in_progress" {
		t.Fatalf("expected 'in_progress', got %s", story.Status)
	}
	if story.AgentID != "jr-1" {
		t.Fatalf("expected agent 'jr-1', got %s", story.AgentID)
	}
}

func TestSQLiteStore_ListStoriesByStatus(t *testing.T) {
	db, _ := state.NewSQLiteStore(":memory:")
	defer db.Close()

	db.Project(state.NewEvent(state.EventStoryCreated, "tl", "s-001", map[string]any{
		"id": "s-001", "req_id": "r-001", "title": "task1", "description": "d", "complexity": 2,
	}))
	db.Project(state.NewEvent(state.EventStoryCreated, "tl", "s-002", map[string]any{
		"id": "s-002", "req_id": "r-001", "title": "task2", "description": "d", "complexity": 5,
	}))
	db.Project(state.NewEvent(state.EventStoryStarted, "jr-1", "s-001", nil))

	stories, _ := db.ListStories(state.StoryFilter{Status: "draft"})
	if len(stories) != 1 {
		t.Fatalf("expected 1 draft story, got %d", len(stories))
	}
	if stories[0].ID != "s-002" {
		t.Fatalf("expected s-002, got %s", stories[0].ID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -v -run SQLite`
Expected: FAIL

**Step 3: Write migration SQL**

```sql
-- migrations/001_init.sql
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
    complexity INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'draft',
    agent_id TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    pr_url TEXT NOT NULL DEFAULT '',
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
```

**Step 4: Write SQLite projection store**

```go
// internal/state/sqlite.go
package state

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed ../../migrations/001_init.sql
var initSQL string

type Requirement struct {
	ID          string
	Title       string
	Description string
	Status      string
	CreatedAt   time.Time
}

type Story struct {
	ID          string
	ReqID       string
	Title       string
	Description string
	Complexity  int
	Status      string
	AgentID     string
	Branch      string
	PRUrl       string
	CreatedAt   time.Time
}

type StoryFilter struct {
	Status string
	ReqID  string
}

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(initSQL); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Project(evt Event) error {
	var data map[string]any
	if evt.Payload != nil {
		json.Unmarshal(evt.Payload, &data)
	}

	switch evt.Type {
	case EventReqSubmitted:
		return s.projectReqSubmitted(data)
	case EventReqPlanned:
		return s.updateReqStatus(data, "planned")
	case EventReqCompleted:
		return s.updateReqStatus(data, "completed")
	case EventStoryCreated:
		return s.projectStoryCreated(data)
	case EventStoryEstimated:
		return s.projectStoryEstimated(evt, data)
	case EventStoryAssigned:
		return s.projectStoryAssigned(evt, data)
	case EventStoryStarted:
		return s.updateStoryStatus(evt.StoryID, "in_progress")
	case EventStoryCompleted:
		return s.updateStoryStatus(evt.StoryID, "review")
	case EventStoryReviewPassed:
		return s.updateStoryStatus(evt.StoryID, "qa")
	case EventStoryReviewFailed:
		return s.updateStoryStatus(evt.StoryID, "in_progress")
	case EventStoryQAPassed:
		return s.updateStoryStatus(evt.StoryID, "pr_submitted")
	case EventStoryQAFailed:
		return s.updateStoryStatus(evt.StoryID, "qa_failed")
	case EventStoryPRCreated:
		return s.projectStoryPR(evt, data)
	case EventStoryMerged:
		return s.updateStoryStatus(evt.StoryID, "merged")
	}
	return nil
}

func (s *SQLiteStore) projectReqSubmitted(data map[string]any) error {
	_, err := s.db.Exec(
		"INSERT INTO requirements (id, title, description, status) VALUES (?, ?, ?, 'pending')",
		getString(data, "id"), getString(data, "title"), getString(data, "description"),
	)
	return err
}

func (s *SQLiteStore) updateReqStatus(data map[string]any, status string) error {
	id := getString(data, "id")
	_, err := s.db.Exec("UPDATE requirements SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", status, id)
	return err
}

func (s *SQLiteStore) projectStoryCreated(data map[string]any) error {
	_, err := s.db.Exec(
		"INSERT INTO stories (id, req_id, title, description, complexity, status) VALUES (?, ?, ?, ?, ?, 'draft')",
		getString(data, "id"), getString(data, "req_id"), getString(data, "title"),
		getString(data, "description"), getInt(data, "complexity"),
	)
	return err
}

func (s *SQLiteStore) projectStoryEstimated(evt Event, data map[string]any) error {
	_, err := s.db.Exec(
		"UPDATE stories SET complexity = ?, status = 'estimated', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		getInt(data, "complexity"), evt.StoryID,
	)
	return err
}

func (s *SQLiteStore) projectStoryAssigned(evt Event, data map[string]any) error {
	_, err := s.db.Exec(
		"UPDATE stories SET agent_id = ?, status = 'assigned', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		getString(data, "agent_id"), evt.StoryID,
	)
	return err
}

func (s *SQLiteStore) updateStoryStatus(storyID, status string) error {
	_, err := s.db.Exec(
		"UPDATE stories SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, storyID,
	)
	return err
}

func (s *SQLiteStore) projectStoryPR(evt Event, data map[string]any) error {
	_, err := s.db.Exec(
		"UPDATE stories SET pr_url = ?, status = 'pr_submitted', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		getString(data, "pr_url"), evt.StoryID,
	)
	return err
}

func (s *SQLiteStore) GetRequirement(id string) (Requirement, error) {
	var r Requirement
	err := s.db.QueryRow("SELECT id, title, description, status, created_at FROM requirements WHERE id = ?", id).
		Scan(&r.ID, &r.Title, &r.Description, &r.Status, &r.CreatedAt)
	return r, err
}

func (s *SQLiteStore) GetStory(id string) (Story, error) {
	var st Story
	err := s.db.QueryRow("SELECT id, req_id, title, description, complexity, status, agent_id, branch, pr_url, created_at FROM stories WHERE id = ?", id).
		Scan(&st.ID, &st.ReqID, &st.Title, &st.Description, &st.Complexity, &st.Status, &st.AgentID, &st.Branch, &st.PRUrl, &st.CreatedAt)
	return st, err
}

func (s *SQLiteStore) ListStories(filter StoryFilter) ([]Story, error) {
	query := "SELECT id, req_id, title, description, complexity, status, agent_id, branch, pr_url, created_at FROM stories WHERE 1=1"
	var args []any
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.ReqID != "" {
		query += " AND req_id = ?"
		args = append(args, filter.ReqID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stories []Story
	for rows.Next() {
		var st Story
		rows.Scan(&st.ID, &st.ReqID, &st.Title, &st.Description, &st.Complexity, &st.Status, &st.AgentID, &st.Branch, &st.PRUrl, &st.CreatedAt)
		stories = append(stories, st)
	}
	return stories, rows.Err()
}

func getString(data map[string]any, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(data map[string]any, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}
```

**Step 5: Install SQLite driver and run tests**

Run: `go get github.com/mattn/go-sqlite3@latest && go test ./internal/state/ -v -run SQLite`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add internal/state/ migrations/
git commit -m "feat(state): add SQLite projection store with event-to-table materialization"
```

---

## Phase 3: Dependency Graph

### Task 3.1: DAG with Topological Sort

**Files:**
- Create: `internal/graph/graph.go`
- Create: `internal/graph/topo.go`
- Test: `internal/graph/graph_test.go`

**Step 1: Write the failing test**

```go
// internal/graph/graph_test.go
package graph_test

import (
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/graph"
)

func TestDAG_AddAndEdges(t *testing.T) {
	g := graph.New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	g.AddEdge("b", "a") // b depends on a
	g.AddEdge("c", "b") // c depends on b

	deps := g.DependenciesOf("c")
	if len(deps) != 1 || deps[0] != "b" {
		t.Fatalf("expected [b], got %v", deps)
	}
}

func TestDAG_TopologicalSort(t *testing.T) {
	g := graph.New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	g.AddNode("d")
	g.AddEdge("b", "a") // b depends on a
	g.AddEdge("c", "a") // c depends on a
	g.AddEdge("d", "b") // d depends on b
	g.AddEdge("d", "c") // d depends on c

	order, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("topo sort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(order))
	}

	// a must come before b and c; b and c must come before d
	pos := make(map[string]int)
	for i, n := range order {
		pos[n] = i
	}
	if pos["a"] > pos["b"] || pos["a"] > pos["c"] {
		t.Fatal("a must come before b and c")
	}
	if pos["b"] > pos["d"] || pos["c"] > pos["d"] {
		t.Fatal("b and c must come before d")
	}
}

func TestDAG_CycleDetection(t *testing.T) {
	g := graph.New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddEdge("a", "b")
	g.AddEdge("b", "a") // cycle

	_, err := g.TopologicalSort()
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestDAG_Waves(t *testing.T) {
	g := graph.New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	g.AddNode("d")
	g.AddEdge("b", "a") // b depends on a
	g.AddEdge("c", "a") // c depends on a
	g.AddEdge("d", "b") // d depends on b

	waves, err := g.Waves()
	if err != nil {
		t.Fatalf("waves: %v", err)
	}
	if len(waves) != 3 {
		t.Fatalf("expected 3 waves, got %d", len(waves))
	}
	// Wave 0: [a] (no deps)
	if len(waves[0]) != 1 || waves[0][0] != "a" {
		t.Fatalf("wave 0: expected [a], got %v", waves[0])
	}
	// Wave 1: [b, c] (depend on a only)
	if len(waves[1]) != 2 {
		t.Fatalf("wave 1: expected 2 nodes, got %d", len(waves[1]))
	}
	// Wave 2: [d] (depends on b)
	if len(waves[2]) != 1 || waves[2][0] != "d" {
		t.Fatalf("wave 2: expected [d], got %v", waves[2])
	}
}

func TestDAG_ReadyNodes(t *testing.T) {
	g := graph.New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	g.AddEdge("b", "a")
	g.AddEdge("c", "b")

	ready := g.ReadyNodes(map[string]bool{})
	if len(ready) != 1 || ready[0] != "a" {
		t.Fatalf("expected [a], got %v", ready)
	}

	ready = g.ReadyNodes(map[string]bool{"a": true})
	if len(ready) != 1 || ready[0] != "b" {
		t.Fatalf("expected [b], got %v", ready)
	}

	ready = g.ReadyNodes(map[string]bool{"a": true, "b": true})
	if len(ready) != 1 || ready[0] != "c" {
		t.Fatalf("expected [c], got %v", ready)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/graph/ -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/graph/graph.go
package graph

import "fmt"

type DAG struct {
	nodes map[string]bool
	edges map[string][]string // node -> nodes it depends on
}

func New() *DAG {
	return &DAG{
		nodes: make(map[string]bool),
		edges: make(map[string][]string),
	}
}

func (g *DAG) AddNode(id string) {
	g.nodes[id] = true
}

func (g *DAG) AddEdge(from, to string) {
	g.edges[from] = append(g.edges[from], to)
}

func (g *DAG) DependenciesOf(id string) []string {
	return g.edges[id]
}

func (g *DAG) ReadyNodes(completed map[string]bool) []string {
	var ready []string
	for node := range g.nodes {
		if completed[node] {
			continue
		}
		allDepsComplete := true
		for _, dep := range g.edges[node] {
			if !completed[dep] {
				allDepsComplete = false
				break
			}
		}
		if allDepsComplete {
			ready = append(ready, node)
		}
	}
	return ready
}
```

```go
// internal/graph/topo.go
package graph

import "fmt"

func (g *DAG) TopologicalSort() ([]string, error) {
	inDegree := make(map[string]int)
	for node := range g.nodes {
		inDegree[node] = 0
	}

	reverse := make(map[string][]string) // dep -> dependents
	for node, deps := range g.edges {
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], node)
			inDegree[node]++
		}
	}

	var queue []string
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}

	var order []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		for _, dependent := range reverse[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	if len(order) != len(g.nodes) {
		return nil, fmt.Errorf("cycle detected: processed %d of %d nodes", len(order), len(g.nodes))
	}
	return order, nil
}

func (g *DAG) Waves() ([][]string, error) {
	if _, err := g.TopologicalSort(); err != nil {
		return nil, err
	}

	var waves [][]string
	completed := make(map[string]bool)

	for len(completed) < len(g.nodes) {
		ready := g.ReadyNodes(completed)
		if len(ready) == 0 {
			break
		}
		waves = append(waves, ready)
		for _, node := range ready {
			completed[node] = true
		}
	}
	return waves, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/graph/ -v`
Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/graph/
git commit -m "feat(graph): add DAG with topological sort, wave computation, and cycle detection"
```

---

## Phase 4: Configuration

### Task 4.1: Config Struct and YAML Loader

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/loader.go`
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Workspace.Backend != "dolt" {
		t.Fatalf("expected default backend 'dolt', got %s", cfg.Workspace.Backend)
	}
	if cfg.Routing.JuniorMaxComplexity != 3 {
		t.Fatalf("expected junior max 3, got %d", cfg.Routing.JuniorMaxComplexity)
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
version: "1.0"
workspace:
  backend: sqlite
  log_level: debug
routing:
  junior_max_complexity: 5
`), 0644)

	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Workspace.Backend != "sqlite" {
		t.Fatalf("expected 'sqlite', got %s", cfg.Workspace.Backend)
	}
	if cfg.Workspace.LogLevel != "debug" {
		t.Fatalf("expected 'debug', got %s", cfg.Workspace.LogLevel)
	}
	if cfg.Routing.JuniorMaxComplexity != 5 {
		t.Fatalf("expected 5, got %d", cfg.Routing.JuniorMaxComplexity)
	}
}

func TestLoadConfig_Validation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vxd.config.yaml")
	os.WriteFile(path, []byte(`
version: "1.0"
workspace:
  backend: invalid_backend
`), 0644)

	_, err := config.LoadFromFile(path)
	if err == nil {
		t.Fatal("expected validation error for invalid backend")
	}
}
```

**Step 2: Run to verify failure, then implement, then run to verify pass**

Implementation: Config struct with YAML tags, DefaultConfig() returning sensible defaults, LoadFromFile() with yaml.Unmarshal + validation, Validate() checking backend is "dolt" or "sqlite".

**Step 3: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat(config): add config struct with YAML loader, defaults, and validation"
```

---

## Phase 5: LLM Client Layer

### Task 5.1: LLM Interface and Replay Client

**Files:**
- Create: `internal/llm/client.go`
- Create: `internal/llm/replay.go`
- Test: `internal/llm/replay_test.go`

**Step 1: Write test, implement, verify**

Interface: `Client` with `Complete(ctx, CompletionRequest) (CompletionResponse, error)`.
ReplayClient: loads pre-recorded JSON responses from `testdata/`, returns them sequentially.

**Step 2: Commit**

```bash
git commit -m "feat(llm): add LLM client interface and replay client for testing"
```

### Task 5.2: Anthropic Client

**Files:**
- Create: `internal/llm/anthropic.go`
- Test: `internal/llm/anthropic_test.go` (using httptest server)

**Step 1: Write test with httptest mock, implement HTTP client, verify**

**Step 2: Commit**

```bash
git commit -m "feat(llm): add Anthropic API client implementation"
```

### Task 5.3: OpenAI Client

**Files:**
- Create: `internal/llm/openai.go`
- Test: `internal/llm/openai_test.go`

Same pattern as Anthropic client.

```bash
git commit -m "feat(llm): add OpenAI API client implementation"
```

---

## Phase 6: Tmux Management

### Task 6.1: Tmux Session Operations

**Files:**
- Create: `internal/tmux/session.go`
- Create: `internal/tmux/capture.go`
- Create: `internal/tmux/send.go`
- Test: `internal/tmux/session_test.go`

**Step 1: Write tests** (skip in CI if tmux not installed)

```go
func TestTmuxAvailable(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}
```

Session operations: CreateSession, KillSession, ListSessions, CapturePaneOutput, SendKeys.
All operations shell out to `tmux` binary via `os/exec`.

**Step 2: Commit**

```bash
git commit -m "feat(tmux): add session management with create, kill, capture, and send-keys"
```

---

## Phase 7: Git Operations

### Task 7.1: Worktree Management

**Files:**
- Create: `internal/git/worktree.go`
- Test: `internal/git/worktree_test.go`

CreateWorktree, DeleteWorktree, ListWorktrees. Uses `git worktree add/remove/list`.

```bash
git commit -m "feat(git): add worktree create, delete, and list operations"
```

### Task 7.2: Branch and Repo Scanning

**Files:**
- Create: `internal/git/branch.go`
- Create: `internal/git/repo.go`
- Test: `internal/git/repo_test.go`

CreateBranch, DeleteBranch, CurrentBranch. ScanRepo detects tech stack from marker files.

```bash
git commit -m "feat(git): add branch management and repo tech stack detection"
```

### Task 7.3: GitHub PR Operations

**Files:**
- Create: `internal/git/github.go`
- Test: `internal/git/github_test.go`

CreatePR, MergePR, GetPRStatus. Uses `gh` CLI via exec.

```bash
git commit -m "feat(git): add GitHub PR operations via gh CLI"
```

---

## Phase 8: Runtime Abstraction

### Task 8.1: Runtime Interface and Registry

**Files:**
- Create: `internal/runtime/runtime.go`
- Create: `internal/runtime/registry.go`
- Test: `internal/runtime/registry_test.go`

Registry loads runtimes from config YAML, returns Runtime implementations by name.

```bash
git commit -m "feat(runtime): add Runtime interface and config-driven registry"
```

### Task 8.2: Claude Code Runtime

**Files:**
- Create: `internal/runtime/claude.go`
- Test: `internal/runtime/claude_test.go`

Implements Runtime interface. Spawns via tmux, detects status via regex patterns.

```bash
git commit -m "feat(runtime): add Claude Code runtime implementation"
```

### Task 8.3: Codex and Gemini Runtimes

**Files:**
- Create: `internal/runtime/codex.go`
- Create: `internal/runtime/gemini.go`

Same pattern, different command/args/detection patterns from config.

```bash
git commit -m "feat(runtime): add Codex and Gemini CLI runtime implementations"
```

---

## Phase 9: Agent Definitions

### Task 9.1: Roles and Complexity Routing

**Files:**
- Create: `internal/agent/roles.go`
- Test: `internal/agent/roles_test.go`

```go
func TestRouteByComplexity(t *testing.T) {
	tests := []struct{ complexity int; expected Role }{
		{1, RoleJunior}, {3, RoleJunior},
		{4, RoleIntermediate}, {5, RoleIntermediate},
		{6, RoleSenior}, {13, RoleSenior},
	}
	// ...
}
```

```bash
git commit -m "feat(agent): add role definitions and complexity-based routing"
```

### Task 9.2: System Prompt Templates

**Files:**
- Create: `internal/agent/prompts.go`
- Test: `internal/agent/prompts_test.go`

Templates for each role with placeholder substitution.

```bash
git commit -m "feat(agent): add role-specific system prompt templates"
```

### Task 9.3: Reputation Scoring

**Files:**
- Create: `internal/agent/scoring.go`
- Test: `internal/agent/scoring_test.go`

Score agents on quality (QA pass rate), reliability (stuck rate), speed (duration).

```bash
git commit -m "feat(agent): add reputation scoring for agent performance tracking"
```

---

## Phase 10: Engine — Planner

### Task 10.1: Requirement Intake and Tech Lead Planning

**Files:**
- Create: `internal/engine/planner.go`
- Test: `internal/engine/planner_test.go`

Planner takes requirement text + repo scan output, calls Tech Lead (Opus API) with structured output format, creates stories + dependency graph.

Test with ReplayClient and pre-recorded Tech Lead response in `testdata/planner_response.json`.

```bash
git commit -m "feat(engine): add Planner with Tech Lead API-based requirement decomposition"
```

---

## Phase 11: Engine — Dispatcher

### Task 11.1: Convoy Bundling and Agent Spawning

**Files:**
- Create: `internal/engine/dispatcher.go`
- Test: `internal/engine/dispatcher_test.go`

Dispatcher reads ready stories from graph waves, routes by complexity, creates worktrees, spawns tmux sessions.

```bash
git commit -m "feat(engine): add Dispatcher with wave-based convoy bundling and agent spawning"
```

---

## Phase 12: Engine — Monitor

### Task 12.1: Watchdog

**Files:**
- Create: `internal/engine/watchdog.go`
- Test: `internal/engine/watchdog_test.go`

Screen fingerprinting: capture pane every N seconds, diff fingerprints, detect stuck/permission/plan-mode.

```bash
git commit -m "feat(engine): add Watchdog with stuck detection and permission bypass"
```

### Task 12.2: Supervisor

**Files:**
- Create: `internal/engine/supervisor.go`
- Test: `internal/engine/supervisor_test.go`

Periodic LLM call to review progress. Compares current story statuses against original requirement.

```bash
git commit -m "feat(engine): add Supervisor with periodic drift detection"
```

---

## Phase 13: Engine — Review, QA, Merge, Cleanup

### Task 13.1: Reviewer (Senior Code Review)

**Files:**
- Create: `internal/engine/reviewer.go`
- Test: `internal/engine/reviewer_test.go`

Gets git diff for story branch, sends to Sonnet API for review, emits pass/fail events.

```bash
git commit -m "feat(engine): add Reviewer for Senior-level code review via API"
```

### Task 13.2: QA Pipeline

**Files:**
- Create: `internal/engine/qa.go`
- Test: `internal/engine/qa_test.go`

Runs lint/build/test commands in worktree via exec. Analyzes output via API if needed.

```bash
git commit -m "feat(engine): add QA pipeline with lint, build, and test execution"
```

### Task 13.3: Merger

**Files:**
- Create: `internal/engine/merger.go`
- Test: `internal/engine/merger_test.go`

Creates PR via GitHub API, auto-merges if configured, emits events, triggers Dispatcher for next wave.

```bash
git commit -m "feat(engine): add Merger with PR creation and auto-merge"
```

### Task 13.4: Reaper (Cleanup)

**Files:**
- Create: `internal/engine/reaper.go`
- Test: `internal/engine/reaper_test.go`

Tiered cleanup: immediate worktree delete, deferred branch delete, log archival.

```bash
git commit -m "feat(engine): add Reaper with tiered cleanup and garbage collection"
```

---

## Phase 14: CLI Commands

### Task 14.1: Core Commands (init, req, status, resume)

**Files:**
- Modify: `internal/cli/root.go` — add subcommands
- Create: `internal/cli/init.go`
- Create: `internal/cli/req.go`
- Create: `internal/cli/status.go`
- Create: `internal/cli/resume.go`

Each command wires CLI flags to engine functions.

```bash
git commit -m "feat(cli): add init, req, status, and resume commands"
```

### Task 14.2: Agent and Escalation Commands

**Files:**
- Create: `internal/cli/agents.go`
- Create: `internal/cli/escalations.go`

```bash
git commit -m "feat(cli): add agents and escalations management commands"
```

### Task 14.3: Maintenance Commands (gc, config, events)

**Files:**
- Create: `internal/cli/gc.go`
- Create: `internal/cli/config.go`
- Create: `internal/cli/events.go`

```bash
git commit -m "feat(cli): add gc, config, and events commands"
```

---

## Phase 15: TUI Dashboard

### Task 15.1: Bubbletea Dashboard App

**Files:**
- Create: `internal/dashboard/app.go`
- Create: `internal/dashboard/pipeline.go`
- Create: `internal/dashboard/agents.go`
- Create: `internal/dashboard/activity.go`
- Create: `internal/dashboard/escalations.go`

Four panels: story pipeline, agent status, event activity feed, escalations.

```bash
git commit -m "feat(dashboard): add TUI dashboard with pipeline, agents, activity, and escalation panels"
```

---

## Phase 16: CI and Release

### Task 16.1: GitHub Actions Workflow

**Files:**
- Create: `.github/workflows/ci.yml`

Test (race + coverage), lint, cross-compile build matrix, goreleaser on tag.

```bash
git commit -m "ci: add GitHub Actions workflow with test, lint, build, and release"
```

---

## Phase 17: Integration and E2E Tests

### Task 17.1: Integration Tests

**Files:**
- Create: `internal/state/integration_test.go`
- Create: `internal/engine/integration_test.go`

Event store -> projection roundtrip. Planner -> Dispatcher -> Reviewer pipeline with mock LLM.

```bash
git commit -m "test: add integration tests for state projection and engine pipeline"
```

### Task 17.2: E2E Test

**Files:**
- Create: `test/e2e_test.go`

Full pipeline: `vxd init` -> `vxd req` -> plan -> dispatch (mock agents) -> review -> merge -> cleanup.
Uses test fixture repo and LLM replay client.

```bash
git commit -m "test: add E2E test for full requirement-to-merge pipeline"
```

---

## Phase 18: Documentation and First Release

### Task 18.1: README

**Files:**
- Create: `README.md`

Quick start, feature overview, installation, configuration, architecture diagram.

```bash
git commit -m "docs: add README with quick start and architecture overview"
```

### Task 18.2: Push and Tag v0.1.0

```bash
git push -u origin main
git tag v0.1.0
git push origin v0.1.0
```

---

## Summary

| Phase | Tasks | Estimated Commits |
|-------|-------|-------------------|
| 1. Scaffolding | 1 | 1 |
| 2. State Layer | 3 | 3 |
| 3. Graph | 1 | 1 |
| 4. Config | 1 | 1 |
| 5. LLM Client | 3 | 3 |
| 6. Tmux | 1 | 1 |
| 7. Git Ops | 3 | 3 |
| 8. Runtime | 3 | 3 |
| 9. Agent Defs | 3 | 3 |
| 10. Planner | 1 | 1 |
| 11. Dispatcher | 1 | 1 |
| 12. Monitor | 2 | 2 |
| 13. Review/QA/Merge/Cleanup | 4 | 4 |
| 14. CLI Commands | 3 | 3 |
| 15. TUI Dashboard | 1 | 1 |
| 16. CI/Release | 1 | 1 |
| 17. Tests | 2 | 2 |
| 18. Docs + Release | 2 | 2 |
| **Total** | **35 tasks** | **~36 commits** |
