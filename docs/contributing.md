# Contributing to VXD

This guide covers how to extend VXD with new runtimes, engine components, CLI commands, and LLM providers.

## Development Setup

```bash
git clone https://github.com/tzone85/vortex-dispatch.git
cd vortex-dispatch
make build    # Build the vxd binary
make test     # Run tests with race detection + coverage
make lint     # Run golangci-lint (install: `brew install golangci-lint`)
make verify   # Full green gate: build + vet + tests + doc-coverage + lint
```

### Running Tests

```bash
# Unit + integration tests
go test ./...

# With race detection and coverage
go test ./... -race -coverprofile=coverage.out

# E2E tests only
go test -tags e2e ./test/

# Specific package
go test ./internal/engine/...
```

## Adding a New Runtime

Runtimes are pluggable AI CLI tools (like Claude Code, Codex, Gemini). To add a new one:

### Step 1: Add Config Entry

In your `vxd.yaml`:

```yaml
runtimes:
  my-runtime:
    command: my-cli-tool
    args: ["--auto-mode"]
    models: ["model-a", "model-b"]
    detection:
      idle_pattern: "my-cli>"
      permission_pattern: "allow\\?"
      plan_mode_pattern: ""
```

That's it for basic support — VXD's `CLIRuntime` will use your config to spawn tmux sessions, send input, and detect status via the patterns you define.

### Step 2: Test Detection Patterns

The most important part is getting the regex patterns right. Test them by:

1. Starting a tmux session manually: `tmux new -s test my-cli-tool`
2. Observing the output format when the tool is idle, requesting permissions, or done
3. Writing regex patterns that match those states
4. Verifying with: `vxd config validate`

### Step 3: Bind Models

In the `models` section of your config, reference models supported by your runtime:

```yaml
models:
  junior:
    provider: my-provider
    model: model-a
    max_tokens: 4000
```

## Adding a New Engine Component

Engine components live in `internal/engine/` and follow a consistent pattern.

### Pattern

Every engine component:
1. Accepts dependencies via constructor (interfaces, not concrete types)
2. Has a primary method that does the work
3. Emits events via `state.NewEvent()` + store append
4. Returns a result struct

### Example: Adding a "Formatter" Component

```go
// internal/engine/formatter.go
package engine

import (
    "github.com/tzone85/vortex-dispatch/internal/state"
)

// FormatterResult holds the outcome of formatting.
type FormatterResult struct {
    StoryID    string
    FilesFixed int
    Passed     bool
}

// CommandRunner is the interface for executing shell commands (already exists in qa.go).

// Formatter runs code formatting on a story's worktree.
type Formatter struct {
    events state.EventStore
    runner CommandRunner
}

// NewFormatter creates a Formatter with the given dependencies.
func NewFormatter(events state.EventStore, runner CommandRunner) *Formatter {
    return &Formatter{events: events, runner: runner}
}

// Format runs the formatting command and returns results.
func (f *Formatter) Format(storyID, worktreePath, command string) (FormatterResult, error) {
    output, err := f.runner.Run(worktreePath, "sh", "-c", command)

    result := FormatterResult{
        StoryID: storyID,
        Passed:  err == nil,
    }

    evt := state.NewEvent(state.EventStoryProgress, "", storyID, map[string]any{
        "stage":  "format",
        "passed": result.Passed,
        "output": output,
    })
    if appendErr := f.events.Append(evt); appendErr != nil {
        return result, appendErr
    }

    return result, err
}
```

### Key Rules

- **Depend on interfaces** — use `CommandRunner`, `llm.Client`, `GitHubOps`, etc.
- **Emit events** — every significant action should produce an event
- **Return result structs** — callers should not need to parse events to know the outcome
- **Write tests with mocks** — use the existing mock patterns from `qa_test.go` or `merger_test.go`

## Adding a New CLI Command

CLI commands live in `internal/cli/` with one file per command.

### Pattern

```go
// internal/cli/mycommand.go
package cli

import (
    "fmt"
    "github.com/spf13/cobra"
)

func newMyCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "mycommand",
        Short: "Description of what it does",
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg, events, projections, err := loadStores(cmd)
            if err != nil {
                return err
            }
            defer projections.Close()

            // Your logic here
            fmt.Println("Done")
            return nil
        },
    }

    // Add flags
    cmd.Flags().Bool("verbose", false, "Enable verbose output")

    return cmd
}
```

### Register the Command

In `internal/cli/root.go`, add your command to the root:

```go
rootCmd.AddCommand(newMyCommand())
```

### Helpers

Use the existing helpers in `internal/cli/helpers.go`:
- `loadStores(cmd)` — loads config, event store, and projection store
- `expandHome(path)` — expands `~` in file paths
- `buildLLMClient(modelCfg)` — creates an LLM client from config

## Adding a New LLM Provider

LLM providers live in `internal/llm/` and implement the `Client` interface:

```go
type Client interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
```

### Step 1: Implement the Interface

```go
// internal/llm/myprovider.go
package llm

import "context"

type MyProviderClient struct {
    apiKey string
    model  string
}

func NewMyProviderClient(apiKey, model string) *MyProviderClient {
    return &MyProviderClient{apiKey: apiKey, model: model}
}

func (c *MyProviderClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    // Call your provider's API
    // Return CompletionResponse{Content: "...", TokensUsed: N}
}
```

### Step 2: Register in the Client Factory

In `internal/cli/helpers.go`, update `buildLLMClient`:

```go
case "myprovider":
    return llm.NewMyProviderClient(os.Getenv("MYPROVIDER_API_KEY"), modelCfg.Model), nil
```

### Step 3: Write Tests

Follow the pattern in `anthropic_test.go` or `openai_test.go`:
- Test request construction
- Test response parsing
- Test error handling
- Use `httptest.NewServer` for HTTP-level testing

## Adding a New Event Type

### Step 1: Define the Constant

In `internal/state/events.go`:

```go
const (
    // ... existing events
    EventMyNewEvent EventType = "MY_NEW_EVENT"
)
```

### Step 2: Handle in Projections

In `internal/state/sqlite.go`, add a case to the `Project()` method:

```go
case EventMyNewEvent:
    // Update relevant SQLite tables
```

### Step 3: Add Migration (if new table needed)

Add a migration file in `migrations/` following the existing naming pattern.

## Code Style

- **Interfaces for dependencies** — all external calls go through interfaces
- **Immutable events** — events are append-only, never modified
- **Small files** — aim for 200-400 lines per file
- **Table-driven tests** — use Go's `t.Run()` with test case slices
- **Error wrapping** — use `fmt.Errorf("context: %w", err)` for error chains

## Commit Messages

Follow conventional commits:

```
feat(engine): add code formatter component
fix(watchdog): increase fingerprint buffer size
test(qa): add failure path coverage
docs: update contributing guide
```
