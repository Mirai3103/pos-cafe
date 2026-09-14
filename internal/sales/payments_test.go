package sales_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/sales"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPayCashFingerprintIsStable(t *testing.T) {
	checkID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	requestID := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	first := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_000, CashTenderedVND: 100_000,
	})
	second := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_000, CashTenderedVND: 100_000,
	})
	require.Equal(t, first, second)

	different := sales.PayCashFingerprintFor(sales.PayCashCommand{
		RequestID: requestID, CheckID: checkID,
		AppliedAmountVND: 85_001, CashTenderedVND: 100_000,
	})
	require.NotEqual(t, first, different,
		"a different applied amount must be a different request")
}

func TestValidateCashAmounts(t *testing.T) {
	require.NoError(t, sales.ValidateCashAmounts(85_000, 100_000))
	require.Error(t, sales.ValidateCashAmounts(0, 100_000))
	require.Error(t, sales.ValidateCashAmounts(-1, 100_000))
	require.Error(t, sales.ValidateCashAmounts(85_000, 0))
}
