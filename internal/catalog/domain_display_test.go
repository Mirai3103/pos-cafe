package catalog_test

import (
	"strings"
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strp(s string) *string { return &s }

func TestNormalizeItemCode(t *testing.T) {
	t.Parallel()
	display, key, err := catalog.NormalizeItemCode(strp("  CFsd "))
	require.NoError(t, err)
	assert.Equal(t, "CFsd", display.String)
	assert.Equal(t, "cfsd", key.String)

	for _, blank := range []*string{nil, strp(""), strp("   ")} {
		display, key, err = catalog.NormalizeItemCode(blank)
		require.NoError(t, err)
		assert.False(t, display.Valid)
		assert.False(t, key.Valid)
	}

	for _, bad := range []string{"cà phê", "ab-cd", "abcdefghijklm"} {
		_, _, err = catalog.NormalizeItemCode(strp(bad))
		assert.Error(t, err, bad)
	}
}

func TestNormalizeBadge(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"BEST_SELLER", "HOT", "NEW", "SIGNATURE", "CHEF_PICK"} {
		b, err := catalog.NormalizeBadge(strp(ok))
		require.NoError(t, err)
		assert.Equal(t, ok, b.String)
	}
	b, err := catalog.NormalizeBadge(nil)
	require.NoError(t, err)
	assert.False(t, b.Valid)
	_, err = catalog.NormalizeBadge(strp("FAVORITE"))
	assert.Error(t, err)
	_, err = catalog.NormalizeBadge(strp("hot"))
	assert.Error(t, err, "badges are case-sensitive codes")
}

func TestNormalizeDescription(t *testing.T) {
	t.Parallel()
	d, err := catalog.NormalizeDescription(strp("  Phin truyền thống  "))
	require.NoError(t, err)
	assert.Equal(t, "Phin truyền thống", d.String)

	d, err = catalog.NormalizeDescription(strp("   "))
	require.NoError(t, err)
	assert.False(t, d.Valid)

	_, err = catalog.NormalizeDescription(strp(strings.Repeat("ạ", 300)))
	require.NoError(t, err, "300 runes is allowed even when bytes exceed 300")
	_, err = catalog.NormalizeDescription(strp(strings.Repeat("a", 301)))
	assert.Error(t, err)
}

func TestNormalizeCategoryIcon(t *testing.T) {
	t.Parallel()
	i, err := catalog.NormalizeCategoryIcon(strp("cup-soda"))
	require.NoError(t, err)
	assert.Equal(t, "cup-soda", i.String)
	i, err = catalog.NormalizeCategoryIcon(nil)
	require.NoError(t, err)
	assert.False(t, i.Valid)
	_, err = catalog.NormalizeCategoryIcon(strp("Coffee Cup"))
	assert.Error(t, err)
}

func TestValidateDisplayOrder(t *testing.T) {
	t.Parallel()
	assert.NoError(t, catalog.ValidateDisplayOrder(0))
	assert.NoError(t, catalog.ValidateDisplayOrder(9999))
	assert.Error(t, catalog.ValidateDisplayOrder(-1))
	assert.Error(t, catalog.ValidateDisplayOrder(10000))
}

func TestValidateSelectionRule(t *testing.T) {
	t.Parallel()
	assert.NoError(t, catalog.ValidateSelectionRule(0, 3, 3, 0))
	assert.NoError(t, catalog.ValidateSelectionRule(1, 1, 4, 1))
	assert.Error(t, catalog.ValidateSelectionRule(-1, 1, 3, 0), "min below 0")
	assert.Error(t, catalog.ValidateSelectionRule(0, 0, 3, 0), "max below 1")
	assert.Error(t, catalog.ValidateSelectionRule(2, 1, 3, 1), "min above max")
	assert.Error(t, catalog.ValidateSelectionRule(0, 4, 3, 0), "max above active options")
	assert.Error(t, catalog.ValidateSelectionRule(1, 2, 3, 0), "defaults below min")
	assert.Error(t, catalog.ValidateSelectionRule(0, 1, 3, 2), "defaults above max")
}

func TestIDSetHelpers(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.MustParse("00000000-0000-0000-0000-00000000000a"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000b"),
		uuid.MustParse("00000000-0000-0000-0000-00000000000c")

	set, err := catalog.NormalizeIDSet([]uuid.UUID{c, a}, "ids")
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c}, set)

	set, err = catalog.NormalizeIDSet(nil, "ids")
	require.NoError(t, err)
	assert.NotNil(t, set)
	assert.Empty(t, set)

	_, err = catalog.NormalizeIDSet([]uuid.UUID{a, a}, "ids")
	assert.Error(t, err, "duplicates")
	_, err = catalog.NormalizeIDSet([]uuid.UUID{uuid.Nil}, "ids")
	assert.Error(t, err, "nil id")
	tooMany := make([]uuid.UUID, catalog.MaxAssignmentIDs+1)
	for i := range tooMany {
		tooMany[i] = uuid.New()
	}
	_, err = catalog.NormalizeIDSet(tooMany, "ids")
	assert.Error(t, err, "over the cap")

	added, removed := catalog.DiffIDSets([]uuid.UUID{a, b}, []uuid.UUID{b, c})
	assert.Equal(t, []uuid.UUID{c}, added)
	assert.Equal(t, []uuid.UUID{a}, removed)
	added, removed = catalog.DiffIDSets(nil, nil)
	assert.NotNil(t, added)
	assert.NotNil(t, removed)

	assert.Equal(t, []uuid.UUID{a, b, c}, catalog.UnionIDs([]uuid.UUID{c, a}, []uuid.UUID{a, b}))
	assert.True(t, catalog.ContainsID([]uuid.UUID{a, b}, b))
	assert.False(t, catalog.ContainsID([]uuid.UUID{a, b}, c))
}
