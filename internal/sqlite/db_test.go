package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dayvidpham/providence"
	"github.com/dayvidpham/providence/internal/sqlite"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTaskID(ns string) providence.TaskID {
	return providence.TaskID{Namespace: ns, UUID: uuid.Must(uuid.NewV7())}
}

func newAgentID(ns string) providence.AgentID {
	return providence.AgentID{Namespace: ns, UUID: uuid.Must(uuid.NewV7())}
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

// ---------------------------------------------------------------------------
// DB Open / Close
// ---------------------------------------------------------------------------

func TestOpenMemory(t *testing.T) {
	db := openTestDB(t)
	if db == nil {
		t.Fatal("expected non-nil DB")
	}
}

func TestOpenMemoryIdempotentSchema(t *testing.T) {
	// Opening twice should not fail (CREATE TABLE IF NOT EXISTS).
	db, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = db.Close()
}

func TestCloseIdempotent(t *testing.T) {
	db, err := sqlite.OpenMemory()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	// Second close should be a no-op, not panic.
	if err := db.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tasks: Insert + GetTask
// ---------------------------------------------------------------------------

func TestInsertAndGetTask(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("test-ns")
	task := providence.Task{
		ID:          id,
		Title:       "Test task",
		Description: "A test task",
		Status:      providence.StatusOpen,
		Priority:    providence.PriorityMedium,
		Type:        providence.TaskTypeFeature,
		Phase:       providence.PhaseUnscoped,
		Notes:       "some notes",
		CreatedAt:   n,
		UpdatedAt:   n,
	}

	if err := sqlite.InsertTask(db, task); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	got, found, err := sqlite.GetTask(db, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !found {
		t.Fatal("GetTask: expected to find task, got not found")
	}

	if got.ID != id {
		t.Errorf("ID mismatch: got %v, want %v", got.ID, id)
	}
	if got.Title != task.Title {
		t.Errorf("Title: got %q, want %q", got.Title, task.Title)
	}
	if got.Status != task.Status {
		t.Errorf("Status: got %v, want %v", got.Status, task.Status)
	}
	if got.Phase != task.Phase {
		t.Errorf("Phase: got %v, want %v", got.Phase, task.Phase)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	db := openTestDB(t)
	id := newTaskID("ns")
	_, found, err := sqlite.GetTask(db, id)
	if err != nil {
		t.Fatalf("GetTask unexpected error: %v", err)
	}
	if found {
		t.Error("GetTask: expected not-found, got found")
	}
}

func TestInsertTaskWithOwner(t *testing.T) {
	db := openTestDB(t)
	n := now()

	// Register an agent first to satisfy FK.
	agent, err := sqlite.RegisterHumanAgent(db, "test-ns", "Alice", "alice@example.com", n)
	if err != nil {
		t.Fatalf("RegisterHumanAgent: %v", err)
	}

	id := newTaskID("test-ns")
	task := providence.Task{
		ID:        id,
		Title:     "Owned task",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseWorkerSlices,
		Owner:     &agent.ID,
		CreatedAt: n,
		UpdatedAt: n,
	}

	if err := sqlite.InsertTask(db, task); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	got, found, err := sqlite.GetTask(db, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !found {
		t.Fatal("task not found")
	}
	if got.Owner == nil {
		t.Fatal("expected non-nil Owner")
	}
	if *got.Owner != agent.ID {
		t.Errorf("Owner: got %v, want %v", *got.Owner, agent.ID)
	}
}

// ---------------------------------------------------------------------------
// Tasks: UpdateTask
// ---------------------------------------------------------------------------

func TestUpdateTaskTitle(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("ns")
	task := providence.Task{
		ID:        id,
		Title:     "Original title",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}
	if err := sqlite.InsertTask(db, task); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	newTitle := "Updated title"
	updated, err := sqlite.UpdateTask(db, id, providence.UpdateFields{Title: &newTitle}, n.Add(time.Second))
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Title != newTitle {
		t.Errorf("Title: got %q, want %q", updated.Title, newTitle)
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("ns")
	task := providence.Task{
		ID:        id,
		Title:     "A task",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}
	if err := sqlite.InsertTask(db, task); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	s := providence.StatusInProgress
	updated, err := sqlite.UpdateTask(db, id, providence.UpdateFields{Status: &s}, n.Add(time.Second))
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Status != providence.StatusInProgress {
		t.Errorf("Status: got %v, want in_progress", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// Tasks: CloseTask
// ---------------------------------------------------------------------------

func TestCloseTask(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("ns")
	task := providence.Task{
		ID:        id,
		Title:     "Closable",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}
	if err := sqlite.InsertTask(db, task); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	closed, err := sqlite.CloseTask(db, id, "done", n.Add(time.Second))
	if err != nil {
		t.Fatalf("CloseTask: %v", err)
	}
	if closed.Status != providence.StatusClosed {
		t.Errorf("Status: got %v, want closed", closed.Status)
	}
	if closed.CloseReason != "done" {
		t.Errorf("CloseReason: got %q, want %q", closed.CloseReason, "done")
	}
	if closed.ClosedAt == nil {
		t.Error("ClosedAt should not be nil after close")
	}
}

// ---------------------------------------------------------------------------
// Tasks: ListTasks
// ---------------------------------------------------------------------------

func TestListTasksEmpty(t *testing.T) {
	db := openTestDB(t)
	tasks, err := sqlite.ListTasks(db, providence.ListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestListTasksWithFilter(t *testing.T) {
	db := openTestDB(t)
	n := now()

	insert := func(title string, status providence.Status, phase providence.Phase) {
		id := newTaskID("ns")
		err := sqlite.InsertTask(db, providence.Task{
			ID:        id,
			Title:     title,
			Status:    status,
			Priority:  providence.PriorityMedium,
			Type:      providence.TaskTypeTask,
			Phase:     phase,
			CreatedAt: n,
			UpdatedAt: n,
		})
		if err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}

	insert("open request", providence.StatusOpen, providence.PhaseRequest)
	insert("closed task", providence.StatusClosed, providence.PhaseUnscoped)
	insert("open unscoped", providence.StatusOpen, providence.PhaseUnscoped)

	open := providence.StatusOpen
	tasks, err := sqlite.ListTasks(db, providence.ListFilter{Status: &open})
	if err != nil {
		t.Fatalf("ListTasks with status filter: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 open tasks, got %d", len(tasks))
	}

	phase := providence.PhaseUnscoped
	tasks, err = sqlite.ListTasks(db, providence.ListFilter{Phase: &phase})
	if err != nil {
		t.Fatalf("ListTasks with phase filter: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 unscoped tasks, got %d", len(tasks))
	}
}

// ---------------------------------------------------------------------------
// Tasks: Ready and Blocked
// ---------------------------------------------------------------------------

func TestReadyAndBlockedTasks(t *testing.T) {
	db := openTestDB(t)
	n := now()

	// parent is blocked by child. Once child is closed, parent becomes ready.
	parentID := newTaskID("ns")
	childID := newTaskID("ns")

	for _, task := range []providence.Task{
		{ID: parentID, Title: "parent", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
		{ID: childID, Title: "child", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
	} {
		if err := sqlite.InsertTask(db, task); err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}

	// Parent is blocked by child.
	if err := sqlite.InsertEdge(db, parentID, childID.String(), providence.EdgeBlockedBy, n); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}

	// child should be ready; parent should be blocked.
	ready, err := sqlite.ReadyTasks(db)
	if err != nil {
		t.Fatalf("ReadyTasks: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != childID {
		t.Errorf("expected only child to be ready, got: %v", ready)
	}

	blocked, err := sqlite.BlockedTasks(db)
	if err != nil {
		t.Fatalf("BlockedTasks: %v", err)
	}
	if len(blocked) != 1 || blocked[0].ID != parentID {
		t.Errorf("expected only parent to be blocked, got: %v", blocked)
	}

	// Close child — now parent should become ready.
	if _, err := sqlite.CloseTask(db, childID, "done", n.Add(time.Second)); err != nil {
		t.Fatalf("CloseTask: %v", err)
	}

	ready, err = sqlite.ReadyTasks(db)
	if err != nil {
		t.Fatalf("ReadyTasks after close: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != parentID {
		t.Errorf("expected parent to be ready after child close, got: %v", ready)
	}
}

// ---------------------------------------------------------------------------
// Edges
// ---------------------------------------------------------------------------

func TestInsertAndGetEdges(t *testing.T) {
	db := openTestDB(t)
	n := now()

	srcID := newTaskID("ns")
	tgtID := newTaskID("ns")

	for _, task := range []providence.Task{
		{ID: srcID, Title: "src", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
		{ID: tgtID, Title: "tgt", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
	} {
		if err := sqlite.InsertTask(db, task); err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}

	if err := sqlite.InsertEdge(db, srcID, tgtID.String(), providence.EdgeBlockedBy, n); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}

	edges, err := sqlite.GetEdges(db, srcID, nil)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].Kind != providence.EdgeBlockedBy {
		t.Errorf("Kind: got %v, want blocked_by", edges[0].Kind)
	}
	if edges[0].TargetID != tgtID.String() {
		t.Errorf("TargetID: got %q, want %q", edges[0].TargetID, tgtID.String())
	}
}

func TestEdgeIdempotentInsert(t *testing.T) {
	db := openTestDB(t)
	n := now()

	srcID := newTaskID("ns")
	tgtID := newTaskID("ns")
	for _, task := range []providence.Task{
		{ID: srcID, Title: "src", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
		{ID: tgtID, Title: "tgt", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
	} {
		if err := sqlite.InsertTask(db, task); err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}

	// Insert twice — should not error.
	for i := 0; i < 2; i++ {
		if err := sqlite.InsertEdge(db, srcID, tgtID.String(), providence.EdgeBlockedBy, n); err != nil {
			t.Fatalf("InsertEdge (iteration %d): %v", i, err)
		}
	}

	edges, err := sqlite.GetEdges(db, srcID, nil)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("expected exactly 1 edge after 2 inserts, got %d", len(edges))
	}
}

func TestDeleteEdge(t *testing.T) {
	db := openTestDB(t)
	n := now()

	srcID := newTaskID("ns")
	tgtID := newTaskID("ns")
	for _, task := range []providence.Task{
		{ID: srcID, Title: "src", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
		{ID: tgtID, Title: "tgt", Status: providence.StatusOpen, Priority: providence.PriorityMedium, Type: providence.TaskTypeTask, Phase: providence.PhaseUnscoped, CreatedAt: n, UpdatedAt: n},
	} {
		if err := sqlite.InsertTask(db, task); err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}

	if err := sqlite.InsertEdge(db, srcID, tgtID.String(), providence.EdgeBlockedBy, n); err != nil {
		t.Fatalf("InsertEdge: %v", err)
	}
	if err := sqlite.DeleteEdge(db, srcID, tgtID.String(), providence.EdgeBlockedBy); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}

	edges, err := sqlite.GetEdges(db, srcID, nil)
	if err != nil {
		t.Fatalf("GetEdges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after delete, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// Agents: TPT hierarchy
// ---------------------------------------------------------------------------

func TestRegisterHumanAgent(t *testing.T) {
	db := openTestDB(t)
	agent, err := sqlite.RegisterHumanAgent(db, "ns", "Alice", "alice@example.com", now())
	if err != nil {
		t.Fatalf("RegisterHumanAgent: %v", err)
	}
	if agent.Kind != providence.AgentKindHuman {
		t.Errorf("Kind: got %v, want human", agent.Kind)
	}
	if agent.Name != "Alice" {
		t.Errorf("Name: got %q, want Alice", agent.Name)
	}
	if agent.Contact != "alice@example.com" {
		t.Errorf("Contact: got %q", agent.Contact)
	}

	got, err := sqlite.GetHumanAgent(db, agent.ID)
	if err != nil {
		t.Fatalf("GetHumanAgent: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("GetHumanAgent Name: got %q", got.Name)
	}
}

func TestRegisterMLAgent(t *testing.T) {
	db := openTestDB(t)
	agent, err := sqlite.RegisterMLAgent(db, "ns", providence.RoleWorker, providence.ProviderAnthropic, "claude_sonnet_4")
	if err != nil {
		t.Fatalf("RegisterMLAgent: %v", err)
	}
	if agent.Kind != providence.AgentKindMachineLearning {
		t.Errorf("Kind: got %v, want machine_learning", agent.Kind)
	}
	if agent.Role != providence.RoleWorker {
		t.Errorf("Role: got %v, want worker", agent.Role)
	}
	if agent.Model.Name != "claude_sonnet_4" {
		t.Errorf("Model.Name: got %q", agent.Model.Name)
	}

	got, err := sqlite.GetMLAgent(db, agent.ID)
	if err != nil {
		t.Fatalf("GetMLAgent: %v", err)
	}
	if got.Model.Provider != providence.ProviderAnthropic {
		t.Errorf("Model.Provider: got %v", got.Model.Provider)
	}
}

func TestRegisterMLAgentUnknownModel(t *testing.T) {
	db := openTestDB(t)
	_, err := sqlite.RegisterMLAgent(db, "ns", providence.RoleWorker, providence.ProviderAnthropic, "nonexistent_model")
	if err == nil {
		t.Fatal("expected error for unknown model, got nil")
	}
	if !errors.Is(err, providence.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestRegisterSoftwareAgent(t *testing.T) {
	db := openTestDB(t)
	agent, err := sqlite.RegisterSoftwareAgent(db, "ns", "aura-swarm", "v0.5.0", "git@github.com:dayvidpham/aura-plugins")
	if err != nil {
		t.Fatalf("RegisterSoftwareAgent: %v", err)
	}
	if agent.Kind != providence.AgentKindSoftware {
		t.Errorf("Kind: got %v, want software", agent.Kind)
	}

	got, err := sqlite.GetSoftwareAgent(db, agent.ID)
	if err != nil {
		t.Fatalf("GetSoftwareAgent: %v", err)
	}
	if got.Version != "v0.5.0" {
		t.Errorf("Version: got %q", got.Version)
	}
}

func TestGetAgentNotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := sqlite.GetHumanAgent(db, newAgentID("ns"))
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
	if !errors.Is(err, providence.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Activities
// ---------------------------------------------------------------------------

func TestInsertAndGetActivities(t *testing.T) {
	db := openTestDB(t)
	n := now()

	agent, err := sqlite.RegisterHumanAgent(db, "ns", "Bob", "", n)
	if err != nil {
		t.Fatalf("RegisterHumanAgent: %v", err)
	}

	actID := providence.ActivityID{Namespace: "ns", UUID: uuid.Must(uuid.NewV7())}
	activity := providence.Activity{
		ID:        actID,
		AgentID:   agent.ID,
		Phase:     providence.PhaseWorkerSlices,
		Stage:     providence.StageInProgress,
		StartedAt: n,
		Notes:     "working",
	}

	if err := sqlite.InsertActivity(db, activity); err != nil {
		t.Fatalf("InsertActivity: %v", err)
	}

	activities, err := sqlite.GetActivities(db, nil)
	if err != nil {
		t.Fatalf("GetActivities: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(activities))
	}
	if activities[0].Phase != providence.PhaseWorkerSlices {
		t.Errorf("Phase: got %v", activities[0].Phase)
	}
}

func TestEndActivity(t *testing.T) {
	db := openTestDB(t)
	n := now()

	agent, err := sqlite.RegisterHumanAgent(db, "ns", "Carol", "", n)
	if err != nil {
		t.Fatalf("RegisterHumanAgent: %v", err)
	}

	actID := providence.ActivityID{Namespace: "ns", UUID: uuid.Must(uuid.NewV7())}
	activity := providence.Activity{
		ID:        actID,
		AgentID:   agent.ID,
		Phase:     providence.PhaseCodeReview,
		Stage:     providence.StageInProgress,
		StartedAt: n,
	}

	if err := sqlite.InsertActivity(db, activity); err != nil {
		t.Fatalf("InsertActivity: %v", err)
	}

	ended, err := sqlite.EndActivity(db, actID, n.Add(time.Hour))
	if err != nil {
		t.Fatalf("EndActivity: %v", err)
	}
	if ended.EndedAt == nil {
		t.Error("EndedAt should not be nil after EndActivity")
	}
}

// ---------------------------------------------------------------------------
// Labels
// ---------------------------------------------------------------------------

func TestAddAndGetLabels(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("ns")
	if err := sqlite.InsertTask(db, providence.Task{
		ID:        id,
		Title:     "labeled",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	for _, label := range []string{"alpha", "beta", "gamma"} {
		if err := sqlite.AddLabel(db, id, label); err != nil {
			t.Fatalf("AddLabel %q: %v", label, err)
		}
	}

	// Idempotent add should not duplicate.
	if err := sqlite.AddLabel(db, id, "alpha"); err != nil {
		t.Fatalf("AddLabel idempotent: %v", err)
	}

	labels, err := sqlite.GetLabels(db, id)
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	if len(labels) != 3 {
		t.Errorf("expected 3 labels, got %d: %v", len(labels), labels)
	}
}

func TestRemoveLabel(t *testing.T) {
	db := openTestDB(t)
	n := now()

	id := newTaskID("ns")
	if err := sqlite.InsertTask(db, providence.Task{
		ID:        id,
		Title:     "task",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	if err := sqlite.AddLabel(db, id, "tag"); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}
	if err := sqlite.RemoveLabel(db, id, "tag"); err != nil {
		t.Fatalf("RemoveLabel: %v", err)
	}

	labels, err := sqlite.GetLabels(db, id)
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("expected 0 labels after remove, got %d", len(labels))
	}
}

// ---------------------------------------------------------------------------
// Comments
// ---------------------------------------------------------------------------

func TestInsertAndGetComments(t *testing.T) {
	db := openTestDB(t)
	n := now()

	agent, err := sqlite.RegisterHumanAgent(db, "ns", "Dave", "", n)
	if err != nil {
		t.Fatalf("RegisterHumanAgent: %v", err)
	}

	taskID := newTaskID("ns")
	if err := sqlite.InsertTask(db, providence.Task{
		ID:        taskID,
		Title:     "commented task",
		Status:    providence.StatusOpen,
		Priority:  providence.PriorityMedium,
		Type:      providence.TaskTypeTask,
		Phase:     providence.PhaseUnscoped,
		CreatedAt: n,
		UpdatedAt: n,
	}); err != nil {
		t.Fatalf("InsertTask: %v", err)
	}

	commentID := providence.CommentID{Namespace: "ns", UUID: uuid.Must(uuid.NewV7())}
	comment := providence.Comment{
		ID:        commentID,
		TaskID:    taskID,
		AuthorID:  agent.ID,
		Body:      "This is a comment",
		CreatedAt: n,
	}

	if err := sqlite.InsertComment(db, comment); err != nil {
		t.Fatalf("InsertComment: %v", err)
	}

	comments, err := sqlite.GetComments(db, taskID)
	if err != nil {
		t.Fatalf("GetComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Body != "This is a comment" {
		t.Errorf("Body: got %q", comments[0].Body)
	}
	if comments[0].AuthorID != agent.ID {
		t.Errorf("AuthorID mismatch")
	}
}

// ---------------------------------------------------------------------------
// DepTree
// ---------------------------------------------------------------------------

func TestGetDepTree(t *testing.T) {
	db := openTestDB(t)
	n := now()

	// Build: root -> child1 -> grandchild
	//              -> child2
	ids := make([]providence.TaskID, 4)
	for i := range ids {
		ids[i] = newTaskID("ns")
		if err := sqlite.InsertTask(db, providence.Task{
			ID:        ids[i],
			Title:     "task",
			Status:    providence.StatusOpen,
			Priority:  providence.PriorityMedium,
			Type:      providence.TaskTypeTask,
			Phase:     providence.PhaseUnscoped,
			CreatedAt: n,
			UpdatedAt: n,
		}); err != nil {
			t.Fatalf("InsertTask: %v", err)
		}
	}
	root, child1, child2, grandchild := ids[0], ids[1], ids[2], ids[3]

	edges := [][2]providence.TaskID{
		{root, child1},
		{root, child2},
		{child1, grandchild},
	}
	for _, e := range edges {
		if err := sqlite.InsertEdge(db, e[0], e[1].String(), providence.EdgeBlockedBy, n); err != nil {
			t.Fatalf("InsertEdge: %v", err)
		}
	}

	tree, err := sqlite.GetDepTree(db, root)
	if err != nil {
		t.Fatalf("GetDepTree: %v", err)
	}
	// Should have 3 edges total.
	if len(tree) != 3 {
		t.Errorf("expected 3 edges in tree, got %d: %v", len(tree), tree)
	}
}
