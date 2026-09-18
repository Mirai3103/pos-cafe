package sales

import "github.com/google/uuid"

// ClosureReadiness is the verdict on whether a Service Session may close,
// carrying the failing detail alongside it so the API can say what is
// outstanding rather than only that something is.
type ClosureReadiness struct {
	Eligible bool

	AllChecksSettled   bool
	AllRefundsResolved bool
	AllWorkSubmitted   bool
	HasOrder           bool
	AllPreparationDone bool

	UnsettledCheckIDs           []uuid.UUID
	PendingRefundCheckIDs       []uuid.UUID
	UnsubmittedCommittedItemIDs []uuid.UUID
	NonterminalUnitIDs          []uuid.UUID
}

// EvaluateClosureReadiness is the one closure policy boundary, so the API's
// answer and any client's preview cannot drift apart. It is pure: every input
// it needs already travels in the Service Session projection, which is why 5D
// exposes no readiness endpoint.
//
// A Check carrying a positive PendingRefundVND is settled debt plus money the
// system still owes back. Check state records only the debt side (ADR-044), so
// closure is what enforces resolution: the Session may not close while any
// Check still owes the customer.
func EvaluateClosureReadiness(session ServiceSessionResponse) ClosureReadiness {
	out := ClosureReadiness{
		UnsettledCheckIDs:           make([]uuid.UUID, 0),
		PendingRefundCheckIDs:       make([]uuid.UUID, 0),
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
		if check.PendingRefundVND > 0 {
			out.PendingRefundCheckIDs = append(out.PendingRefundCheckIDs, check.ID)
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
	out.AllRefundsResolved = len(out.PendingRefundCheckIDs) == 0
	out.HasOrder = len(session.Orders) > 0
	out.AllWorkSubmitted = out.HasOrder && len(out.UnsubmittedCommittedItemIDs) == 0
	out.AllPreparationDone = out.HasOrder && len(out.NonterminalUnitIDs) == 0
	out.Eligible = out.AllChecksSettled && out.AllRefundsResolved &&
		out.AllWorkSubmitted && out.AllPreparationDone

	return out
}

// Err returns the first unmet condition as a domain error, or nil when the
// Session may close. The order is load-bearing: staff fix what they are told
// about first, so it decides which of several outstanding problems they are
// sent to resolve — unsettled money, then money owed back, then work, then the
// missing Order, then the bar.
func (r ClosureReadiness) Err() error {
	switch {
	case !r.AllChecksSettled:
		return ErrCheckNotSettledForClosure
	case !r.AllRefundsResolved:
		return ErrPendingRefundForClosure
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
