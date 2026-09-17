package preparation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeModifiersNormalizesJSONNull(t *testing.T) {
	modifiers, err := decodeModifiers(json.RawMessage("null"))

	require.NoError(t, err)
	require.NotNil(t, modifiers)
	require.Empty(t, modifiers)
}
