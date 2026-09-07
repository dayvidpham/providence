package provenance_test

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/dayvidpham/provenance"
)

func openAssignmentConditionTracker(t *testing.T, borrowed bool) (provenance.Tracker, provenance.ActorID, *sql.DB) {
	t.Helper()
	if !borrowed {
		return openGovernedTrackerWithDatabase(t)
	}
	db, _ := openFileDB(t)
	tr, err := provenance.OpenBorrowedSQLite(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr, registerGovernedActor(t, tr, "assignment-condition"), db
}

func activeAssertionFor(child provenance.GovernedChildBinding, actor provenance.ActorID, parent *provenance.AssignmentID) provenance.AssignmentActiveAssertion {
	return provenance.AssignmentActiveAssertion{
		AssignmentID: child.AssignmentID, TaskID: child.TaskID,
		SlotID: provenance.SlotOwnerResponsibility, Occupant: actor,
		AuthorityJournalID: child.AssignmentRow.JournalID, ParentAssignmentID: parent,
	}
}

func endConditionAssignment(t *testing.T, tr provenance.Tracker, actor provenance.ActorID, authority provenance.JournalID, assignment provenance.AssignmentID, operation provenance.OperationID) {
	t.Helper()
	_, err := tr.Journal().Apply(provenance.OperationInput{
		OperationID: operation, ActorID: actor, AuthorityJournalID: &authority,
		CommandDigest: []byte(operation),
		Effects:       []provenance.Effect{{Sort: provenance.EffectAssignmentEnd, AssignmentID: assignment}},
	})
	if err != nil {
		t.Fatalf("end exact action through public Apply: %v", err)
	}
}

// Count every physical application table, including subtype, projection, slot,
// and allocation rows. This is test-only observation, never caller authorization.
func assignmentConditionCounts(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int64, len(names))
	for _, name := range names {
		var count int64
		if err := db.QueryRow(`SELECT count(*) FROM "` + name + `"`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[name] = count
	}
	return counts
}

func requireAssignmentConditionAbsent(t *testing.T, tr provenance.Tracker, db *sql.DB, operation provenance.OperationID, before map[string]int64, err error) {
	t.Helper()
	var failure *provenance.ConditionFailure
	if !errors.As(err, &failure) || failure.Kind != provenance.ConditionAssignmentActive || failure.Reason != provenance.ConditionAssignmentInactive {
		t.Fatalf("want exact assignment condition failure, got %v", err)
	}
	lookup, err := tr.Journal().LookupCommitted(operation)
	if err != nil || lookup.Kind != provenance.CommittedAbsent {
		t.Fatalf("failed operation presence=%+v error=%v", lookup, err)
	}
	if after := assignmentConditionCounts(t, db); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed assertion wrote partial rows: before=%v after=%v", before, after)
	}
}

