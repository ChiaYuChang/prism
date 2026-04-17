package errorcode_test

import (
	"encoding/json"
	"testing"

	"github.com/ChiaYuChang/prism/pkg/errorcode"
	"github.com/stretchr/testify/require"
)

func TestWarning(t *testing.T) {
	w0 := errorcode.Warning{
		Level:   errorcode.WarnInfo,
		Message: "warning round trip test",
	}

	data, err := json.Marshal(w0)
	require.NoError(t, err)

	w1 := errorcode.Warning{}
	err = json.Unmarshal(data, &w1)
	require.NoError(t, err)

	require.Equal(t, w0, w1)
}
