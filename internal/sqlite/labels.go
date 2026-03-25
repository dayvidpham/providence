package sqlite

import (
	"fmt"

	"github.com/dayvidpham/providence"
	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// AddLabel attaches a label to a task. Idempotent (INSERT OR IGNORE).
func AddLabel(db *DB, taskID providence.TaskID, label string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	err := sqlitex.Execute(db.conn,
		`INSERT OR IGNORE INTO labels (task_id, name) VALUES (?1, ?2)`,
		&sqlitex.ExecOptions{
			Args: []any{taskID.String(), label},
		})
	if err != nil {
		return fmt.Errorf(
			"sqlite.AddLabel: failed to add label %q to task %q: %w — "+
				"check that the task exists and the label is non-empty",
			label, taskID.String(), err,
		)
	}
	return nil
}

// RemoveLabel detaches a label from a task. Idempotent (no error if not present).
func RemoveLabel(db *DB, taskID providence.TaskID, label string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	err := sqlitex.Execute(db.conn,
		`DELETE FROM labels WHERE task_id = ?1 AND name = ?2`,
		&sqlitex.ExecOptions{
			Args: []any{taskID.String(), label},
		})
	if err != nil {
		return fmt.Errorf(
			"sqlite.RemoveLabel: failed to remove label %q from task %q: %w",
			label, taskID.String(), err,
		)
	}
	return nil
}

// GetLabels returns all labels attached to a task.
func GetLabels(db *DB, taskID providence.TaskID) ([]string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var labels []string
	err := sqlitex.Execute(db.conn,
		`SELECT name FROM labels WHERE task_id = ?1 ORDER BY name ASC`,
		&sqlitex.ExecOptions{
			Args: []any{taskID.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				labels = append(labels, stmt.ColumnText(0))
				return nil
			},
		})
	if err != nil {
		return nil, fmt.Errorf(
			"sqlite.GetLabels: failed to query labels for task %q: %w",
			taskID.String(), err,
		)
	}
	return labels, nil
}
