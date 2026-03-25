package sqlite

import (
	"fmt"
	"time"

	"github.com/dayvidpham/providence"
	"github.com/google/uuid"
	zs "zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// RegisterHumanAgent inserts (or replaces) a human agent.
// A new UUIDv7 AgentID is assigned on every call. For idempotent
// registration by identity, callers should look up by name first.
func RegisterHumanAgent(db *DB, namespace, name, contact string, now time.Time) (providence.HumanAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	id := providence.AgentID{
		Namespace: namespace,
		UUID:      uuid.Must(uuid.NewV7()),
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents (id, kind_id) VALUES (?1, 0)`,
		&sqlitex.ExecOptions{Args: []any{id.String()}}); err != nil {
		return providence.HumanAgent{}, fmt.Errorf(
			"sqlite.RegisterHumanAgent: failed to insert base agent row for %q: %w",
			id.String(), err,
		)
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents_human (agent_id, name, contact) VALUES (?1, ?2, ?3)`,
		&sqlitex.ExecOptions{Args: []any{id.String(), name, contact}}); err != nil {
		return providence.HumanAgent{}, fmt.Errorf(
			"sqlite.RegisterHumanAgent: failed to insert human agent row for %q: %w",
			id.String(), err,
		)
	}

	return providence.HumanAgent{
		Agent:   providence.Agent{ID: id, Kind: providence.AgentKindHuman},
		Name:    name,
		Contact: contact,
	}, nil
}

// RegisterMLAgent inserts an ML agent after looking up the model by (provider, modelName).
// Returns ErrNotFound if the model combination is not in the ml_models table.
func RegisterMLAgent(db *DB, namespace string, role providence.Role, provider providence.Provider, modelName string) (providence.MLAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Look up model_id from ml_models.
	var modelID int
	var modelFound bool
	if err := sqlitex.Execute(db.conn,
		`SELECT id FROM ml_models WHERE provider_id = ?1 AND name = ?2`,
		&sqlitex.ExecOptions{
			Args: []any{int(provider), modelName},
			ResultFunc: func(stmt *zs.Stmt) error {
				modelID = stmt.ColumnInt(0)
				modelFound = true
				return nil
			},
		}); err != nil {
		return providence.MLAgent{}, fmt.Errorf(
			"sqlite.RegisterMLAgent: failed to look up model (%s, %q): %w",
			provider.String(), modelName, err,
		)
	}
	if !modelFound {
		return providence.MLAgent{}, fmt.Errorf(
			"%w: model (%s, %q) not found in ml_models — "+
				"only models seeded at schema creation time are available; "+
				"fix by using a known (provider, name) combination from the lookup table",
			providence.ErrNotFound, provider.String(), modelName,
		)
	}

	id := providence.AgentID{
		Namespace: namespace,
		UUID:      uuid.Must(uuid.NewV7()),
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents (id, kind_id) VALUES (?1, 1)`,
		&sqlitex.ExecOptions{Args: []any{id.String()}}); err != nil {
		return providence.MLAgent{}, fmt.Errorf(
			"sqlite.RegisterMLAgent: failed to insert base agent row: %w", err,
		)
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents_ml (agent_id, role_id, model_id) VALUES (?1, ?2, ?3)`,
		&sqlitex.ExecOptions{Args: []any{id.String(), int(role), modelID}}); err != nil {
		return providence.MLAgent{}, fmt.Errorf(
			"sqlite.RegisterMLAgent: failed to insert ml agent row: %w", err,
		)
	}

	return providence.MLAgent{
		Agent: providence.Agent{ID: id, Kind: providence.AgentKindMachineLearning},
		Role:  role,
		Model: providence.MLModel{
			ID:       modelID,
			Provider: provider,
			Name:     modelName,
		},
	}, nil
}

// RegisterSoftwareAgent inserts a software agent.
func RegisterSoftwareAgent(db *DB, namespace, name, version, source string) (providence.SoftwareAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	id := providence.AgentID{
		Namespace: namespace,
		UUID:      uuid.Must(uuid.NewV7()),
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents (id, kind_id) VALUES (?1, 2)`,
		&sqlitex.ExecOptions{Args: []any{id.String()}}); err != nil {
		return providence.SoftwareAgent{}, fmt.Errorf(
			"sqlite.RegisterSoftwareAgent: failed to insert base agent row: %w", err,
		)
	}

	if err := sqlitex.Execute(db.conn,
		`INSERT INTO agents_software (agent_id, name, version, source) VALUES (?1, ?2, ?3, ?4)`,
		&sqlitex.ExecOptions{Args: []any{id.String(), name, version, source}}); err != nil {
		return providence.SoftwareAgent{}, fmt.Errorf(
			"sqlite.RegisterSoftwareAgent: failed to insert software agent row: %w", err,
		)
	}

	return providence.SoftwareAgent{
		Agent:   providence.Agent{ID: id, Kind: providence.AgentKindSoftware},
		Name:    name,
		Version: version,
		Source:  source,
	}, nil
}

// GetAgent returns the base agent (kind only) by ID.
func GetAgent(db *DB, id providence.AgentID) (providence.Agent, bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var agent providence.Agent
	var found bool
	err := sqlitex.Execute(db.conn,
		`SELECT id, kind_id FROM agents WHERE id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				agent = providence.Agent{
					ID:   id,
					Kind: providence.AgentKind(stmt.ColumnInt(1)),
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return providence.Agent{}, false, fmt.Errorf("sqlite.GetAgent: %w", err)
	}
	return agent, found, nil
}

// GetHumanAgent returns a human agent by ID.
// Returns ErrNotFound if the agent doesn't exist; ErrAgentKindMismatch if it's not human.
func GetHumanAgent(db *DB, id providence.AgentID) (providence.HumanAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var ha providence.HumanAgent
	var found bool
	err := sqlitex.Execute(db.conn,
		`SELECT a.kind_id, h.name, h.contact
		 FROM agents a JOIN agents_human h ON a.id = h.agent_id
		 WHERE a.id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				kind := providence.AgentKind(stmt.ColumnInt(0))
				if kind != providence.AgentKindHuman {
					return fmt.Errorf("%w: expected human, got %s for agent %q — "+
						"call Agent() first to inspect the Kind",
						providence.ErrAgentKindMismatch, kind.String(), id.String())
				}
				ha = providence.HumanAgent{
					Agent:   providence.Agent{ID: id, Kind: providence.AgentKindHuman},
					Name:    stmt.ColumnText(1),
					Contact: stmt.ColumnText(2),
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return providence.HumanAgent{}, fmt.Errorf("sqlite.GetHumanAgent: %w", err)
	}
	if !found {
		// Distinguish "not found" from "wrong kind" by checking base table.
		return providence.HumanAgent{}, fmt.Errorf("%w: human agent %q not found — "+
			"the agent may not exist or may be a different kind (ML or software)",
			providence.ErrNotFound, id.String())
	}
	return ha, nil
}

// GetMLAgent returns an ML agent by ID.
func GetMLAgent(db *DB, id providence.AgentID) (providence.MLAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var mla providence.MLAgent
	var found bool
	err := sqlitex.Execute(db.conn,
		`SELECT a.kind_id, m.role_id, ml.id, ml.provider_id, ml.name
		 FROM agents a
		 JOIN agents_ml m ON a.id = m.agent_id
		 JOIN ml_models ml ON m.model_id = ml.id
		 WHERE a.id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				mla = providence.MLAgent{
					Agent: providence.Agent{ID: id, Kind: providence.AgentKindMachineLearning},
					Role:  providence.Role(stmt.ColumnInt(1)),
					Model: providence.MLModel{
						ID:       stmt.ColumnInt(2),
						Provider: providence.Provider(stmt.ColumnInt(3)),
						Name:     stmt.ColumnText(4),
					},
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return providence.MLAgent{}, fmt.Errorf("sqlite.GetMLAgent: %w", err)
	}
	if !found {
		return providence.MLAgent{}, fmt.Errorf("%w: ML agent %q not found — "+
			"the agent may not exist or may be a different kind (human or software)",
			providence.ErrNotFound, id.String())
	}
	return mla, nil
}

// GetSoftwareAgent returns a software agent by ID.
func GetSoftwareAgent(db *DB, id providence.AgentID) (providence.SoftwareAgent, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var sa providence.SoftwareAgent
	var found bool
	err := sqlitex.Execute(db.conn,
		`SELECT a.kind_id, s.name, s.version, s.source
		 FROM agents a JOIN agents_software s ON a.id = s.agent_id
		 WHERE a.id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{id.String()},
			ResultFunc: func(stmt *zs.Stmt) error {
				sa = providence.SoftwareAgent{
					Agent:   providence.Agent{ID: id, Kind: providence.AgentKindSoftware},
					Name:    stmt.ColumnText(1),
					Version: stmt.ColumnText(2),
					Source:  stmt.ColumnText(3),
				}
				found = true
				return nil
			},
		})
	if err != nil {
		return providence.SoftwareAgent{}, fmt.Errorf("sqlite.GetSoftwareAgent: %w", err)
	}
	if !found {
		return providence.SoftwareAgent{}, fmt.Errorf("%w: software agent %q not found — "+
			"the agent may not exist or may be a different kind (human or ML)",
			providence.ErrNotFound, id.String())
	}
	return sa, nil
}
