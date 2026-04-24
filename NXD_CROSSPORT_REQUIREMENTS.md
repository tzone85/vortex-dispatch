# Requirements: Port NXD Capabilities to VXD

## Context
NXD has several features that VXD should adopt. These were identified during the VXD→NXD porting analysis.

## Requirements

### 1. Pipeline Timeout Guard
**Priority:** HIGH | **Port from:** NXD `monitor.go` lines 327-331

Wrap `postExecutionPipeline()` in a 5-minute context timeout to prevent LLM calls (review, QA, conflict resolution) from hanging indefinitely.

```go
pipelineCtx, pipelineCancel := context.WithTimeout(ctx, 5*time.Minute)
defer pipelineCancel()
```

### 2. QA Failure Analysis with LLM Hints
**Priority:** HIGH | **Port from:** NXD `AnalyzeFailure()`

When QA fails, analyze the error output with the LLM and provide a diagnostic hint to the agent on retry. Currently VXD just resets the story to draft without actionable feedback.

### 3. Story ID Validation at Dispatch
**Priority:** MEDIUM | **Port from:** NXD `dispatcher.go` lines 17-18, 93-95

Validate story IDs against `^[a-zA-Z0-9._-]+$` pattern BEFORE dispatch (fail early) instead of sanitizing branch names after the fact.

### 4. MemPalace Semantic Mining (Long-term)
**Priority:** LOW | **Port from:** NXD `internal/memory/`

After each story merge, review decision, and QA failure, mine the event into a semantic knowledge base. This builds persistent project knowledge that improves future agent context.

## Architectural Note
These ports maintain DDD with hexagonal ports/adapters. No architectural change needed — all features are domain-level additions to `internal/engine/`.
