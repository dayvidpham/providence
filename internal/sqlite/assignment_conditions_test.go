package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/provenance/internal/fusedtx"
	"github.com/dayvidpham/provenance/internal/journal"
)

type countedAssignmentReader struct {
	fusedtx.SQLReader
	queries int
}

func (r *countedAssignmentReader) QueryRow(ctx context.Context, query string, args ...any) fusedtx.Row {
	r.queries++
	return r.SQLReader.QueryRow(ctx, query, args...)
}

func TestAssignmentActiveIndexedLineageBudget(t *testing.T) {
	env := newFactCondEnv(t)
	var parent journal.AssignmentID
	var assertions []journal.AssignmentActiveAssertion
	for depth := 1; depth <= 16; depth++ {
		assignment := journal.AssignmentID(fmt.Sprintf("depth-%d", depth))
		result, err := env.db.Apply(journal.OperationInput{
			OperationID: journal.OperationID(assignment), ActorID: env.act,
			AuthorityJournalID: &env.boot, CommandDigest: []byte(assignment),
			Effects: []journal.Effect{{Sort: journal.EffectAssignmentStart,
				AssignmentID: assignment, TaskID: env.task, Occupant: env.act,
				SlotID: journal.SlotOwnerResponsibility, Parent: parent, ResultSlot: "authority"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		assertion := journal.AssignmentActiveAssertion{
			AssignmentID: assignment, TaskID: env.task, Occupant: env.act,
			SlotID: journal.SlotOwnerResponsibility, AuthorityJournalID: result.ResultSlots[0].ProducedJournalID,
		}
		if parent != "" {
			value := parent
			assertion.ParentAssignmentID = &value
		}
		assertions = append(assertions, assertion)
		parent = assignment
	}
	scope, err := env.db.bindScope(t.Context(), projectionTargetLive)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.release()
	err = runScopedTransaction(scope.ctx, scope.conn, "BEGIN IMMEDIATE", func() error {
		for _, depth := range []int{1, 4, 16} {
			reader := &countedAssignmentReader{SQLReader: allocationSQLTx{conn: scope.conn}}
			conditions := make([]journal.Condition, journal.MaxCanonicalConditions)
			for i := range conditions {
				conditions[i] = journal.AssignmentActiveCondition(assertions[depth-1])
			}
			if err := checkConditionsInTransaction(scope.ctx, reader, conditions, func(factContextRelation, journal.JournalID) error {
				t.Fatal("assignment condition entered fact verification")
				return nil
			}); err != nil {
				return err
			}
			if reader.queries != journal.MaxCanonicalConditions*depth {
				t.Fatalf("depth %d: got %d queries, want %d indexed episode probes", depth, reader.queries, journal.MaxCanonicalConditions*depth)
			}
			t.Logf("64 assertions at depth %d: %d indexed lineage queries", depth, reader.queries)
		}
		rows, err := scope.conn.QueryContext(scope.ctx, "EXPLAIN QUERY PLAN "+assignmentConditionEpisodeSQL,
			string(parent), transitionStartedID, transitionEndedID, int(journal.AuthorityKindAssignment), int(journal.JournalKindAuthority))
		if err != nil {
			return err
		}
		defer rows.Close()
		searches := 0
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				return err
			}
			t.Log(detail)
			if strings.Contains(detail, "SCAN ") {
				t.Fatalf("exact assertion query performed a scan: %s", detail)
			}
			if strings.Contains(detail, "SEARCH ") {
				searches++
			}
		}
		if searches != 5 {
			t.Fatalf("query plan has %d searches, want episode/start/end/authority/journal indexed probes", searches)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
