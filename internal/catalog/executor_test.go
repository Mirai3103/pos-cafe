package catalog

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSecurityDenial(t *testing.T) {
	assert.False(t, policy.IsSecurityDenial(fmt.Errorf("load staff for PIN verification: %w", sql.ErrConnDone)))
	assert.True(t, policy.IsSecurityDenial(fmt.Errorf("%w: manager PIN verification failed", ErrInvalidManagerPin)))
	assert.True(t, policy.IsSecurityDenial(ErrForbidden))
	assert.True(t, policy.IsSecurityDenial(ErrUnauthorized))
	assert.False(t, policy.IsSecurityDenial(nil))
}
