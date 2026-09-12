package catalog

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSecurityDenial(t *testing.T) {
	assert.False(t, isSecurityDenial(fmt.Errorf("load staff for PIN verification: %w", sql.ErrConnDone)))
	assert.True(t, isSecurityDenial(fmt.Errorf("%w: manager PIN verification failed", ErrInvalidManagerPin)))
	assert.True(t, isSecurityDenial(ErrForbidden))
	assert.True(t, isSecurityDenial(ErrUnauthorized))
	assert.False(t, isSecurityDenial(nil))
}
