package sqlite

import (
	"fmt"
	"time"

	"github.com/dayvidpham/providence"
	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// InsertActivity inserts a new activity row.
func InsertActivity(db *DB, activity providence.Activity) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return sqlitex.Execute(db.conn,
		`INSERT INTO activities (id, agent_id, phase_id, stage_id, started_at, ended_at, notes)
		 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`,
		&sqlitex.ExecOptions{
			Args: []any{
				activity.ID.String(),
				activity.AgentID.String(),
				int(activity.Phase),
				int(activity.Stage),
				activity.StartedAt.UnixNano(),
				timeToNullInt(activity.EndedAt),
				activity.Notes,
			},
		})
}

// EndActivity sets the ended_at timestamp for an activity.
// Returns ErrNotFound if the activity does not exist.
func EndActivity(db *DB, id providence.ActivityID, endTime time.Time) (providence.Activity, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if err := sqlitex.Execute(db.conn,
		`UPDATE activities SET ended_at = ?2 WHERE id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String(), endTime.UnixNano()},
		}); err != nil {
		return providence.Activity{}, fmt.Errorf("sqlite.EndActivity: %w", err)
	}

	var act providence.Activity
	var found bool
	if err := sqlitex.Execute(db.conn,
		`SELECT id, agent_id, phase_id, stage_id, started_at, ended_at, notes
		 FROM activities WHERE id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				var err error
				act, err = scanActivity(stmt)
				if err != nil {
					return err
				}
				found = true
				return nil
			},
		}); err != nil {
		return providence.Activity{}, fmt.Errorf("sqlite.EndActivity: re-fetch: %w", err)
	}
	if !found {
		return providence.Activity{}, fmt.Errorf("%w: activity %q not found in sqlite.EndActivity", providence.ErrNotFound, id.String())
	}
	return act, nil
}

// GetActivities returns all activities, optionally filtered by agent.
func GetActivities(db *DB, agentID *providence.AgentID) ([]providence.Activity, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	query := `SELECT id, agent_id, phase_id, stage_id, started_at, ended_at, notes
	          FROM activities`
	var args []any
	if agentID != nil {
		query += ` WHERE agent_id = ?1`
		args = append(args, agentID.String())
	}
	query += ` ORDER BY started_at ASC`

	var activities []providence.Activity
	err := sqlitex.Execute(db.conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *zs.Stmt) error {
			act, err := scanActivity(stmt)
			if err != nil {
				return err
			}
			activities = append(activities, act)
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite.GetActivities: %w", err)
	}
	return activities, nil
}

func scanActivity(stmt *zs.Stmt) (providence.Activity, error) {
	idStr := stmt.ColumnText(0)
	id, err := providence.ParseActivityID(idStr)
	if err != nil {
		return providence.Activity{}, fmt.Errorf("scanActivity: invalid activity ID %q: %w", idStr, err)
	}

	agentIDStr := stmt.ColumnText(1)
	agentID, err := providence.ParseAgentID(agentIDStr)
	if err != nil {
		return providence.Activity{}, fmt.Errorf("scanActivity: invalid agent_id %q: %w", agentIDStr, err)
	}

	startedAt := time.Unix(0, stmt.ColumnInt64(4)).UTC()
	var endedAt *time.Time
	if !stmt.ColumnIsNull(5) {
		t := time.Unix(0, stmt.ColumnInt64(5)).UTC()
		endedAt = &t
	}

	return providence.Activity{
		ID:        id,
		AgentID:   agentID,
		Phase:     providence.Phase(stmt.ColumnInt(2)),
		Stage:     providence.Stage(stmt.ColumnInt(3)),
		StartedAt: startedAt,
		EndedAt:   endedAt,
		Notes:     stmt.ColumnText(6),
	}, nil
}
