package catalog_test

import (
	"testing"

	"github.com/Mirai3103/pos-cafe/internal/catalog"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeNamePreservesInternalWhitespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantDisplay string
		wantKey     string
	}{
		{
			name:        "trim leading and trailing spaces",
			input:       "  Hello  ",
			wantDisplay: "Hello",
			wantKey:     "hello",
		},
		{
			name:        "preserve internal whitespace",
			input:       "  Cà   Phê  ",
			wantDisplay: "Cà   Phê",
			wantKey:     "cà   phê",
		},
		{
			name:        "unicode lowercase key",
			input:       "  HÀ NỘI  ",
			wantDisplay: "HÀ NỘI",
			wantKey:     "hà nội",
		},
		{
			name:        "empty string trims to empty",
			input:       "",
			wantDisplay: "",
			wantKey:     "",
		},
		{
			name:        "whitespace only trims to empty",
			input:       "   ",
			wantDisplay: "",
			wantKey:     "",
		},
		{
			name:        "internal tabs and newlines preserved",
			input:       "  Hello\t\nWorld  ",
			wantDisplay: "Hello\t\nWorld",
			wantKey:     "hello\t\nworld",
		},
		{
			name:        "mixed unicode and ascii",
			input:       "  Café latte  ",
			wantDisplay: "Café latte",
			wantKey:     "café latte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			display, key := catalog.NormalizeName(tt.input)
			assert.Equal(t, tt.wantDisplay, display)
			assert.Equal(t, tt.wantKey, key)
		})
	}
}

func TestValidatePrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		price   int64
		wantErr bool
	}{
		{name: "minimum valid price", price: 1, wantErr: false},
		{name: "maximum valid price", price: 2_147_483_647, wantErr: false},
		{name: "typical price", price: 45_000, wantErr: false},
		{name: "zero is invalid", price: 0, wantErr: true},
		{name: "negative is invalid", price: -1, wantErr: true},
		{name: "exceeds max", price: 2_147_483_648, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := catalog.ValidatePrice(tt.price)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSurcharge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		surcharge int64
		wantErr   bool
	}{
		{name: "zero surcharge valid", surcharge: 0, wantErr: false},
		{name: "maximum valid surcharge", surcharge: 2_147_483_647, wantErr: false},
		{name: "typical surcharge", surcharge: 5_000, wantErr: false},
		{name: "negative is invalid", surcharge: -1, wantErr: true},
		{name: "exceeds max", surcharge: 2_147_483_648, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := catalog.ValidateSurcharge(tt.surcharge)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRetirement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		r       catalog.Retirement
		wantErr bool
	}{
		{
			name:    "zero value retirement is valid",
			r:       catalog.Retirement{},
			wantErr: false,
		},
		{
			name:    "NO_LONGER_OFFERED without note",
			r:       catalog.Retirement{Reason: "NO_LONGER_OFFERED"},
			wantErr: false,
		},
		{
			name:    "MENU_RESTRUCTURE without note",
			r:       catalog.Retirement{Reason: "MENU_RESTRUCTURE"},
			wantErr: false,
		},
		{
			name:    "OTHER with note",
			r:       catalog.Retirement{Reason: "OTHER", Note: "discontinued by supplier"},
			wantErr: false,
		},
		{
			name:    "OTHER without note is invalid",
			r:       catalog.Retirement{Reason: "OTHER"},
			wantErr: true,
		},
		{
			name:    "OTHER with blank note is invalid",
			r:       catalog.Retirement{Reason: "OTHER", Note: "   "},
			wantErr: true,
		},
		{
			name:    "OTHER with note exceeding 500 runes",
			r:       catalog.Retirement{Reason: "OTHER", Note: string(make([]rune, 501))},
			wantErr: true,
		},
		{
			name:    "OTHER with note exactly 500 runes",
			r:       catalog.Retirement{Reason: "OTHER", Note: string(make([]rune, 500))},
			wantErr: false,
		},
		{
			name:    "unknown reason is invalid",
			r:       catalog.Retirement{Reason: "INVALID_REASON"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := catalog.ValidateRetirement(tt.r)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEffectiveGroupIDs(t *testing.T) {
	t.Parallel()

	g1 := uuid.New()
	g2 := uuid.New()
	g3 := uuid.New()
	g4 := uuid.New()

	tests := []struct {
		name             string
		inheritedGroups  []uuid.UUID
		excludedGroupIDs []uuid.UUID
		directGroups     []uuid.UUID
		want             []uuid.UUID
	}{
		{
			name:             "direct only",
			inheritedGroups:  nil,
			excludedGroupIDs: nil,
			directGroups:     []uuid.UUID{g1, g2},
			want:             []uuid.UUID{g1, g2},
		},
		{
			name:             "inherited minus exclusion plus direct dedup",
			inheritedGroups:  []uuid.UUID{g1, g2, g3},
			excludedGroupIDs: []uuid.UUID{g2},
			directGroups:     []uuid.UUID{g3, g4},
			want:             []uuid.UUID{g1, g3, g4},
		},
		{
			name:             "exclusion does not suppress direct",
			inheritedGroups:  []uuid.UUID{g1},
			excludedGroupIDs: []uuid.UUID{g1},
			directGroups:     []uuid.UUID{g1},
			want:             []uuid.UUID{g1},
		},
		{
			name:             "all empty",
			inheritedGroups:  nil,
			excludedGroupIDs: nil,
			directGroups:     nil,
			want:             nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalog.EffectiveGroupIDs(tt.inheritedGroups, tt.excludedGroupIDs, tt.directGroups)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestIsSellable(t *testing.T) {
	t.Parallel()

	g1 := uuid.New()

	tests := []struct {
		name  string
		state catalog.ItemState
		want  bool
	}{
		{
			name: "direct priced available item with no required groups is sellable",
			state: catalog.ItemState{
				Available:             true,
				HasDirectPrice:        true,
				AvailableSizeCount:    0,
				EffectiveGroups:       nil,
				AvailableOptionCounts: map[uuid.UUID]int{},
			},
			want: true,
		},
		{
			name: "unavailable item is not sellable",
			state: catalog.ItemState{
				Available:      false,
				HasDirectPrice: true,
			},
			want: false,
		},
		{
			name: "neither direct nor sized pricing",
			state: catalog.ItemState{
				Available:          true,
				HasDirectPrice:     false,
				AvailableSizeCount: 0,
			},
			want: false,
		},
		{
			name: "sized with no available sizes",
			state: catalog.ItemState{
				Available:          true,
				HasDirectPrice:     false,
				AvailableSizeCount: 0,
			},
			want: false,
		},
		{
			name: "sized with at least one available size is sellable",
			state: catalog.ItemState{
				Available:          true,
				HasDirectPrice:     false,
				AvailableSizeCount: 1,
			},
			want: true,
		},
		{
			name: "required group missing options",
			state: catalog.ItemState{
				Available:             true,
				HasDirectPrice:        true,
				AvailableSizeCount:    0,
				EffectiveGroups:       []catalog.EffectiveGroup{{ID: g1, MinSelections: 1}},
				AvailableOptionCounts: map[uuid.UUID]int{g1: 0},
			},
			want: false,
		},
		{
			name: "required group has enough options",
			state: catalog.ItemState{
				Available:             true,
				HasDirectPrice:        true,
				AvailableSizeCount:    0,
				EffectiveGroups:       []catalog.EffectiveGroup{{ID: g1, MinSelections: 1}},
				AvailableOptionCounts: map[uuid.UUID]int{g1: 2},
			},
			want: true,
		},
		{
			name: "optional group does not block sellability",
			state: catalog.ItemState{
				Available:             true,
				HasDirectPrice:        true,
				AvailableSizeCount:    0,
				EffectiveGroups:       []catalog.EffectiveGroup{{ID: g1, MinSelections: 0}},
				AvailableOptionCounts: map[uuid.UUID]int{g1: 0},
			},
			want: true,
		},
		{
			name: "both direct price and available sizes is not sellable",
			state: catalog.ItemState{
				Available:          true,
				HasDirectPrice:     true,
				AvailableSizeCount: 1,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := catalog.IsSellable(tt.state)
			assert.Equal(t, tt.want, got)
		})
	}
}
