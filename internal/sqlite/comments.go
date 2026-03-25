package sqlite

import (
	"fmt"

	"github.com/dayvidpham/providence"
	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// InsertComment inserts a new comment row.
func InsertComment(db *DB, comment providence.Comment) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	err := sqlitex.Execute(db.conn,
		`INSERT INTO comments (id, task_id, author_id, body, created_at)
		 VALUES (?1, ?2, ?3, ?4, ?5)`,
		&sqlitex.ExecOptions{
			Args: []any{
				comment.ID.String(),
				comment.TaskID.String(),
				comment.AuthorID.String(),
				comment.Body,
				comment.CreatedAt.UnixNano(),
			},
		})
	if err != nil {
		return fmt.Errorf(
			"sqlite.InsertComment: failed to insert comment on task %q: %w — "+
				"check that the task and author agent both exist",
			comment.TaskID.String(), err,
		)
	}
	return nil
}

// GetComments returns all comments on a task in chronological order.
func GetComments(db *DB, taskID providence.TaskID) ([]providence.Comment, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var comments []providence.Comment
	err := sqlitex.Execute(db.conn,
		`SELECT id, task_id, author_id, body, created_at
		 FROM comments WHERE task_id = ?1 ORDER BY created_at ASC`,
		&sqlitex.ExecOptions{
			Args: []any{taskID.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				c, err := scanComment(stmt)
				if err != nil {
					return err
				}
				comments = append(comments, c)
				return nil
			},
		})
	if err != nil {
		return nil, fmt.Errorf(
			"sqlite.GetComments: failed to query comments for task %q: %w",
			taskID.String(), err,
		)
	}
	return comments, nil
}

func scanComment(stmt *zs.Stmt) (providence.Comment, error) {
	idStr := stmt.ColumnText(0)
	id, err := providence.ParseCommentID(idStr)
	if err != nil {
		return providence.Comment{}, fmt.Errorf("scanComment: invalid comment ID %q: %w", idStr, err)
	}

	taskIDStr := stmt.ColumnText(1)
	taskID, err := providence.ParseTaskID(taskIDStr)
	if err != nil {
		return providence.Comment{}, fmt.Errorf("scanComment: invalid task_id %q: %w", taskIDStr, err)
	}

	authorIDStr := stmt.ColumnText(2)
	authorID, err := providence.ParseAgentID(authorIDStr)
	if err != nil {
		return providence.Comment{}, fmt.Errorf("scanComment: invalid author_id %q: %w", authorIDStr, err)
	}

	return providence.Comment{
		ID:        id,
		TaskID:    taskID,
		AuthorID:  authorID,
		Body:      stmt.ColumnText(3),
		CreatedAt: timeFromNano(stmt.ColumnInt64(4)),
	}, nil
}
