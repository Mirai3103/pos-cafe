package testdb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name        string
		rawURL      string
		packageName string
		suffix      string
		wantErr     string
	}{
		{"valid", "postgres://user:secret@localhost:5432/cafe_pos_test?sslmode=disable", "sales", "0123456789ab", ""},
		{"missing URL", "", "sales", "0123456789ab", "TEST_DATABASE_URL is required"},
		{"wrong scheme", "mysql://user:secret@localhost/cafe_pos_test", "sales", "0123456789ab", "postgres URL"},
		{"missing host", "postgres://user:secret@/cafe_pos_test", "sales", "0123456789ab", "host is required"},
		{"unsafe base", "postgres://user:secret@localhost/cafe_pos", "sales", "0123456789ab", "must end in _test"},
		{"unsafe identifier", "postgres://user:secret@localhost/Cafe-pos_test", "sales", "0123456789ab", "not a safe test identifier"},
		{"unknown package", "postgres://user:secret@localhost/cafe_pos_test", "inventory", "0123456789ab", "unsupported integration package"},
		{"invalid suffix", "postgres://user:secret@localhost/cafe_pos_test", "sales", "not-random", "invalid clone suffix"},
		{"name too long", "postgres://user:secret@localhost/" + strings.Repeat("a", 55) + "_test", "sales", "0123456789ab", "exceeds 63 bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseConfig(tt.rawURL, tt.packageName, tt.suffix)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.NotContains(t, err.Error(), "secret")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "cafe_pos_test", cfg.baseName)
			assert.Equal(t, "cafe_pos_test_template", cfg.templateName)
			assert.Equal(t, "cafe_pos_test_sales_0123456789ab", cfg.cloneName)
			assert.Contains(t, cfg.maintenanceDSN, "/postgres?")
			assert.Contains(t, cfg.templateDSN, "/cafe_pos_test_template?")
			assert.Contains(t, cfg.cloneDSN, "/cafe_pos_test_sales_0123456789ab?")
			assert.Contains(t, cfg.cloneDSN, "sslmode=disable")
		})
	}
}

func TestIsEphemeralClone(t *testing.T) {
	assert.True(t, isEphemeralClone("cafe_pos_test", "cafe_pos_test_sales_0123456789ab"))
	assert.False(t, isEphemeralClone("cafe_pos_test", "cafe_pos_test"))
	assert.False(t, isEphemeralClone("cafe_pos_test", "cafe_pos_test_template"))
	assert.False(t, isEphemeralClone("cafe_pos_test", "cafe_pos_test_inventory_0123456789ab"))
	assert.False(t, isEphemeralClone("cafe_pos_test", "other_test_sales_0123456789ab"))
}

func TestNewSuffix(t *testing.T) {
	first, err := newSuffix()
	require.NoError(t, err)
	second, err := newSuffix()
	require.NoError(t, err)

	assert.Regexp(t, `^[0-9a-f]{12}$`, first)
	assert.Regexp(t, `^[0-9a-f]{12}$`, second)
	assert.Regexp(t, suffixPattern, first, "generated suffix must satisfy the pattern parseConfig enforces")
	assert.NotEqual(t, first, second, "two suffixes must not collide")
}

func TestSanitizeError(t *testing.T) {
	rawURL := "postgres://user:secret@localhost:5432/cafe_pos_test?sslmode=disable"
	err := fmt.Errorf("connect %s: password=secret", rawURL)
	got := sanitizeError(err, rawURL)
	require.Error(t, got)
	assert.NotContains(t, got.Error(), "secret")
	assert.Contains(t, got.Error(), "xxxxx")
}