func TestAssignmentActiveApplyExactEpisodeAndReplay(t *testing.T) {
	for _, storage := range []struct {
		name     string
		borrowed bool
	}{{"direct", false}, {"borrowed", true}} {
		t.Run(storage.name, func(t *testing.T) {
			tr, actor, db := openAssignmentConditionTracker(t, storage.borrowed)
			root := initializeRoot(t, tr, actor)
			closure, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGoverned(t.Context(), governedRequest("action", actor, root.AssignmentID, 1))
			if err != nil {
				t.Fatal(err)
			}
			action := closure.Children()[0]
			assertion := activeAssertionFor(action, actor, &root.AssignmentID)
			input := provenance.OperationInput{
				OperationID: "guarded", ActorID: actor, AuthorityJournalID: &root.AssignmentRow.JournalID,
				CommandDigest: []byte("guarded"),
				Conditions:    []provenance.Condition{provenance.AssignmentActiveCondition(assertion)},
				Effects:       []provenance.Effect{{Sort: provenance.EffectEvidence, TaskID: action.TaskID, EvidenceKind: "test.submission", ContentDigest: []byte("submission"), Payload: []byte(`{"accepted":true}`), ResultSlot: "submission"}},
			}
			first, err := tr.Journal().Apply(input)
			if err != nil {
				t.Fatalf("active exact assignment must permit parent-authorized effects: %v", err)
			}
			if len(first.ResultSlots) != 1 {
				t.Fatalf("submission slot missing: %+v", first)
			}
			selector := provenance.FactSelector{
				Kind: provenance.FactEvidence, EvidenceKind: "test.submission",
				Filter: provenance.FactFilter{TaskScope: provenance.FactTaskScope{Kind: provenance.FactTaskExact, TaskID: action.TaskID}},
			}
			legacy := input
			legacy.OperationID = "legacy-facts"
			legacy.Conditions = []provenance.Condition{
				{Kind: provenance.ConditionExactFact, Selector: selector, AssertedJournalID: first.ResultSlots[0].ProducedJournalID},
				{Kind: provenance.ConditionCurrentFact, Selector: selector, AssertedJournalID: first.ResultSlots[0].ProducedJournalID},
			}
			legacyPrepared, err := provenance.Canonicalize(legacy)
			if err != nil || legacyPrepared.EncodingVersion() != provenance.MutationEncodingV1 {
				t.Fatalf("old fact-only operation promoted: %v", err)
			}
			legacyFirst, err := tr.Journal().Apply(legacy)
			if err != nil {
				t.Fatalf("commit legacy fact conditions: %v", err)
			}
			// An alternate axis-equivalent episode keeps the parent's task reachability
			// alive. Only the original exact action must control the new submission.
			_, err = tr.Journal().Apply(provenance.OperationInput{
				OperationID: "alternate", ActorID: actor, AuthorityJournalID: &root.AssignmentRow.JournalID,
				CommandDigest: []byte("alternate"),
				Effects:       []provenance.Effect{{Sort: provenance.EffectAssignmentStart, AssignmentID: "alternate-action", TaskID: action.TaskID, SlotID: provenance.SlotOwnerResponsibility, Occupant: actor, Parent: root.AssignmentID}},
			})
			if err != nil {
				t.Fatal(err)
			}
			active, err := tr.Journal().AuthorityGovernsTaskAt(assertion.AuthorityJournalID, action.TaskID, provenance.JournalID(1<<63-1))
			if err != nil || !active {
				t.Fatalf("real preflight failed: active=%v err=%v", active, err)
			}
			var path string
			if err := db.QueryRow(`SELECT file FROM pragma_database_list WHERE name='main'`).Scan(&path); err != nil {
				t.Fatal(err)
			}
			// A separate pool commits the revocation after the original reader's
			// preflight. Completion, not a sleep, orders the later guarded Apply.
			writer, err := provenance.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			endConditionAssignment(t, writer, actor, root.AssignmentRow.JournalID, action.AssignmentID, "revoke-after-preflight")
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			parentActive, err := tr.Journal().AuthorityGovernsTaskAt(root.AssignmentRow.JournalID, action.TaskID, provenance.JournalID(1<<63-1))
			if err != nil || !parentActive {
				t.Fatalf("alternate must retain parent governance: active=%v err=%v", parentActive, err)
			}
			before := assignmentConditionCounts(t, db)
			replay, err := tr.Journal().Apply(input)
			first.ShortCircuited = true
			if err != nil || !reflect.DeepEqual(replay, first) {
				t.Fatalf("exact replay after revocation: got=%+v want=%+v err=%v", replay, first, err)
			}
			legacyReplay, err := tr.Journal().Apply(legacy)
			legacyFirst.ShortCircuited = true
			if err != nil || !reflect.DeepEqual(legacyReplay, legacyFirst) {
				t.Fatalf("legacy V1 replay after assignment revocation and fact replacement: %v", err)
			}
			var storedBytes, storedDigest []byte
			if err := db.QueryRow(`SELECT canonical_mutation, mutation_digest FROM journal_operations WHERE operation_id=?1`, string(legacy.OperationID)).Scan(&storedBytes, &storedDigest); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(storedBytes, legacyPrepared.CanonicalBytes()) || !reflect.DeepEqual(storedDigest, legacyPrepared.DerivedDigest()) {
				t.Fatal("legacy V1 bytes or digest changed on replay")
			}
			if after := assignmentConditionCounts(t, db); !reflect.DeepEqual(after, before) {
				t.Fatal("exact replay wrote rows")
			}
			changed := input
			changed.Conditions = nil
			if _, err := tr.Journal().Apply(changed); !errors.Is(err, provenance.ErrOperationConflict) {
				t.Fatalf("removing assertion under same operation must conflict: %v", err)
			}
			changedAssertion := assertion
			changedAssertion.AssignmentID = "alternate-action"
			changed.Conditions = []provenance.Condition{provenance.AssignmentActiveCondition(changedAssertion)}
			if _, err := tr.Journal().Apply(changed); !errors.Is(err, provenance.ErrOperationConflict) {
				t.Fatalf("changed assertion must conflict before mutable checks: %v", err)
			}
			input.OperationID = "stale-submission"
			_, err = tr.Journal().Apply(input)
			requireAssignmentConditionAbsent(t, tr, db, input.OperationID, before, err)
			if err := tr.Journal().VerifyIntegrity(); err != nil {
				t.Fatalf("V1/V2 mixed history integrity after revocation: %v", err)
			}
			reopened, err := provenance.OpenSQLite(path)
			if err != nil {
				t.Fatalf("reopen mixed V1/V2 history: %v", err)
			}
			defer reopened.Close()
			if _, err := reopened.Journal().Apply(legacy); err != nil {
				t.Fatalf("legacy exact replay through reopened reader: %v", err)
			}
		})
	}
}

