package provenance

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dayvidpham/provenance/internal/journal"
)

func TestDBOSAssignmentActiveConditionFailureRoundTrip(t *testing.T) {
	stack := newInternalDBOSStack(t, "dbos-assignment-condition")
	callbacks := 0
	stack.adapter.testHooks.onWorkflowEntry = func() { callbacks++ }
	operation := stack.operation("assignment-condition-absent")
	operation.Conditions = []Condition{AssignmentActiveCondition(AssignmentActiveAssertion{
		AssignmentID: "missing-assignment", TaskID: testTaskID(t),
		SlotID: SlotOwnerResponsibility, Occupant: stack.actor,
		AuthorityJournalID: stack.authority,
	})}
	contract := newDBOSContractSnapshot()
	encoded, normalized, err := encodeApplyInput(contract, operation)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeApplyInput(contract, encoded)
	if err != nil || !reflect.DeepEqual(decoded.Conditions, normalized.Conditions) {
		t.Fatalf("DBOS input lost exact assignment assertion: %v", err)
	}
	_, err = stack.adapter.Apply(t.Context(), operation)
	var first *journal.ConditionFailure
	if !errors.As(err, &first) || first.Kind != ConditionAssignmentActive || first.Reason != ConditionAssignmentInactive {
		t.Fatalf("expected durable assignment condition domain failure: %v", err)
	}
	lookup, lookupErr := stack.tracker.Journal().LookupCommitted(operation.OperationID)
	if lookupErr != nil || lookup.Kind != CommittedAbsent {
		t.Fatalf("condition failure committed operation: %+v %v", lookup, lookupErr)
	}
	attempts := callbacks
	_, err = stack.adapter.Apply(t.Context(), operation)
	var replayed *journal.ConditionFailure
	if !errors.As(err, &replayed) || !reflect.DeepEqual(replayed, first) || callbacks != attempts {
		t.Fatalf("checkpoint failure replay changed or reexecuted: first=%+v replay=%+v callbacks=%d/%d error=%v", first, replayed, callbacks, attempts, err)
	}
}
