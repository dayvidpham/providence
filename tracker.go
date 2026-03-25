package providence

// tracker.go contains the sqliteTracker implementation of the Tracker interface.
//
// Architecture: Types live in pkg/ptypes (no dependencies). The SQL persistence
// layer lives in internal/sqlite (imports only pkg/ptypes + zombiezen sqlite).
// This root package imports both, so there is no import cycle.
//
// The graphStore (dgraph.Store) adapter remains here because it needs access to
// both the sqliteTracker (for its graph field) and internal/sqlite (for SQL).
// Graph traversal methods (Ancestors, Descendants) also remain here since they
// use the dgraph library directly.

import (
	"fmt"
	"time"

	dbsqlite "github.com/dayvidpham/providence/internal/sqlite"
	dgraph "github.com/dominikbraun/graph"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// sqliteTracker — implements Tracker
// ---------------------------------------------------------------------------

// sqliteTracker is the canonical implementation of Tracker.
// The db field holds the internal/sqlite.DB for all SQL operations.
// The graph field is a blocked-by graph used for cycle prevention and
// traversal; its store reads/writes via the same DB.
type sqliteTracker struct {
	db *dbsqlite.DB
	// graph is a directed, cycle-preventing dgraph over task ID strings.
	// Vertices are task ID wire strings; the vertex value type is Task.
	// The store delegates to the same SQLite connection via db.
	graph dgraph.Graph[string, Task]
}

// openTracker opens (or creates) a SQLite database at dbPath and returns
// an initialised Tracker. Pass ":memory:" for an in-memory database.
func openTracker(dbPath string) (Tracker, error) {
	db, err := dbsqlite.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("providence.openTracker: %w", err)
	}

	t := &sqliteTracker{db: db}

	// Construct the blocked-by graph backed by the same connection.
	store := &graphStore{t: t}
	t.graph = dgraph.NewWithStore(
		func(task Task) string { return task.ID.String() },
		store,
		dgraph.Directed(),
		dgraph.PreventCycles(),
	)

	return t, nil
}

// ---------------------------------------------------------------------------
// graphStore — implements dgraph.Store[string, Task]
// ---------------------------------------------------------------------------

// graphStore implements dgraph.Store[string, Task] for the blocked-by subgraph.
// It delegates all persistence to the sqliteTracker's internal/sqlite.DB.
type graphStore struct {
	t *sqliteTracker
}

var _ dgraph.Store[string, Task] = (*graphStore)(nil)

func (s *graphStore) AddVertex(hash string, value Task, _ dgraph.VertexProperties) error {
	if hash != value.ID.String() {
		return fmt.Errorf(
			"graphStore.AddVertex: hash %q does not match task ID %q — "+
				"the hash function must return task.ID.String()",
			hash, value.ID.String(),
		)
	}
	return s.t.db.InsertTask(value)
}

func (s *graphStore) Vertex(hash string) (Task, dgraph.VertexProperties, error) {
	id, err := ParseTaskID(hash)
	if err != nil {
		return Task{}, dgraph.VertexProperties{}, fmt.Errorf(
			"graphStore.Vertex: cannot parse hash %q as TaskID: %w", hash, err,
		)
	}
	task, found, err := s.t.db.GetTask(id)
	if err != nil {
		return Task{}, dgraph.VertexProperties{}, fmt.Errorf(
			"graphStore.Vertex: failed to get task %q: %w", hash, err,
		)
	}
	if !found {
		return Task{}, dgraph.VertexProperties{}, dgraph.ErrVertexNotFound
	}
	return task, dgraph.VertexProperties{}, nil
}

func (s *graphStore) RemoveVertex(_ string) error {
	return fmt.Errorf(
		"graphStore.RemoveVertex: not implemented — " +
			"close the task via CloseTask instead of deleting it",
	)
}

func (s *graphStore) ListVertices() ([]string, error) {
	tasks, err := s.t.db.ListTasks(ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("graphStore.ListVertices: %w", err)
	}
	hashes := make([]string, len(tasks))
	for i, task := range tasks {
		hashes[i] = task.ID.String()
	}
	return hashes, nil
}

