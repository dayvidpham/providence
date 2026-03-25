package sqlite

import (
	"fmt"
	"time"

	"github.com/dayvidpham/providence"
	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// InsertEdge inserts a single edge. The edge's created_at is set to now.
// Returns nil if the edge already exists (idempotent insert via INSERT OR IGNORE).
func InsertEdge(db *DB, sourceID providence.TaskID, targetID string, kind providence.EdgeKind, now time.Time) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	err := sqlitex.Execute(db.conn,
		`INSERT OR IGNORE INTO edges (source_id, target_id, kind_id, created_at)
		 VALUES (?1, ?2, ?3, ?4)`,
		&sqlitex.ExecOptions{
			Args: []any{sourceID.String(), targetID, int(kind), now.UnixNano()},
		})
	if err != nil {
		return fmt.Errorf(
			"sqlite.InsertEdge: failed to insert edge %q->%q kind=%s: %w — "+
				"check that the source task exists and kind is valid",
			sourceID.String(), targetID, kind.String(), err,
		)
	}
	return nil
}

// DeleteEdge removes a specific edge. Returns nil even if the edge did not exist.
func DeleteEdge(db *DB, sourceID providence.TaskID, targetID string, kind providence.EdgeKind) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	err := sqlitex.Execute(db.conn,
		`DELETE FROM edges WHERE source_id = ?1 AND target_id = ?2 AND kind_id = ?3`,
		&sqlitex.ExecOptions{
			Args: []any{sourceID.String(), targetID, int(kind)},
		})
	if err != nil {
		return fmt.Errorf(
			"sqlite.DeleteEdge: failed to delete edge %q->%q kind=%s: %w",
			sourceID.String(), targetID, kind.String(), err,
		)
	}
	return nil
}

// EdgeExists reports whether a specific edge exists in the table.
func EdgeExists(db *DB, sourceID providence.TaskID, targetID string, kind providence.EdgeKind) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var exists bool
	err := sqlitex.Execute(db.conn,
		`SELECT 1 FROM edges WHERE source_id = ?1 AND target_id = ?2 AND kind_id = ?3 LIMIT 1`,
		&sqlitex.ExecOptions{
			Args: []any{sourceID.String(), targetID, int(kind)},
			ResultFunc: func(_ *zs.Stmt) error {
				exists = true
				return nil
			},
		})
	if err != nil {
		return false, fmt.Errorf("sqlite.EdgeExists: %w", err)
	}
	return exists, nil
}

// GetEdges returns all edges for a given source task, optionally filtered by kind.
// If kind is nil, all edge kinds are returned.
func GetEdges(db *DB, sourceID providence.TaskID, kind *providence.EdgeKind) ([]providence.Edge, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	query := `SELECT source_id, target_id, kind_id FROM edges WHERE source_id = ?1`
	args := []any{sourceID.String()}

	if kind != nil {
		query += " AND kind_id = ?2"
		args = append(args, int(*kind))
	}
	query += " ORDER BY created_at ASC"

	var edges []providence.Edge
	err := sqlitex.Execute(db.conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *zs.Stmt) error {
			edges = append(edges, providence.Edge{
				SourceID: stmt.ColumnText(0),
				TargetID: stmt.ColumnText(1),
				Kind:     providence.EdgeKind(stmt.ColumnInt(2)),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf(
			"sqlite.GetEdges: failed to query edges for %q: %w",
			sourceID.String(), err,
		)
	}
	return edges, nil
}

// GetBlockedByEdges returns all EdgeBlockedBy edges where source_id = taskID.
// This is the subgraph used by the graph library for cycle prevention.
func GetBlockedByEdges(db *DB) ([]providence.Edge, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	const query = `SELECT source_id, target_id, kind_id FROM edges WHERE kind_id = 0 ORDER BY created_at ASC`

	var edges []providence.Edge
	err := sqlitex.Execute(db.conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *zs.Stmt) error {
			edges = append(edges, providence.Edge{
				SourceID: stmt.ColumnText(0),
				TargetID: stmt.ColumnText(1),
				Kind:     providence.EdgeBlockedBy,
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite.GetBlockedByEdges: %w", err)
	}
	return edges, nil
}

// GetDepTree returns all blocked-by edges reachable from rootID via DFS.
// Only EdgeBlockedBy edges are traversed. The result is in depth-first order.
func GetDepTree(db *DB, rootID providence.TaskID) ([]providence.Edge, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Get all blocked-by edges for the entire graph, then do DFS in Go.
	// This avoids recursive SQL CTEs for simplicity at our scale.
	type adjacency struct {
		targets []string
	}
	adj := make(map[string][]string)

	err := sqlitex.Execute(db.conn,
		`SELECT source_id, target_id FROM edges WHERE kind_id = 0`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *zs.Stmt) error {
				src := stmt.ColumnText(0)
				tgt := stmt.ColumnText(1)
				adj[src] = append(adj[src], tgt)
				return nil
			},
		})
	if err != nil {
		return nil, fmt.Errorf("sqlite.GetDepTree: failed to load edges: %w", err)
	}

	var result []providence.Edge
	visited := make(map[string]bool)
	var dfs func(srcID string)
	dfs = func(srcID string) {
		for _, tgtID := range adj[srcID] {
			result = append(result, providence.Edge{
				SourceID: srcID,
				TargetID: tgtID,
				Kind:     providence.EdgeBlockedBy,
			})
			if !visited[tgtID] {
				visited[tgtID] = true
				dfs(tgtID)
			}
		}
	}
	visited[rootID.String()] = true
	dfs(rootID.String())

	return result, nil
}