func TestAssignmentActiveIdentityAndAncestry(t *testing.T) {
	tr, actor, db := openAssignmentConditionTracker(t, true)
	root := initializeRoot(t, tr, actor)
	closure, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGoverned(t.Context(), governedRequest("identity-action", actor, root.AssignmentID, 1))
	if err != nil {
		t.Fatal(err)
	}
	action := closure.Children()[0]
	valid := activeAssertionFor(action, actor, &root.AssignmentID)
	otherActor := registerGovernedActor(t, tr, "other-occupant")
	for _, test := range []struct {
		name   string
		change func(*provenance.AssignmentActiveAssertion)
	}{
		{"slot", func(a *provenance.AssignmentActiveAssertion) { a.SlotID = "axis-reviewer" }},
		{"zero-authority", func(a *provenance.AssignmentActiveAssertion) { a.AuthorityJournalID = 0 }},
		{"missing-assignment", func(a *provenance.AssignmentActiveAssertion) { a.AssignmentID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertion := valid
			test.change(&assertion)
			input := provenance.OperationInput{
				OperationID: provenance.OperationID("malformed-" + test.name), ActorID: actor,
				AuthorityJournalID: &root.AssignmentRow.JournalID, CommandDigest: []byte("malformed"),
				Conditions: []provenance.Condition{provenance.AssignmentActiveCondition(assertion)},
			}
			before := assignmentConditionCounts(t, db)
			if _, err := tr.Journal().Apply(input); !errors.Is(err, provenance.ErrCanonicalMutation) {
				t.Fatalf("malformed identity must fail before write admission: %v", err)
			}
			lookup, err := tr.Journal().LookupCommitted(input.OperationID)
			if err != nil || lookup.Kind != provenance.CommittedAbsent || !reflect.DeepEqual(before, assignmentConditionCounts(t, db)) {
				t.Fatalf("malformed identity wrote durable state: %+v %v", lookup, err)
			}
		})
	}
	for _, test := range []struct {
		name   string
		change func(*provenance.AssignmentActiveAssertion)
	}{
		{"assignment", func(a *provenance.AssignmentActiveAssertion) { a.AssignmentID = "absent" }},
		{"authority", func(a *provenance.AssignmentActiveAssertion) { a.AuthorityJournalID = root.AssignmentRow.JournalID }},
		{"task", func(a *provenance.AssignmentActiveAssertion) { a.TaskID = root.TaskID }},
		{"occupant", func(a *provenance.AssignmentActiveAssertion) { a.Occupant = otherActor }},
		{"parent-presence", func(a *provenance.AssignmentActiveAssertion) { a.ParentAssignmentID = nil }},
		{"parent-id", func(a *provenance.AssignmentActiveAssertion) {
			parent := provenance.AssignmentID("absent-parent")
			a.ParentAssignmentID = &parent
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertion := valid
			test.change(&assertion)
			input := provenance.OperationInput{
				OperationID: provenance.OperationID("wrong-" + test.name), ActorID: actor, AuthorityJournalID: &root.AssignmentRow.JournalID,
				CommandDigest: []byte("identity"),
				Conditions:    []provenance.Condition{provenance.AssignmentActiveCondition(assertion)},
				Effects:       []provenance.Effect{{Sort: provenance.EffectEvidence, TaskID: action.TaskID, EvidenceKind: "test.identity", ContentDigest: []byte("identity")}},
			}
			before := assignmentConditionCounts(t, db)
			_, err := tr.Journal().Apply(input)
			requireAssignmentConditionAbsent(t, tr, db, input.OperationID, before, err)
		})
	}
	// Root has no parent; nil is a positive exact assertion, not omitted identity.
	rootInput := provenance.OperationInput{
		OperationID: "nil-parent", ActorID: actor, AuthorityJournalID: &root.AssignmentRow.JournalID,
		CommandDigest: []byte("nil-parent"),
		Conditions:    []provenance.Condition{provenance.AssignmentActiveCondition(activeAssertionFor(root, actor, nil))},
		Effects:       []provenance.Effect{{Sort: provenance.EffectEvidence, TaskID: root.TaskID, EvidenceKind: "test.root", ContentDigest: []byte("root")}},
	}
	if _, err := tr.Journal().Apply(rootInput); err != nil {
		t.Fatalf("exact nil parent failed: %v", err)
	}
	// A direct authority on its own task used to bypass ancestry in ordinary
	// governance. This independent assertion must reject the revoked root chain.
	endConditionAssignment(t, tr, actor, root.AssignmentRow.JournalID, root.AssignmentID, "revoke-ancestor")
	input := provenance.OperationInput{
		OperationID: "revoked-ancestor", ActorID: actor, AuthorityJournalID: &action.AssignmentRow.JournalID,
		CommandDigest: []byte("revoked-ancestor"),
		Conditions:    []provenance.Condition{provenance.AssignmentActiveCondition(valid)},
		Effects:       []provenance.Effect{{Sort: provenance.EffectEvidence, TaskID: action.TaskID, EvidenceKind: "test.ancestry", ContentDigest: []byte("ancestry")}},
	}
	before := assignmentConditionCounts(t, db)
	_, err = tr.Journal().Apply(input)
	requireAssignmentConditionAbsent(t, tr, db, input.OperationID, before, err)
}

