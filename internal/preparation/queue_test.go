package preparation

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Mirai3103/pos-cafe/internal/database/sqlc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDecodeModifiersNormalizesJSONNull(t *testing.T) {
	modifiers, err := decodeModifiers(json.RawMessage("null"))

	require.NoError(t, err)
	require.NotNil(t, modifiers)
	require.Empty(t, modifiers)
}

// wasteAlertRow builds one active WASTE alert row whose Waste fact resolved.
func wasteAlertRow(wasteID uuid.UUID) sqlc.ListActivePreparationAlertsRow {
	return sqlc.ListActivePreparationAlertsRow{
		ID:                uuid.New(),
		PreparationUnitID: uuid.New(),
		Kind:              AlertKindWaste,
		Reason:            ReasonQualityFailure,
		CreatedAt:         time.Date(2026, 9, 17, 8, 0, 0, 0, time.UTC),
		ServiceNumber:     "S20260917-0001",
		ItemName:          "Cà phê sữa",
		UnitNumber:        1,
		WasteID:           uuid.NullUUID{UUID: wasteID, Valid: true},
	}
}

func TestQueueAlertMapperResolvesWasteIDOnlyForWasteKind(t *testing.T) {
	wasteID := uuid.New()

	t.Run("a WASTE alert resolves its Waste fact", func(t *testing.T) {
		alert, err := queueAlertFromRow(wasteAlertRow(wasteID))
		require.NoError(t, err)
		require.Equal(t, AlertKindWaste, alert.Kind)
		require.NotNil(t, alert.WasteID)
		require.Equal(t, wasteID, *alert.WasteID)
		require.Nil(t, alert.Note)
		require.Nil(t, alert.AcknowledgedByStaffIdentityID)
		require.Nil(t, alert.AcknowledgedAt)
	})

	t.Run("reserved Cancellation kinds keep a null waste_id", func(t *testing.T) {
		for _, kind := range []string{AlertKindCancellation, AlertKindChange} {
			row := wasteAlertRow(wasteID)
			row.Kind = kind
			row.WasteID = uuid.NullUUID{}
			alert, err := queueAlertFromRow(row)
			require.NoError(t, err)
			require.Equal(t, kind, alert.Kind)
			require.Nil(t, alert.WasteID, "%s must project a null waste_id", kind)
		}
	})

	t.Run("a WASTE alert without a Waste fact is an internal error", func(t *testing.T) {
		row := wasteAlertRow(wasteID)
		row.WasteID = uuid.NullUUID{}
		_, err := queueAlertFromRow(row)
		require.Error(t, err, "a WASTE alert is written with its Waste fact; a null resolution is malformed, never a partial contract row")
	})
}

// wasteCorrectionRow builds one WASTE history row exactly as the generated
// UNION ALL query returns it.
func wasteCorrectionRow() sqlc.ListRecentPreparationCorrectionsRow {
	return sqlc.ListRecentPreparationCorrectionsRow{
		EntryKind:         correctionEntryWaste,
		FactID:            uuid.New(),
		PreparationUnitID: uuid.New(),
		UnitNumber:        3,
		ServiceNumber:     "S20260917-0002",
		ItemName:          "Cà phê sữa",
		Reason:            ReasonQualityFailure,
		OccurredAt:        time.Date(2026, 9, 17, 8, 1, 0, 0, time.UTC),
	}
}

func TestQueueCorrectionMapperMapsBothEntryKinds(t *testing.T) {
	t.Run("a WASTE row maps with nil remake linkage", func(t *testing.T) {
		row := wasteCorrectionRow()
		row.Note = sql.NullString{String: "ly vỡ", Valid: true}
		entry, err := queueCorrectionFromRow(row)
		require.NoError(t, err)
		require.Equal(t, correctionEntryWaste, entry.EntryKind)
		require.Equal(t, row.FactID, entry.ID)
		require.Equal(t, row.PreparationUnitID, entry.PreparationUnitID)
		require.Nil(t, entry.WasteID)
		require.Nil(t, entry.SourcePreparationUnitID)
		require.Nil(t, entry.SourceUnitNumber)
		require.Equal(t, row.UnitNumber, entry.UnitNumber)
		require.Equal(t, row.ServiceNumber, entry.ServiceNumber)
		require.Equal(t, row.ItemName, entry.ItemName)
		require.Equal(t, row.Reason, entry.Reason)
		require.NotNil(t, entry.Note)
		require.Equal(t, "ly vỡ", *entry.Note)
		require.Equal(t, row.OccurredAt, entry.OccurredAt)
	})

	t.Run("a REMAKE row maps its waste linkage", func(t *testing.T) {
		wasteID := uuid.New()
		sourceID := uuid.New()
		row := wasteCorrectionRow()
		row.EntryKind = correctionEntryRemake
		row.FactID = uuid.New()
		row.WasteID = uuid.NullUUID{UUID: wasteID, Valid: true}
		row.SourcePreparationUnitID = uuid.NullUUID{UUID: sourceID, Valid: true}
		row.SourceUnitNumber = sql.NullInt32{Int32: 1, Valid: true}
		entry, err := queueCorrectionFromRow(row)
		require.NoError(t, err)
		require.Equal(t, correctionEntryRemake, entry.EntryKind)
		require.Equal(t, row.FactID, entry.ID)
		require.NotNil(t, entry.WasteID)
		require.Equal(t, wasteID, *entry.WasteID)
		require.NotNil(t, entry.SourcePreparationUnitID)
		require.Equal(t, sourceID, *entry.SourcePreparationUnitID)
		require.NotNil(t, entry.SourceUnitNumber)
		require.Equal(t, int32(1), *entry.SourceUnitNumber)
	})
}

func TestQueueCorrectionMapperRejectsUnknownDiscriminator(t *testing.T) {
	row := wasteCorrectionRow()
	row.EntryKind = "CORRECTION"
	_, err := queueCorrectionFromRow(row)
	require.Error(t, err, "an unknown discriminator must be an internal error, never a partial contract row")
}

func TestQueueCorrectionMapperRejectsMalformedNullableCombinations(t *testing.T) {
	wasteID := uuid.New()
	sourceID := uuid.New()

	t.Run("a REMAKE row missing any linkage is an internal error", func(t *testing.T) {
		for name, mutate := range map[string]func(*sqlc.ListRecentPreparationCorrectionsRow){
			"null waste id":      func(r *sqlc.ListRecentPreparationCorrectionsRow) { r.WasteID = uuid.NullUUID{} },
			"null source id":     func(r *sqlc.ListRecentPreparationCorrectionsRow) { r.SourcePreparationUnitID = uuid.NullUUID{} },
			"null source number": func(r *sqlc.ListRecentPreparationCorrectionsRow) { r.SourceUnitNumber = sql.NullInt32{} },
		} {
			row := wasteCorrectionRow()
			row.EntryKind = correctionEntryRemake
			row.WasteID = uuid.NullUUID{UUID: wasteID, Valid: true}
			row.SourcePreparationUnitID = uuid.NullUUID{UUID: sourceID, Valid: true}
			row.SourceUnitNumber = sql.NullInt32{Int32: 1, Valid: true}
			mutate(&row)
			_, err := queueCorrectionFromRow(row)
			require.Error(t, err, "%s must reject the malformed REMAKE row", name)
		}
	})

	t.Run("a WASTE row carrying remake-only fields is an internal error", func(t *testing.T) {
		row := wasteCorrectionRow()
		row.WasteID = uuid.NullUUID{UUID: wasteID, Valid: true}
		_, err := queueCorrectionFromRow(row)
		require.Error(t, err)
	})
}
