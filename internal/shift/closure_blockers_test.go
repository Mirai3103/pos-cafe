package shift

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/stretchr/testify/require"
)

// TestClosureBlockerPrecedence pins spec 8 as Phase 08 extends it: Awaiting
// Submission reports ahead of the generic active-Session blocker and behind
// every money blocker.
func TestClosureBlockerPrecedence(t *testing.T) {
	require.NoError(t, closureBlockerErr(sqlc.GetGlobalShiftClosureBlockersRow{}))

	awaiting := sqlc.GetGlobalShiftClosureBlockersRow{
		AwaitingSubmissionCount:   1,
		ActiveServiceSessionCount: 1,
	}
	require.ErrorIs(t, closureBlockerErr(awaiting), ErrAwaitingSubmission)

	awaiting.UnresolvedCorrectionVnd = 1
	require.ErrorIs(t, closureBlockerErr(awaiting), ErrUnresolvedCorrection)
}
