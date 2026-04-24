# Port NXD Features to VXD

## Context
NXD has capabilities that VXD should adopt. These are domain-level additions to internal/engine/.

## Requirements

### 1. Pipeline Timeout Guard
Wrap postExecutionPipeline() in a 5-minute context timeout. Add at the start of postExecutionPipeline in monitor.go:
```go
pipelineCtx, pipelineCancel := context.WithTimeout(ctx, 5*time.Minute)
defer pipelineCancel()
```
Use pipelineCtx for all downstream calls (review, QA, conflict resolution, merge).

### 2. QA Failure Analysis
When QA fails in postExecutionPipeline, analyze the error output and provide a diagnostic hint to the agent on retry. Create AnalyzeFailure(qaOutput string) string that uses simple heuristics (not LLM) to suggest fixes. Include the hint in the retry feedback event so the re-dispatched agent has actionable context.

### 3. Story ID Validation
In dispatcher.go DispatchWave(), validate story IDs against a safe pattern before creating branches. Add:
```go
var safeStoryIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
```
Return error if story ID contains unsafe characters. This catches the problem at dispatch time instead of at branch creation time.

## Constraints
- Go code, tests with -race
- Must not break existing 828+ engine tests
- go build ./... must pass
