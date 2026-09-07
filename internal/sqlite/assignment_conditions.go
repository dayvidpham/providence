package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dayvidpham/provenance/internal/fusedtx"
	"github.com/dayvidpham/provenance/internal/journal"
)

// assignmentConditionEpisodeSQL visits only the requested episode, its unique
// start/end transitions, and the start's primary-key authority/journal rows.
// It never substitutes another assignment on the same task. One execution per
// lineage depth is required; MaxCanonicalConditions bounds assertion count, not
// depth. No new schema, read connection, transaction, or process lock is involved.
const assignmentConditionEpisodeSQL = `SELECT episode.task_id, episode.slot_id,
	episode.actor_id, episode.parent_assignment_id, started.journal_id,
	EXISTS(SELECT 1 FROM journal_authority_assignment_transitions ended
	       WHERE ended.assignment_id=episode.assignment_id AND ended.transition_id=?3)
	FROM journal_authority_assignment_episodes episode
	JOIN journal_authority_assignment_transitions started
	  ON started.assignment_id=episode.assignment_id AND started.transition_id=?2
	JOIN journal_authorities authority
	  ON authority.journal_id=started.journal_id AND authority.authority_kind_id=?4
	JOIN journal start_row
	  ON start_row.journal_id=started.journal_id AND start_row.kind_id=?5
	WHERE episode.assignment_id=?1`

func checkAssignmentActiveInTransaction(ctx context.Context, reader fusedtx.SQLReader, condition journal.Condition, index int) error {
	a := condition.AssignmentActive
	failure := &journal.ConditionFailure{
		Index: index, Kind: journal.ConditionAssignmentActive,
		Reason: journal.ConditionAssignmentInactive,
	}
	if a == nil {
		return failure
	}
	failure.AssertedJournalID = a.AuthorityJournalID
	current := a.AssignmentID
	var childStart int64
	for {
		var task, actor string
		var slot int
		var parent sql.NullString
		var start int64
		var ended bool
		err := reader.QueryRow(ctx, assignmentConditionEpisodeSQL, string(current),
			transitionStartedID, transitionEndedID, int(journal.AuthorityKindAssignment), int(journal.JournalKindAuthority)).Scan(
			&task, &slot, &actor, &parent, &start, &ended)
		if fusedtx.IsNoRows(err) {
			return failure
		}
		if err != nil {
			return fmt.Errorf("condition[%d] exact assignment %q lineage lookup failed inside Apply transaction; nothing committed; repair the store or retry the database error: %w", index, current, err)
		}
		if ended || start <= 0 || slot != slotOwnerResponsibilityID {
			return failure
		}
		if childStart == 0 {
			if journal.JournalID(start) != a.AuthorityJournalID || task != a.TaskID.String() || actor != a.Occupant.String() ||
				a.SlotID != journal.SlotOwnerResponsibility || parent.Valid != (a.ParentAssignmentID != nil) {
				return failure
			}
			if parent.Valid && parent.String != string(*a.ParentAssignmentID) {
				return failure
			}
		} else if start >= childStart {
			// Every cited ancestor must predate its child. Strict descent both
			// authenticates the citation and terminates corrupt cyclic chains.
			return failure
		}
		if !parent.Valid {
			return nil
		}
		if err := journal.ValidateOperationID(journal.OperationID(parent.String)); err != nil {
			return failure
		}
		current = journal.AssignmentID(parent.String)
		childStart = start
	}
}
