package sales

import "github.com/google/uuid"

// ClosureReadiness is the verdict on whether a Service Session may close,
// carrying the failing detail alongside it so the API can say what is
// outstanding rather than only that something is.
type ClosureReadiness struct {
	Eligible bool

	AllChecksSettled   bool
	AllWorkSubmitted   bool
	HasOrder           bool
	AllPreparationDone bool

	UnsettledCheckIDs           []uuid.UUID
	UnsubmittedCommittedItemIDs []uuid.UUID
	NonterminalUnitIDs          []uuid.UUID
}

// EvaluateClosureReadiness is the one closure policy boundary, so the API's
// answer and any client's preview cannot drift apart. It is pure: every input
// it needs already travels in the Service Session projection, which is why 5D
// exposes no readiness endpoint.
//
// The canonical function also reports Checks carrying a pending Refund. That
// branch is not migrated (ADR-029): Refund is outside Phase 5, the column it
// reads is absent from the contract, and a check with no data source behind it
// is a check that always passes.
func EvaluateClosureReadiness(session ServiceSessionResponse) ClosureReadiness {
	out := ClosureReadiness{
		UnsettledCheckIDs:           make([]uuid.UUID, 0),
		UnsubmittedCommittedItemIDs: make([]uuid.UUID, 0),
		NonterminalUnitIDs:          make([]uuid.UUID, 0),
	}

	survivingCheck := false
	for _, check := range session.Checks {
		if check.State != CheckStateMerged {
			survivingCheck = true
		}
		if check.State != CheckStateSettled && check.State != CheckStateMerged {
			out.UnsettledCheckIDs = append(out.UnsettledCheckIDs, check.ID)
		}
		for _, allocation := range check.Allocations {
			if !allocation.Submitted {
				out.UnsubmittedCommittedItemIDs = append(
					out.UnsubmittedCommittedItemIDs, allocation.CommittedItemID)
			}
		}
	}
	for _, unit := range session.PreparationUnits {
		if !IsTerminalUnitState(unit.State) {
			out.NonterminalUnitIDs = append(out.NonterminalUnitIDs, unit.ID)
		}
	}

	// survivingCheck matters on its own: a Session whose Checks were all
	// merged away has no surviving Check and has settled nothing, even though
	// no Check is OPEN.
	out.AllChecksSettled = survivingCheck && len(out.UnsettledCheckIDs) == 0
	out.HasOrder = len(session.Orders) > 0
	out.AllWorkSubmitted = out.HasOrder && len(out.UnsubmittedCommittedItemIDs) == 0
	out.AllPreparationDone = out.HasOrder && len(out.NonterminalUnitIDs) == 0
	out.Eligible = out.AllChecksSettled && out.AllWorkSubmitted && out.AllPreparationDone

	return out
}

// Err returns the first unmet condition as a domain error, or nil when the
// Session may close. The order is load-bearing: staff fix what they are told
// about first, so it decides which of several outstanding problems they are
// sent to resolve — money before work, and work before the bar.
func (r ClosureReadiness) Err() error {
	switch {
	case !r.AllChecksSettled:
		return ErrCheckNotSettledForClosure
	case len(r.UnsubmittedCommittedItemIDs) > 0:
		return ErrUnsubmittedWorkForClosure
	case !r.HasOrder:
		return ErrOrderRequiredForClosure
	case !r.AllPreparationDone:
		return ErrUnfulfilledPreparationForClosure
	default:
		return nil
	}
}