func (s *graphStore) VertexCount() (int, error) {
	hashes, err := s.ListVertices()
	if err != nil {
		return 0, fmt.Errorf("graphStore.VertexCount: %w", err)
	}
	return len(hashes), nil
}

func (s *graphStore) AddEdge(sourceHash, targetHash string, _ dgraph.Edge[string]) error {
	srcID, err := ParseTaskID(sourceHash)
	if err != nil {
		return fmt.Errorf("graphStore.AddEdge: invalid source hash %q: %w", sourceHash, err)
	}
	return s.t.db.InsertEdge(srcID, targetHash, EdgeBlockedBy, time.Now().UTC())
}

func (s *graphStore) UpdateEdge(sourceHash, targetHash string, _ dgraph.Edge[string]) error {
	_, err := s.Edge(sourceHash, targetHash)
	return err
}

func (s *graphStore) RemoveEdge(sourceHash, targetHash string) error {
	srcID, err := ParseTaskID(sourceHash)
	if err != nil {
		return fmt.Errorf("graphStore.RemoveEdge: invalid source hash %q: %w", sourceHash, err)
	}
	return s.t.db.DeleteEdge(srcID, targetHash, EdgeBlockedBy)
}

func (s *graphStore) Edge(sourceHash, targetHash string) (dgraph.Edge[string], error) {
	srcID, err := ParseTaskID(sourceHash)
	if err != nil {
		return dgraph.Edge[string]{}, fmt.Errorf("graphStore.Edge: invalid source hash %q: %w", sourceHash, err)
	}
	kind := EdgeBlockedBy
	edges, err := s.t.db.GetEdges(srcID, &kind)
	if err != nil {
		return dgraph.Edge[string]{}, fmt.Errorf("graphStore.Edge: %w", err)
	}
	for _, e := range edges {
		if e.TargetID == targetHash {
			return dgraph.Edge[string]{
				Source:     sourceHash,
				Target:     targetHash,
				Properties: dgraph.EdgeProperties{Attributes: map[string]string{}},
			}, nil
		}
	}
	return dgraph.Edge[string]{}, dgraph.ErrEdgeNotFound
}

