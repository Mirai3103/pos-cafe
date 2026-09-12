package tables_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/tables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTableName(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantDisplay string
		wantKey     string
	}{
		{"trims surrounding whitespace", "  Ban 1  ", "Ban 1", "ban 1"},
		{"collapses internal whitespace", "Ban    1", "Ban 1", "ban 1"},
		{"collapses tabs and newlines", "Ban\t\n 1", "Ban 1", "ban 1"},
		{"lowercases Vietnamese diacritics", "BÀN GHÉP", "BÀN GHÉP", "bàn ghép"},
		{"preserves single internal spaces", "Ban ghep so 1", "Ban ghep so 1", "ban ghep so 1"},
		{"empty stays empty", "   ", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			display, key := tables.NormalizeTableName(tc.in)
			assert.Equal(t, tc.wantDisplay, display)
			assert.Equal(t, tc.wantKey, key)
		})
	}
}

func TestNormalizeTableNameKeyMatchesPostgresLower(t *testing.T) {
	// The database CHECK is normalized_name = lower(name), so the key must be
	// exactly the lowercase of the display form, not of the raw input.
	display, key := tables.NormalizeTableName("  Bàn  Ghép  ")
	assert.Equal(t, "Bàn Ghép", display)
	assert.Equal(t, strings.ToLower(display), key)
}

func TestValidateTableName(t *testing.T) {
	t.Run("accepts a one-rune name", func(t *testing.T) {
		require.NoError(t, tables.ValidateTableName("A"))
	})

	t.Run("accepts exactly 60 runes", func(t *testing.T) {
		require.NoError(t, tables.ValidateTableName(strings.Repeat("à", 60)))
	})

	t.Run("rejects empty", func(t *testing.T) {
		require.Error(t, tables.ValidateTableName(""))
	})

	t.Run("rejects 61 runes", func(t *testing.T) {
		require.Error(t, tables.ValidateTableName(strings.Repeat("a", 61)))
	})

	t.Run("counts runes not bytes", func(t *testing.T) {
		// 60 Vietnamese runes are 120 bytes; a byte-based check would reject this.
		name := strings.Repeat("à", 60)
		require.Greater(t, len(name), 60)
		require.NoError(t, tables.ValidateTableName(name))
	})
}
