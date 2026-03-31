# FOLLOWUP_PROPOSAL-2: Providence Refactor and Test Coverage

**Beads ID:** (to be assigned)
**Supersedes:** FOLLOWUP_PROPOSAL-1 (aura-plugins-dw5pi)
**References:**
- Follow-up epic: aura-plugins-bu568
- Original URD: aura-plugins-f85gw

---

## 1. Problem

`tracker.go` is 1620 lines because types live in the root package, creating an import cycle with `internal/*`. The dead `internal/` code was deleted; the root cause remains. 6 Tracker-level integration tests and minor cleanups are also outstanding.

## 2. Fix: `pkg/ptypes` Extraction

### Why `pkg/ptypes` over `internal/ptypes`

PROPOSAL-1 used `internal/ptypes`. The elegance reviewer correctly noted that `pkg/ptypes` is strictly better:
- `pkg/` is importable by both root package AND internal packages — no Go visibility restrictions
- `internal/` is importable only within the module — same benefit but adds the re-export maintenance burden
- Since providence is a library (consumers import root), and the types ARE the public API, putting them in `pkg/ptypes` with root aliases is the canonical pattern

### Import graph:
```
pkg/ptypes          (all types, enums, IDs, errors — zero deps)
  ↑           ↑          ↑
  │           │          │
internal/   internal/   providence (root)
sqlite      graph         ↑
  ↑           ↑          │
  └───────────┴──────────┘
        (root imports internal/*)
```

### What moves to `pkg/ptypes`:
- `enums.go` → `pkg/ptypes/enums.go` (all 9 enum types + constants)
- `types.go` → `pkg/ptypes/types.go` (all ID types, entity structs, UpdateFields, ListFilter)
- `errors.go` → `pkg/ptypes/errors.go` (sentinel errors)
- Tests move too: `enums_test.go`, `types_test.go`

### Root re-exports:
```go
package providence

import "github.com/dayvidpham/provenance/pkg/ptypes"

type TaskID = ptypes.TaskID  // transparent alias
// ... all types, constants, errors, parse functions
```

### Re-export sync guard (addresses reviewer finding #3):
```go
// reexport_test.go — fails if pkg/ptypes adds a type not re-exported from root
func TestReexportCompleteness(t *testing.T) {
    // Use reflect to compare exported names in both packages
    // Or: compile-time assignment tests for every type
}
```

## 3. Implementation Slices (4 slices, revised from 3)

### Slice 1: Extract `pkg/ptypes`

**Deliverables:**
- Create `pkg/ptypes/` with `enums.go`, `types.go`, `errors.go`
- Move tests: `enums_test.go`, `types_test.go`
- Update root `providence.go` with type aliases and re-exports
- Add `reexport_test.go` sync guard
- Verify: all existing tests pass, `go build ./...` succeeds

**Exit criteria:** Consumer API unchanged. Re-export test passes.

### Slice 2a: Restore `internal/sqlite`

**Deliverables:**
- Recreate `internal/sqlite/` importing `pkg/ptypes`
- Extract from tracker.go: schema DDL, seed data, pragmas, all SQL CRUD helpers, scan functions
- `internal/sqlite/db.go` — Open, Close, schema, pragmas
- `internal/sqlite/tasks.go` — Task CRUD
- `internal/sqlite/agents.go` — Agent TPT CRUD
- `internal/sqlite/edges.go`, `labels.go`, `comments.go`, `activities.go`
- Tests: `internal/sqlite/db_test.go`

**Exit criteria:** `internal/sqlite` tests pass. tracker.go compiles but still has graph code inline.

### Slice 2b: Restore `internal/graph` + `internal/helpers`

**Deliverables:**
- Recreate `internal/graph/store.go` — graphStore backed by `internal/sqlite`
- Recreate `internal/helpers/ancestors.go` — Ancestors/Descendants
- Extract graph code from tracker.go
- Rewrite tracker.go to delegate to all internal packages
- Tests: `internal/graph/store_test.go`, `internal/helpers/ancestors_test.go`

**Exit criteria:** `tracker.go` under 400 lines. All tests pass. No import cycles. `go vet` clean.

### Slice 3: Missing integration tests + minor cleanups

**Deliverables (IMPORTANT — 6 tests):**
- `TestRegisterSoftwareAgent`
- `TestAgent` (base agent retrieval)
- `TestActivities` (start + end + list)
- `TestRemoveEdge`
- `TestNonBlockedByEdges` (EdgeDerivedFrom etc. don't affect readiness)
- `TestRemoveLabel`

**Deliverables (MINOR — 6 cleanups):**
- M1: Remove redundant blank imports in doc.go
- M2: Generic ID parse helper via generics or shared function
- M3: Namespace validation (reject empty in Create)
- M4: Concurrent access test (10 goroutines × 20 ops)
- M5: Shared test fixtures (`mustOpenMemory`, `mustCreateTask`)
- M6: ListFilter field tests (Label, Namespace)

**Exit criteria:** All new tests pass with `-race`. Full `go test -race ./...` green.

## 4. Slice Dependencies

```
Slice 1 → Slice 2a → Slice 2b → Slice 3
```

Slice 3 can start after Slice 2b since it tests through the public Tracker API.

## 5. Changes from PROPOSAL-1

| Item | PROPOSAL-1 | PROPOSAL-2 |
|------|-----------|-----------|
| Type location | `internal/ptypes` | `pkg/ptypes` (eliminates re-export maintenance) |
| tracker.go target | ~200 lines | Under 400 lines (realistic) |
| Slice 2 | Single slice | Split into 2a (sqlite) + 2b (graph) |
| Re-export guard | None | `reexport_test.go` compile-time check |
| Slice count | 3 | 4 |
