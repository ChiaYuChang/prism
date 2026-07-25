package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadScheduleDefinitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
schedules:
  - id: 019f23a0-0000-7000-8000-000000000001
    name: party-dpp-directory
    enabled: true
    source_type: PARTY
    source_abbr: dpp
    kind: DIRECTORY_FETCH
    url: https://www.dpp.org.tw/media/00
    frequency: 24h
    run_on_insert: true
`), 0o600))

	schedules, err := LoadScheduleDefinitions(path)
	require.NoError(t, err)
	require.Len(t, schedules, 1)
	require.Equal(t, "party-dpp-directory", schedules[0].Name)
	require.Equal(t, 24*time.Hour, schedules[0].Frequency)
	require.JSONEq(t, `{}`, string(schedules[0].Payload))
	require.Len(t, schedules[0].ConfigHash, 64)
}

func TestLoadScheduleDefinitionsRequiresUUIDv7(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`version: 1
schedules:
  - id: 550e8400-e29b-41d4-a716-446655440000
    name: bad-id
    enabled: true
    source_type: PARTY
    source_abbr: dpp
    kind: DIRECTORY_FETCH
    url: https://www.dpp.org.tw/media/00
    frequency: 24h
`), 0o600))

	_, err := LoadScheduleDefinitions(path)
	require.ErrorContains(t, err, "UUIDv7")
}
