package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCLIParsesRequiredArguments(t *testing.T) {
	var out bytes.Buffer

	opts, err := parseCLI([]string{
		"--source=dpp",
		"--until=2026-01-01",
		"--max-pages=5",
		"--pg-host=127.0.0.1",
		"--pg-username=postgres",
		"--messenger-type=gochannel",
	}, &out)
	require.NoError(t, err)

	assert.Equal(t, "dpp", opts.source)
	assert.Equal(t, 5, opts.maxPages)
	assert.Equal(t, "127.0.0.1", opts.postgres.Host)
	assert.Equal(t, "gochannel", opts.messengerType)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), opts.until)
}

func TestParseCLIReturnsUsageErrorWhenRequiredFlagsMissing(t *testing.T) {
	var out bytes.Buffer

	_, err := parseCLI([]string{}, &out)
	require.ErrorIs(t, err, ErrUsage)
	assert.Contains(t, out.String(), "Usage: backfiller")
}

func TestParseCLIReturnsParseErrorForInvalidDate(t *testing.T) {
	var out bytes.Buffer

	_, err := parseCLI([]string{"--source=dpp", "--until=2026/01/01"}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse --until")
}