func (s *graphStore) ListEdges() ([]dgraph.Edge[string], error) {
	edges, err := s.t.db.GetBlockedByEdges()
	if err != nil {
		return nil, fmt.Errorf("graphStore.ListEdges: %w", err)
	}
	result := make([]dgraph.Edge[string], len(edges))
	for i, e := range edges {
		result[i] = dgraph.Edge[string]{
			Source:     e.SourceID,
			Target:     e.TargetID,
			Properties: dgraph.EdgeProperties{Attributes: map[string]string{}},
		}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func (t *sqliteTracker) Close() error {
	if err := t.db.Close(); err != nil {
		return fmt.Errorf(
			"providence.Tracker.Close: %w", err,
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Task CRUD
// ---------------------------------------------------------------------------

func (t *sqliteTracker) Create(namespace, title, description string, taskType TaskType, priority Priority, phase Phase) (Task, error) {
	if namespace == "" {
		return Task{}, fmt.Errorf(
			"%w: Create — namespace is empty — "+
				"provide a non-empty namespace string such as 'aura-plugins' or 'my-project'",
			ErrInvalidID,
		)
	}

	now := time.Now().UTC()
	task := Task{
		ID:          TaskID{Namespace: namespace, UUID: uuid.Must(uuid.NewV7())},
		Title:       title,
		Description: description,
		Status:      StatusOpen,
		Priority:    priority,
		Type:        taskType,
		Phase:       phase,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// AddVertex calls graphStore.AddVertex → db.InsertTask.
	if err := t.graph.AddVertex(task); err != nil {
		return Task{}, fmt.Errorf(
			"providence.Tracker.Create: failed to insert task %q: %w — "+
				"check that the database is writable and the namespace is valid",
			task.ID.String(), err,
		)
	}
	return task, nil
}

func (t *sqliteTracker) Show(id TaskID) (Task, error) {
	task, found, err := t.db.GetTask(id)
	if err != nil {
		return Task{}, fmt.Errorf("providence.Tracker.Show: %w", err)
	}
	if !found {
		return Task{}, fmt.Errorf(
			"%w: Show — task %q does not exist — "+
				"verify the TaskID was obtained from Create or a previous List/Show call",
			ErrNotFound, id.String(),
		)
	}
	return task, nil
}

func (t *sqliteTracker) Update(id TaskID, fields UpdateFields) (Task, error) {
	task, err := t.db.UpdateTask(id, fields, time.Now().UTC())
	if err != nil {
		return Task{}, fmt.Errorf("providence.Tracker.Update: %w", err)
	}
	return task, nil
}

func (t *sqliteTracker) CloseTask(id TaskID, reason string) (Task, error) {
	current, found, err := t.db.GetTask(id)
	if err != nil {
		return Task{}, fmt.Errorf("providence.Tracker.CloseTask: failed to fetch task %q: %w", id.String(), err)
	}
	if !found {
		return Task{}, fmt.Errorf(
			"%w: CloseTask — task %q does not exist — "+
				"verify the TaskID was obtained from Create or a previous List/Show call",
			ErrNotFound, id.String(),
		)
	}
	if current.Status == StatusClosed {
		return Task{}, fmt.Errorf(
			"%w: CloseTask — task %q is already closed (reason: %q) — "+
				"use Update to reopen the task before closing again",
			ErrAlreadyClosed, id.String(), current.CloseReason,
		)
	}

	task, err := t.db.CloseTask(id, reason, time.Now().UTC())
	if err != nil {
		return Task{}, fmt.Errorf("providence.Tracker.CloseTask: %w", err)
	}
	return task, nil
}

func (t *sqliteTracker) List(filter ListFilter) ([]Task, error) {
	tasks, err := t.db.ListTasks(filter)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.List: %w", err)
	}
	return tasks, nil
}

// ---------------------------------------------------------------------------
// Typed Dependency Edges
// ---------------------------------------------------------------------------

func (t *sqliteTracker) AddEdge(sourceID TaskID, targetID string, kind EdgeKind) error {
	if kind == EdgeBlockedBy {
		if err := t.graph.AddEdge(sourceID.String(), targetID); err != nil {
			if dgraph.ErrEdgeCreatesCycle == err {
				return fmt.Errorf(
					"%w: AddEdge — adding blocked-by edge from %q to %q would create a cycle — "+
						"the target must be work that finishes BEFORE the source; "+
						"use DepTree or Ancestors to inspect the current dependency graph",
					ErrCycleDetected, sourceID.String(), targetID,
				)
			}
			return fmt.Errorf(
				"providence.Tracker.AddEdge: failed to add blocked-by edge %q->%q: %w",
				sourceID.String(), targetID, err,
			)
		}
		return nil
	}

	if err := t.db.InsertEdge(sourceID, targetID, kind, time.Now().UTC()); err != nil {
		return fmt.Errorf(
			"providence.Tracker.AddEdge: failed to insert edge %q->%q kind=%s: %w",
			sourceID.String(), targetID, kind.String(), err,
		)
	}
	return nil
}

func (t *sqliteTracker) RemoveEdge(sourceID TaskID, targetID string, kind EdgeKind) error {
	if kind == EdgeBlockedBy {
		if err := t.graph.RemoveEdge(sourceID.String(), targetID); err != nil {
			if dgraph.ErrEdgeNotFound == err {
				return nil
			}
			return fmt.Errorf(
				"providence.Tracker.RemoveEdge: failed to remove blocked-by edge %q->%q: %w",
				sourceID.String(), targetID, err,
			)
		}
		return nil
	}

	if err := t.db.DeleteEdge(sourceID, targetID, kind); err != nil {
		return fmt.Errorf(
			"providence.Tracker.RemoveEdge: failed to delete edge %q->%q kind=%s: %w",
			sourceID.String(), targetID, kind.String(), err,
		)
	}
	return nil
}

func (t *sqliteTracker) Edges(id TaskID, kind *EdgeKind) ([]Edge, error) {
	edges, err := t.db.GetEdges(id, kind)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Edges: %w", err)
	}
	return edges, nil
}

// ---------------------------------------------------------------------------
// Readiness Queries
// ---------------------------------------------------------------------------

func (t *sqliteTracker) Blocked() ([]Task, error) {
	tasks, err := t.db.BlockedTasks()
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Blocked: %w", err)
	}
	return tasks, nil
}

func (t *sqliteTracker) Ready() ([]Task, error) {
	tasks, err := t.db.ReadyTasks()
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Ready: %w", err)
	}
	return tasks, nil
}

func (t *sqliteTracker) DepTree(id TaskID) ([]Edge, error) {
	edges, err := t.db.GetDepTree(id)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.DepTree: %w", err)
	}
	return edges, nil
}

// Ancestors returns all tasks that transitively block the given task.
// In the blocked-by graph, A->B means "A is blocked by B". Ancestors of A
// are B and everything B transitively waits for (outgoing adjacency DFS).
func (t *sqliteTracker) Ancestors(id TaskID) ([]Task, error) {
	adjacency, err := t.graph.AdjacencyMap()
	if err != nil {
		return nil, fmt.Errorf(
			"providence.Tracker.Ancestors: failed to compute adjacency map for task %q: %w",
			id.String(), err,
		)
	}

	var ids []TaskID
	visited := make(map[string]bool)
	var dfs func(cur string)
	dfs = func(cur string) {
		for adj := range adjacency[cur] {
			if !visited[adj] {
				visited[adj] = true
				if tid, err := ParseTaskID(adj); err == nil {
					ids = append(ids, tid)
				}
				dfs(adj)
			}
		}
	}
	dfs(id.String())

	tasks := make([]Task, 0, len(ids))
	for _, tid := range ids {
		task, found, err := t.db.GetTask(tid)
		if err != nil {
			return nil, fmt.Errorf("providence.Tracker.Ancestors: failed to resolve task %q: %w", tid.String(), err)
		}
		if found {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// Descendants returns all tasks that are transitively waiting for the given
// task. In the blocked-by graph, A->B means "A is blocked by B". Descendants
// of B are A and everything that transitively depends on A (predecessor DFS).
func (t *sqliteTracker) Descendants(id TaskID) ([]Task, error) {
	predecessors, err := t.graph.PredecessorMap()
	if err != nil {
		return nil, fmt.Errorf(
			"providence.Tracker.Descendants: failed to compute predecessor map for task %q: %w",
			id.String(), err,
		)
	}

	var ids []TaskID
	visited := make(map[string]bool)
	var dfs func(cur string)
	dfs = func(cur string) {
		for pred := range predecessors[cur] {
			if !visited[pred] {
				visited[pred] = true
				if tid, err := ParseTaskID(pred); err == nil {
					ids = append(ids, tid)
				}
				dfs(pred)
			}
		}
	}
	dfs(id.String())

	tasks := make([]Task, 0, len(ids))
	for _, tid := range ids {
		task, found, err := t.db.GetTask(tid)
		if err != nil {
			return nil, fmt.Errorf("providence.Tracker.Descendants: failed to resolve task %q: %w", tid.String(), err)
		}
		if found {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// ---------------------------------------------------------------------------
// Labels
// ---------------------------------------------------------------------------

func (t *sqliteTracker) AddLabel(id TaskID, label string) error {
	return t.db.AddLabel(id, label)
}

func (t *sqliteTracker) RemoveLabel(id TaskID, label string) error {
	return t.db.RemoveLabel(id, label)
}

func (t *sqliteTracker) Labels(id TaskID) ([]string, error) {
	labels, err := t.db.GetLabels(id)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Labels: %w", err)
	}
	return labels, nil
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

func (t *sqliteTracker) AddComment(id TaskID, authorID AgentID, body string) (Comment, error) {
	comment, err := t.db.AddComment(id, authorID, body)
	if err != nil {
		return Comment{}, fmt.Errorf("providence.Tracker.AddComment: %w", err)
	}
	return comment, nil
}

func (t *sqliteTracker) Comments(id TaskID) ([]Comment, error) {
	comments, err := t.db.GetComments(id)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Comments: %w", err)
	}
	return comments, nil
}

// ---------------------------------------------------------------------------
// PROV-O Agents
// ---------------------------------------------------------------------------

func (t *sqliteTracker) RegisterHumanAgent(namespace, name, contact string) (HumanAgent, error) {
	ha, err := t.db.RegisterHumanAgent(namespace, name, contact)
	if err != nil {
		return HumanAgent{}, fmt.Errorf("providence.Tracker.RegisterHumanAgent: %w", err)
	}
	return ha, nil
}

func (t *sqliteTracker) RegisterMLAgent(namespace string, role Role, provider Provider, modelName string) (MLAgent, error) {
	mla, err := t.db.RegisterMLAgent(namespace, role, provider, modelName)
	if err != nil {
		return MLAgent{}, fmt.Errorf("providence.Tracker.RegisterMLAgent: %w", err)
	}
	return mla, nil
}

func (t *sqliteTracker) RegisterSoftwareAgent(namespace, name, version, source string) (SoftwareAgent, error) {
	sa, err := t.db.RegisterSoftwareAgent(namespace, name, version, source)
	if err != nil {
		return SoftwareAgent{}, fmt.Errorf("providence.Tracker.RegisterSoftwareAgent: %w", err)
	}
	return sa, nil
}

func (t *sqliteTracker) Agent(id AgentID) (Agent, error) {
	agent, err := t.db.GetAgent(id)
	if err != nil {
		return Agent{}, fmt.Errorf("providence.Tracker.Agent: %w", err)
	}
	return agent, nil
}

func (t *sqliteTracker) HumanAgent(id AgentID) (HumanAgent, error) {
	ha, err := t.db.GetHumanAgent(id)
	if err != nil {
		return HumanAgent{}, fmt.Errorf("providence.Tracker.HumanAgent: %w", err)
	}
	return ha, nil
}

func (t *sqliteTracker) MLAgent(id AgentID) (MLAgent, error) {
	mla, err := t.db.GetMLAgent(id)
	if err != nil {
		return MLAgent{}, fmt.Errorf("providence.Tracker.MLAgent: %w", err)
	}
	return mla, nil
}

func (t *sqliteTracker) SoftwareAgent(id AgentID) (SoftwareAgent, error) {
	sa, err := t.db.GetSoftwareAgent(id)
	if err != nil {
		return SoftwareAgent{}, fmt.Errorf("providence.Tracker.SoftwareAgent: %w", err)
	}
	return sa, nil
}

// ---------------------------------------------------------------------------
// PROV-O Activities
// ---------------------------------------------------------------------------

func (t *sqliteTracker) StartActivity(agentID AgentID, phase Phase, stage Stage, notes string) (Activity, error) {
	act, err := t.db.StartActivity(agentID, phase, stage, notes)
	if err != nil {
		return Activity{}, fmt.Errorf("providence.Tracker.StartActivity: %w", err)
	}
	return act, nil
}

func (t *sqliteTracker) EndActivity(id ActivityID) (Activity, error) {
	act, err := t.db.EndActivity(id)
	if err != nil {
		return Activity{}, fmt.Errorf("providence.Tracker.EndActivity: %w", err)
	}
	return act, nil
}

func (t *sqliteTracker) Activities(agentID *AgentID) ([]Activity, error) {
	activities, err := t.db.GetActivities(agentID)
	if err != nil {
		return nil, fmt.Errorf("providence.Tracker.Activities: %w", err)
	}
	return activities, nil
}