func TestAssignmentActiveTransferredEpisodeCannotBeReplaced(t *testing.T) {
	tr, actor, db := openAssignmentConditionTracker(t, true)
	root := initializeRoot(t, tr, actor)
	closure, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGoverned(t.Context(), governedRequest("transfer-action", actor, root.AssignmentID, 1))
	if err != nil {
		t.Fatal(err)
	}
	action := closure.Children()[0]
	assertion := activeAssertionFor(action, actor, &root.AssignmentID)
	_, err = tr.As(actor, action.AssignmentRow.JournalID).TransferAssignment(provenance.AssignmentTransferRequest{
		TaskID: action.TaskID, SlotID: provenance.SlotOwnerResponsibility,
		PreviousAssignmentID: action.AssignmentID, NextAssignmentID: "successor", NextOccupant: actor,
	}, provenance.WithOperationID("transfer-exact-action"))
	if err != nil {
		t.Fatal(err)
	}
	input := provenance.OperationInput{
		OperationID: "transferred-assertion", ActorID: actor, AuthorityJournalID: &root.AssignmentRow.JournalID,
		CommandDigest: []byte("transferred"), Conditions: []provenance.Condition{provenance.AssignmentActiveCondition(assertion)},
		Effects: []provenance.Effect{{Sort: provenance.EffectEvidence, TaskID: action.TaskID, EvidenceKind: "test.transfer", ContentDigest: []byte("transfer")}},
	}
	before := assignmentConditionCounts(t, db)
	_, err = tr.Journal().Apply(input)
	requireAssignmentConditionAbsent(t, tr, db, input.OperationID, before, err)
}

func TestAssignmentActiveComposedTransaction(t *testing.T) {
	tr, actor, db := openAssignmentConditionTracker(t, false)
	root := initializeRoot(t, tr, actor)
	actionClosure, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGoverned(t.Context(), governedRequest("composed-action", actor, root.AssignmentID, 1))
	if err != nil {
		t.Fatal(err)
	}
	action := actionClosure.Children()[0]
	condition := provenance.AssignmentActiveCondition(activeAssertionFor(action, actor, &root.AssignmentID))
	request := composedGovernedRequest("active-composed", actor, root, 1)
	request.Conditions = []provenance.Condition{condition}
	first, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGovernedComposed(t.Context(), request)
	if err != nil {
		t.Fatalf("real composed active assertion: %v", err)
	}
	endConditionAssignment(t, tr, actor, root.AssignmentRow.JournalID, action.AssignmentID, "end-composed-action")
	before := assignmentConditionCounts(t, db)
	replay, err := tr.As(actor, root.AssignmentRow.JournalID).AllocateGovernedComposed(t.Context(), request)
	if err != nil || !first.Closure().Equal(replay.Closure()) || !reflect.DeepEqual(first.SupplementalResultSlots(), replay.SupplementalResultSlots()) {
		t.Fatalf("composed exact replay after revocation: %v", err)
	}
	if after := assignmentConditionCounts(t, db); !reflect.DeepEqual(after, before) {
		t.Fatal("composed exact replay wrote rows")
	}
	stale := composedGovernedRequest("stale-composed", actor, root, 1)
	stale.Conditions = []provenance.Condition{condition}
	_, err = tr.As(actor, root.AssignmentRow.JournalID).AllocateGovernedComposed(t.Context(), stale)
	requireAssignmentConditionAbsent(t, tr, db, provenance.GovernedAllocationSupplementOperationID(stale.Allocation.OperationID), before, err)
	assertGovernedOperationAbsent(t, tr, stale.Allocation.OperationID)
}
