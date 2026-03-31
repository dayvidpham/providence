# FOLLOWUP_PROPOSAL-1: Providence Refactor and Test Coverage

**Beads ID:** aura-plugins-dw5pi
**References:**
- Follow-up epic: aura-plugins-bu568
- Original request: aura-plugins-oviik
- Original URD: aura-plugins-f85gw

---

## 1. Problem

`tracker.go` is 1620 lines because it cannot import `internal/*` packages — they import the root `providence` package for types, creating an import cycle. The dead `internal/` code was deleted in the BLOCKER fix, but the root cause (types in root + implementation in root) remains. Additionally, 6 Tracker-level integration tests and several minor cleanups are outstanding.

## 2. Proposed Fix: `internal/ptypes` Extraction

### Current import graph (broken):
```
providence (types + tracker) → internal/sqlite → providence (types)  ← CYCLE
```

### Proposed import graph:
```
internal/ptypes     (all types, enums, IDs, errors — zero deps)
  ↑                  ↑
  │                  │
internal/sqlite     providence (root)
internal/graph        ↑
internal/helpers      │
  ↑                   │
  └───────────────────┘
        (root imports internal/*)
```

### What moves to `internal/ptypes`:
- `enums.go` → `internal/ptypes/enums.go` (all 9 enum types)
- `types.go` → `internal/ptypes/types.go` (TaskID, AgentID, ActivityID, CommentID, Task, Agent, HumanAgent, MLAgent, SoftwareAgent, Activity, Edge, Label, Comment, UpdateFields, ListFilter)
- `errors.go` → `internal/ptypes/errors.go` (ErrNotFound, ErrCycleDetected, etc.)

### What stays in root `providence` package:
- `providence.go` — Tracker interface + constructors (re-exports types via aliases)
- `tracker.go` — slim delegation to internal packages (~200 lines)
- `doc.go` — package documentation

### Root package re-exports (type aliases):
```go
// providence.go
package providence

import "github.com/dayvidpham/provenance/internal/ptypes"

// Re-export all public types so consumers still use:
//   import "github.com/dayvidpham/provenance"
//   var t providence.Task
type (
    TaskID     = ptypes.TaskID
    AgentID    = ptypes.AgentID
    ActivityID = ptypes.ActivityID
    CommentID  = ptypes.CommentID

    Task           = ptypes.Task
    Agent          = ptypes.Agent
    HumanAgent     = ptypes.HumanAgent
    MLAgent        = ptypes.MLAgent
    SoftwareAgent  = ptypes.SoftwareAgent
    Activity       = ptypes.Activity
    Edge           = ptypes.Edge
    Label          = ptypes.Label
    Comment        = ptypes.Comment
    UpdateFields   = ptypes.UpdateFields
    ListFilter     = ptypes.ListFilter

    Status    = ptypes.Status
    Priority  = ptypes.Priority
    TaskType  = ptypes.TaskType
    EdgeKind  = ptypes.EdgeKind
    AgentKind = ptypes.AgentKind
    Provider  = ptypes.Provider
    Role      = ptypes.Role
    Phase     = ptypes.Phase
    Stage     = ptypes.Stage
    Model     = ptypes.Model
)

// Re-export constants
const (
    StatusOpen       = ptypes.StatusOpen
    StatusInProgress = ptypes.StatusInProgress
    StatusClosed     = ptypes.StatusClosed
    // ... all enum constants
)

// Re-export sentinel errors
var (
    ErrNotFound         = ptypes.ErrNotFound
    ErrCycleDetected    = ptypes.ErrCycleDetected
    ErrAlreadyClosed    = ptypes.ErrAlreadyClosed
    ErrInvalidID        = ptypes.ErrInvalidID
    ErrAgentKindMismatch = ptypes.ErrAgentKindMismatch
)

// Re-export parse functions
var (
    ParseTaskID     = ptypes.ParseTaskID
    ParseAgentID    = ptypes.ParseAgentID
    ParseActivityID = ptypes.ParseActivityID
    ParseCommentID  = ptypes.ParseCommentID
)
```

This preserves the consumer API — `import "github.com/dayvidpham/provenance"` still works with all the same types and constants.

## 3. Implementation Slices

### Slice 1: Extract `internal/ptypes`

**Deliverables:**
- Move `enums.go`, `types.go`, `errors.go` to `internal/ptypes/`
- Change package declaration to `package ptypes`
- Move corresponding tests: `enums_test.go`, `types_test.go`
- Update `providence.go` with type aliases and constant re-exports
- Verify: `go test -race ./...` passes, `go build ./...` succeeds

**Exit criteria:** All existing tests pass. Consumer API unchanged (type aliases are transparent).

### Slice 2: Restore `internal/sqlite`, `internal/graph`, `internal/helpers`

**Deliverables:**
- Recreate `internal/sqlite/` importing `internal/ptypes` (not root)
- Recreate `internal/graph/` (graphStore) importing `internal/ptypes`
- Recreate `internal/helpers/` (Ancestors/Descendants) importing `internal/ptypes`
- Rewrite `tracker.go` to delegate to internal packages (~200 lines)
- Remove all SQL/graph code from tracker.go

**Exit criteria:** `tracker.go` under 300 lines. All tests pass. No import cycles.

### Slice 3: Missing integration tests + minor cleanups

**Deliverables (IMPORTANT findings):**
- `TestRegisterSoftwareAgent` — register + retrieve
- `TestAgent` — base agent retrieval by ID
- `TestActivities` — start + end + list
- `TestRemoveEdge` — add then remove
- `TestNonBlockedByEdges` — EdgeDerivedFrom, EdgeSupersedes don't affect readiness
- `TestRemoveLabel` — add then remove, verify idempotent

**Deliverables (MINOR findings):**
- M1: Remove redundant blank imports in doc.go
- M2: Generic ID parse helper (shared by all 4 parse functions)
- M3: Namespace validation (reject empty namespace in Create)
- M4: Concurrent access test (10 goroutines × 20 ops, per pasture pattern)
- M5: Shared test helper (extract `mustOpenMemory`, `mustCreateTask` fixtures)
- M6: Test ListFilter fields (Label, Namespace)

**Exit criteria:** All new tests pass with `-race`. `go vet ./...` clean.

## 4. Dependencies

Slice 1 → Slice 2 → Slice 3 (sequential — each builds on the previous).

## 5. Risk

The type alias approach (`type TaskID = ptypes.TaskID`) is fully transparent — existing consumers see no change. The `=` in Go type aliases means the types are identical, not merely assignable. This is the standard Go pattern for extracting types without breaking API.
