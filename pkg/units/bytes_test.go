package units

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "empty", raw: "", want: 0},
		{name: "plain", raw: "2048", want: 2048},
		{name: "bytes", raw: "2048B", want: 2048},
		{name: "kb", raw: "10KB", want: 10_000},
		{name: "mb", raw: "10MB", want: 10_000_000},
		{name: "gb", raw: "1GB", want: 1_000_000_000},
		{name: "kib", raw: "10KiB", want: 10 * 1024},
		{name: "mib", raw: "10MiB", want: 10 * 1024 * 1024},
		{name: "decimal", raw: "1.5MB", want: 1_500_000},
		{name: "spaces", raw: " 512 KB ", want: 512_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBytes(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseBytesRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"nope", "MB", "-1MB"} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseBytes(raw)
			require.Error(t, err)
		})
	}
}

func TestBytesUnmarshalText(t *testing.T) {
	var b Bytes
	require.NoError(t, b.UnmarshalText([]byte("10MB")))
	assert.Equal(t, Bytes("10MB"), b)

	got, err := b.Int64()
	require.NoError(t, err)
	assert.EqualValues(t, 10_000_000, got)
}
