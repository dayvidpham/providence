package journal

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/dayvidpham/provenance/pkg/ptypes"
)

// AssignmentActiveAssertion identifies one immutable assignment-start episode.
// A nil ParentAssignmentID asserts absence of a parent, not a wildcard. Activity
// includes every cited ancestor at condition evaluation, before new effects.
// This predicate neither grants delegation nor consumes the assignment.
type AssignmentActiveAssertion struct {
	AssignmentID       AssignmentID
	TaskID             TaskID
	SlotID             AssignmentSlotID
	Occupant           ActorID
	AuthorityJournalID JournalID
	ParentAssignmentID *AssignmentID
}

// AssignmentActiveCondition constructs the disjoint assignment condition arm.
// Canonicalize/Apply validate it; construction makes no preflight database read.
func AssignmentActiveCondition(assertion AssignmentActiveAssertion) Condition {
	if assertion.ParentAssignmentID != nil {
		parent := *assertion.ParentAssignmentID
		assertion.ParentAssignmentID = &parent
	}
	return Condition{Kind: ConditionAssignmentActive, AssignmentActive: &assertion}
}

func normalizeAssignmentCondition(in Condition, index int) (Condition, error) {
	invalid := func(reason string) (Condition, error) {
		return Condition{}, canonicalMutationError(conditionName(index, "assignment-active"), reason,
			"supply only AssignmentActive with canonical assignment/task/occupant IDs, SlotOwnerResponsibility, positive start authority, and nil or exact nonempty parent")
	}
	selector := in.Selector
	if in.AssertedJournalID != 0 || selector.Kind != 0 || selector.DecisionKind != "" || selector.EvidenceKind != "" ||
		selector.Filter.TaskScope != (FactTaskScope{}) || len(selector.Filter.RequiredContexts) != 0 ||
		len(selector.Filter.EffectiveActorIDs) != 0 || len(selector.Filter.OperationIDs) != 0 {
		return invalid("assignment conditions cannot carry fact selector or asserted fact journal fields")
	}
	if in.AssignmentActive == nil {
		return invalid("assignment assertion is missing")
	}
	a := *in.AssignmentActive
	if err := ValidateOperationID(OperationID(a.AssignmentID)); err != nil {
		return invalid("assignment ID: " + err.Error())
	}
	if err := validateTaskID(a.TaskID); err != nil {
		return invalid("task ID: " + err.Error())
	}
	if err := validateActorID(a.Occupant); err != nil {
		return invalid("occupant: " + err.Error())
	}
	if a.SlotID != SlotOwnerResponsibility || a.AuthorityJournalID <= 0 {
		return invalid("unknown slot or nonpositive start authority")
	}
	if a.ParentAssignmentID != nil {
		if err := ValidateOperationID(OperationID(*a.ParentAssignmentID)); err != nil {
			return invalid("parent assignment ID: " + err.Error())
		}
		if *a.ParentAssignmentID == a.AssignmentID {
			return invalid(fmt.Sprintf("assignment %q cannot be its own parent", a.AssignmentID))
		}
	}
	return AssignmentActiveCondition(a), nil
}

// This wire DTO is deliberately independent of Go's exported API field names.
// Every member is required, including an explicit null for the no-parent arm.
type assignmentAssertionWire struct {
	AssignmentID       string  `json:"assignment_id"`
	TaskID             string  `json:"task_id"`
	SlotID             string  `json:"slot_id"`
	Occupant           string  `json:"occupant"`
	AuthorityJournalID int64   `json:"authority_journal_id"`
	ParentAssignmentID *string `json:"parent_assignment_id"`
}

func encodeAssignmentAssertion(a AssignmentActiveAssertion) ([]byte, error) {
	wire := assignmentAssertionWire{
		AssignmentID: string(a.AssignmentID), TaskID: a.TaskID.String(),
		SlotID: string(a.SlotID), Occupant: a.Occupant.String(),
		AuthorityJournalID: int64(a.AuthorityJournalID),
	}
	if a.ParentAssignmentID != nil {
		parent := string(*a.ParentAssignmentID)
		wire.ParentAssignmentID = &parent
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	return mutationV1Codec.canonicalJSON(payload)
}

func decodeAssignmentAssertion(payload []byte) (*AssignmentActiveAssertion, error) {
	// The common canonical JSON boundary refuses duplicate fields and trailing
	// values. Whole-mutation re-encoding also rejects missing/null scalar fields,
	// omitted parent, noncanonical spelling, and alternate numeric encodings.
	if _, err := mutationV1Codec.canonicalJSON(payload); err != nil {
		return nil, err
	}
	var wire assignmentAssertionWire
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	task, err := ptypes.ParseTaskID(wire.TaskID)
	if err != nil {
		return nil, err
	}
	actor, err := ptypes.ParseActorID(wire.Occupant)
	if err != nil {
		return nil, err
	}
	a := AssignmentActiveAssertion{
		AssignmentID: AssignmentID(wire.AssignmentID), TaskID: task,
		SlotID: AssignmentSlotID(wire.SlotID), Occupant: actor,
		AuthorityJournalID: JournalID(wire.AuthorityJournalID),
	}
	if wire.ParentAssignmentID != nil {
		parent := AssignmentID(*wire.ParentAssignmentID)
		a.ParentAssignmentID = &parent
	}
	return &a, nil
}
