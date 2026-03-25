package graph_test

import (
	"testing"
	"time"

	intgraph "github.com/dayvidpham/providence/internal/graph"
	dbsqlite "github.com/dayvidpham/providence/internal/sqlite"
	"github.com/dayvidpham/providence/pkg/ptypes"
	dgraph "github.com/dominikbraun/graph"
	"github.com/google/uuid"
)

// openTestDB returns a fresh in-memory sqlite.DB for testing.
func openTestDB(t *testing.T) *dbsqlite.DB {
	t.Helper()
	db, err := dbsqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("sqlite.Open(:memory:) failed: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() failed: %v", err)
		}
	})
	return db
}

// makeTask creates a minimal Task for testing.
func makeTask(ns string) ptypes.Task {
	now := time.Now().UTC()
	return ptypes.Task{
		ID:        ptypes.TaskID{Namespace: ns, UUID: uuid.Must(uuid.NewV7())},
		Title:     "test task",
		Status:    ptypes.StatusOpen,
		Priority:  ptypes.PriorityMedium,
		Type:      ptypes.TaskTypeTask,
		Phase:     ptypes.PhaseUnscoped,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestNewStoreImplementsInterface(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	// Compile-time check is in store.go via var _ dgraph.Store = (*Store)(nil).
	// Runtime check: the store should be non-nil.
	if store == nil {
		t.Fatal("NewStore returned nil")
	}
}

func TestAddVertexAndVertex(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	task := makeTask("test-ns")
	hash := task.ID.String()

	// AddVertex should succeed.
	if err := store.AddVertex(hash, task, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex() error: %v", err)
	}

	// Vertex should return the task.
	got, props, err := store.Vertex(hash)
	if err != nil {
		t.Fatalf("Vertex() error: %v", err)
	}
	_ = props
	if got.ID != task.ID {
		t.Errorf("Vertex() ID = %v, want %v", got.ID, task.ID)
	}
	if got.Title != task.Title {
		t.Errorf("Vertex() Title = %q, want %q", got.Title, task.Title)
	}
}

func TestAddVertexHashMismatch(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	task := makeTask("test-ns")

	// Use a mismatched hash.
	err := store.AddVertex("wrong-hash", task, dgraph.VertexProperties{})
	if err == nil {
		t.Fatal("AddVertex with mismatched hash should return error")
	}
}

func TestVertexNotFound(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	fakeID := ptypes.TaskID{Namespace: "ns", UUID: uuid.Must(uuid.NewV7())}
	_, _, err := store.Vertex(fakeID.String())
	if err != dgraph.ErrVertexNotFound {
		t.Errorf("Vertex() for non-existent task: got %v, want ErrVertexNotFound", err)
	}
}

func TestRemoveVertexNotImplemented(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	err := store.RemoveVertex("anything")
	if err == nil {
		t.Fatal("RemoveVertex should return an error (not implemented)")
	}
}

func TestListVerticesAndVertexCount(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	// Initially empty.
	hashes, err := store.ListVertices()
	if err != nil {
		t.Fatalf("ListVertices() error: %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("ListVertices() initial = %d, want 0", len(hashes))
	}

	count, err := store.VertexCount()
	if err != nil {
		t.Fatalf("VertexCount() error: %v", err)
	}
	if count != 0 {
		t.Errorf("VertexCount() initial = %d, want 0", count)
	}

	// Add two tasks.
	t1 := makeTask("ns")
	t2 := makeTask("ns")
	if err := store.AddVertex(t1.ID.String(), t1, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t1 error: %v", err)
	}
	if err := store.AddVertex(t2.ID.String(), t2, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t2 error: %v", err)
	}

	hashes, err = store.ListVertices()
	if err != nil {
		t.Fatalf("ListVertices() error: %v", err)
	}
	if len(hashes) != 2 {
		t.Errorf("ListVertices() = %d, want 2", len(hashes))
	}

	count, err = store.VertexCount()
	if err != nil {
		t.Fatalf("VertexCount() error: %v", err)
	}
	if count != 2 {
		t.Errorf("VertexCount() = %d, want 2", count)
	}
}

func TestAddEdgeAndEdge(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	t1 := makeTask("ns")
	t2 := makeTask("ns")
	if err := store.AddVertex(t1.ID.String(), t1, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t1 error: %v", err)
	}
	if err := store.AddVertex(t2.ID.String(), t2, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t2 error: %v", err)
	}

	edge := dgraph.Edge[string]{Source: t1.ID.String(), Target: t2.ID.String()}
	if err := store.AddEdge(t1.ID.String(), t2.ID.String(), edge); err != nil {
		t.Fatalf("AddEdge() error: %v", err)
	}

	got, err := store.Edge(t1.ID.String(), t2.ID.String())
	if err != nil {
		t.Fatalf("Edge() error: %v", err)
	}
	if got.Source != t1.ID.String() {
		t.Errorf("Edge source = %q, want %q", got.Source, t1.ID.String())
	}
	if got.Target != t2.ID.String() {
		t.Errorf("Edge target = %q, want %q", got.Target, t2.ID.String())
	}
}

func TestEdgeNotFound(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	t1 := makeTask("ns")
	t2 := makeTask("ns")
	if err := store.AddVertex(t1.ID.String(), t1, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t1 error: %v", err)
	}
	if err := store.AddVertex(t2.ID.String(), t2, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t2 error: %v", err)
	}

	_, err := store.Edge(t1.ID.String(), t2.ID.String())
	if err != dgraph.ErrEdgeNotFound {
		t.Errorf("Edge() for non-existent edge: got %v, want ErrEdgeNotFound", err)
	}
}

func TestRemoveEdge(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	t1 := makeTask("ns")
	t2 := makeTask("ns")
	if err := store.AddVertex(t1.ID.String(), t1, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t1 error: %v", err)
	}
	if err := store.AddVertex(t2.ID.String(), t2, dgraph.VertexProperties{}); err != nil {
		t.Fatalf("AddVertex t2 error: %v", err)
	}

	edge := dgraph.Edge[string]{Source: t1.ID.String(), Target: t2.ID.String()}
	if err := store.AddEdge(t1.ID.String(), t2.ID.String(), edge); err != nil {
		t.Fatalf("AddEdge() error: %v", err)
	}

	if err := store.RemoveEdge(t1.ID.String(), t2.ID.String()); err != nil {
		t.Fatalf("RemoveEdge() error: %v", err)
	}

	_, err := store.Edge(t1.ID.String(), t2.ID.String())
	if err != dgraph.ErrEdgeNotFound {
		t.Errorf("Edge() after RemoveEdge: got %v, want ErrEdgeNotFound", err)
	}
}

func TestListEdges(t *testing.T) {
	db := openTestDB(t)
	store := intgraph.NewStore(db)

	t1 := makeTask("ns")
	t2 := makeTask("ns")
	t3 := makeTask("ns")
	for _, task := range []ptypes.Task{t1, t2, t3} {
		if err := store.AddVertex(task.ID.String(), task, dgraph.VertexProperties{}); err != nil {
			t.Fatalf("AddVertex error: %v", err)
		}
	}

	// Add two edges.
	if err := store.AddEdge(t1.ID.String(), t2.ID.String(), dgraph.Edge[string]{}); err != nil {
		t.Fatalf("AddEdge t1->t2 error: %v", err)
	}
	if err := store.AddEdge(t1.ID.String(), t3.ID.String(), dgraph.Edge[string]{}); err != nil {
		t.Fatalf("AddEdge t1->t3 error: %v", err)
	}

	edges, err := store.ListEdges()
	if err != nil {
		t.Fatalf("ListEdges() error: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("ListEdges() = %d edges, want 2", len(edges))
	}
}

func TestNewGraphCycleDetection(t *testing.T) {
	db := openTestDB(t)
	g := intgraph.NewGraph(db)

	t1 := makeTask("ns")
	t2 := makeTask("ns")
	if err := g.AddVertex(t1); err != nil {
		t.Fatalf("AddVertex t1: %v", err)
	}
	if err := g.AddVertex(t2); err != nil {
		t.Fatalf("AddVertex t2: %v", err)
	}

	// t1 -> t2 (t1 blocked by t2).
	if err := g.AddEdge(t1.ID.String(), t2.ID.String()); err != nil {
		t.Fatalf("AddEdge t1->t2: %v", err)
	}

	// t2 -> t1 should be rejected (cycle).
	err := g.AddEdge(t2.ID.String(), t1.ID.String())
	if err == nil {
		t.Fatal("AddEdge t2->t1 should have been rejected (cycle)")
	}
	if err != dgraph.ErrEdgeCreatesCycle {
		t.Errorf("AddEdge t2->t1: got %v, want ErrEdgeCreatesCycle", err)
	}
}
