package sqlite

import (
	"fmt"

	"zombiezen.com/go/sqlite/sqlitex"
)

// ensureSchema applies CREATE TABLE IF NOT EXISTS statements and seeds
// reference data. It is idempotent and safe to call on every Open.
//
// The schema follows BCNF: every non-trivial FD has a superkey as its
// determinant. Enum columns use INTEGER FK references to lookup tables.
// All tables use STRICT mode.
func (db *DB) ensureSchema() error {
	stmts := []string{
		// ----------------------------------------------------------------
		// Reference (lookup) tables — enum types
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS statuses (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS priorities (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS task_types (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS edge_kinds (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS agent_kinds (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS providers (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS roles (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS phases (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		`CREATE TABLE IF NOT EXISTS stages (
			id   INTEGER PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		) STRICT`,

		// ----------------------------------------------------------------
		// Composite lookup table: ml_models
		// FD: id -> (provider_id, name); (provider_id, name) -> id
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS ml_models (
			id          INTEGER PRIMARY KEY,
			provider_id INTEGER NOT NULL REFERENCES providers(id),
			name        TEXT NOT NULL,
			UNIQUE (provider_id, name)
		) STRICT`,

		// ----------------------------------------------------------------
		// Agent TPT hierarchy
		// ----------------------------------------------------------------

		// FD: id -> kind_id
		`CREATE TABLE IF NOT EXISTS agents (
			id      TEXT PRIMARY KEY,
			kind_id INTEGER NOT NULL REFERENCES agent_kinds(id)
		) STRICT`,

		// FD: agent_id -> (name, contact)
		`CREATE TABLE IF NOT EXISTS agents_human (
			agent_id TEXT PRIMARY KEY REFERENCES agents(id),
			name     TEXT NOT NULL,
			contact  TEXT NOT NULL DEFAULT ''
		) STRICT, WITHOUT ROWID`,

		// FD: agent_id -> (role_id, model_id)
		`CREATE TABLE IF NOT EXISTS agents_ml (
			agent_id TEXT PRIMARY KEY REFERENCES agents(id),
			role_id  INTEGER NOT NULL REFERENCES roles(id),
			model_id INTEGER NOT NULL REFERENCES ml_models(id)
		) STRICT, WITHOUT ROWID`,

		// FD: agent_id -> (name, version, source)
		`CREATE TABLE IF NOT EXISTS agents_software (
			agent_id TEXT PRIMARY KEY REFERENCES agents(id),
			name     TEXT NOT NULL,
			version  TEXT NOT NULL DEFAULT '',
			source   TEXT NOT NULL DEFAULT ''
		) STRICT, WITHOUT ROWID`,

		// ----------------------------------------------------------------
		// Tasks (PROV-O Entity)
		// FD: id -> all columns
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS tasks (
			id           TEXT PRIMARY KEY,
			namespace    TEXT NOT NULL,
			title        TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			status_id    INTEGER NOT NULL DEFAULT 0 REFERENCES statuses(id),
			priority_id  INTEGER NOT NULL DEFAULT 2 REFERENCES priorities(id),
			type_id      INTEGER NOT NULL DEFAULT 2 REFERENCES task_types(id),
			phase_id     INTEGER NOT NULL REFERENCES phases(id),
			owner_id     TEXT REFERENCES agents(id),
			notes        TEXT NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL,
			updated_at   INTEGER NOT NULL,
			closed_at    INTEGER,
			close_reason TEXT NOT NULL DEFAULT ''
		) STRICT`,

		`CREATE INDEX IF NOT EXISTS idx_tasks_namespace ON tasks (namespace)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status    ON tasks (status_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_priority  ON tasks (priority_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_type      ON tasks (type_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_phase     ON tasks (phase_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_owner     ON tasks (owner_id)`,

		// ----------------------------------------------------------------
		// Edges (all 6 kinds, cross-entity capable)
		// FD: (source_id, target_id, kind_id) -> created_at
		// target_id has NO FK — may reference tasks, agents, or activities
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS edges (
			source_id  TEXT NOT NULL REFERENCES tasks(id),
			target_id  TEXT NOT NULL,
			kind_id    INTEGER NOT NULL REFERENCES edge_kinds(id),
			created_at INTEGER NOT NULL,
			PRIMARY KEY (source_id, target_id, kind_id)
		) STRICT, WITHOUT ROWID`,

		`CREATE INDEX IF NOT EXISTS idx_edges_source ON edges (source_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_target ON edges (target_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_kind   ON edges (kind_id)`,

		// ----------------------------------------------------------------
		// Activities (PROV-O Activity)
		// FD: id -> all columns
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS activities (
			id         TEXT PRIMARY KEY,
			agent_id   TEXT NOT NULL REFERENCES agents(id),
			phase_id   INTEGER NOT NULL REFERENCES phases(id),
			stage_id   INTEGER NOT NULL REFERENCES stages(id),
			started_at INTEGER NOT NULL,
			ended_at   INTEGER,
			notes      TEXT NOT NULL DEFAULT ''
		) STRICT`,

		`CREATE INDEX IF NOT EXISTS idx_activities_agent ON activities (agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_activities_phase ON activities (phase_id)`,

		// ----------------------------------------------------------------
		// Labels
		// FD: (task_id, name) -> {} (composite PK, no non-key attributes)
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS labels (
			task_id TEXT NOT NULL REFERENCES tasks(id),
			name    TEXT NOT NULL,
			PRIMARY KEY (task_id, name)
		) STRICT, WITHOUT ROWID`,

		`CREATE INDEX IF NOT EXISTS idx_labels_name ON labels (name)`,

		// ----------------------------------------------------------------
		// Comments
		// FD: id -> all columns
		// ----------------------------------------------------------------
		`CREATE TABLE IF NOT EXISTS comments (
			id         TEXT PRIMARY KEY,
			task_id    TEXT NOT NULL REFERENCES tasks(id),
			author_id  TEXT NOT NULL REFERENCES agents(id),
			body       TEXT NOT NULL,
			created_at INTEGER NOT NULL
		) STRICT`,

		`CREATE INDEX IF NOT EXISTS idx_comments_task   ON comments (task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_author ON comments (author_id)`,
	}

	for _, stmt := range stmts {
		if err := sqlitex.ExecuteTransient(db.conn, stmt, nil); err != nil {
			return fmt.Errorf("ensureSchema: %w — statement: %s", err, stmt[:min(len(stmt), 80)])
		}
	}

	return db.seedReferenceData()
}

// seedReferenceData inserts the canonical enum values into lookup tables.
// INSERT OR IGNORE makes this idempotent.
func (db *DB) seedReferenceData() error {
	seeds := []string{
		`INSERT OR IGNORE INTO statuses (id, name) VALUES
			(0, 'open'), (1, 'in_progress'), (2, 'closed')`,

		`INSERT OR IGNORE INTO priorities (id, name) VALUES
			(0, 'critical'), (1, 'high'), (2, 'medium'), (3, 'low'), (4, 'backlog')`,

		`INSERT OR IGNORE INTO task_types (id, name) VALUES
			(0, 'bug'), (1, 'feature'), (2, 'task'), (3, 'epic'), (4, 'chore')`,

		`INSERT OR IGNORE INTO edge_kinds (id, name) VALUES
			(0, 'blocked_by'), (1, 'derived_from'), (2, 'supersedes'),
			(3, 'discovered_from'), (4, 'generated_by'), (5, 'attributed_to')`,

		`INSERT OR IGNORE INTO agent_kinds (id, name) VALUES
			(0, 'human'), (1, 'machine_learning'), (2, 'software')`,

		`INSERT OR IGNORE INTO providers (id, name) VALUES
			(0, 'anthropic'), (1, 'google'), (2, 'openai'), (3, 'local')`,

		`INSERT OR IGNORE INTO roles (id, name) VALUES
			(0, 'human'), (1, 'architect'), (2, 'supervisor'), (3, 'worker'), (4, 'reviewer')`,

		`INSERT OR IGNORE INTO phases (id, name) VALUES
			(0, 'request'), (1, 'elicit'), (2, 'propose'), (3, 'review'),
			(4, 'plan_uat'), (5, 'ratify'), (6, 'handoff'), (7, 'impl_plan'),
			(8, 'worker_slices'), (9, 'code_review'), (10, 'impl_uat'), (11, 'landing'),
			(12, 'unscoped')`,

		`INSERT OR IGNORE INTO stages (id, name) VALUES
			(0, 'not_started'), (1, 'in_progress'), (2, 'blocked'), (3, 'complete')`,

		// ml_models — closed lookup table (known models at schema creation time)
		`INSERT OR IGNORE INTO ml_models (id, provider_id, name) VALUES
			(0, 0, 'claude_opus_4'),
			(1, 0, 'claude_sonnet_4'),
			(2, 0, 'claude_haiku_4')`,
	}

	for _, seed := range seeds {
		if err := sqlitex.ExecuteTransient(db.conn, seed, nil); err != nil {
			return fmt.Errorf("seedReferenceData: %w — seed: %s", err, seed[:min(len(seed), 80)])
		}
	}

	return nil
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
