package model_test

import (
	"testing"
	"time"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// baseCandidate returns a Candidates fixture used across fingerprint tests.
// Fingerprint is computed from URL, Title and PublishedAt only; the other
// fields are populated to ensure they do NOT contribute to the hash.
func baseCandidate() model.Candidates {
	return model.Candidates{
		BatchID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		SourceAbbr:      "src-0",
		TraceID:         "trace-1",
		URL:             "https://example.com/a",
		Title:           "Hello",
		Description:     "desc",
		IngestionMethod: "DIRECTORY",
		PublishedAt:     time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		DiscoveredAt:    time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		Metadata:        map[string]any{"a": 1},
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	c := baseCandidate()
	require.Equal(t, c.Fingerprint(), c.Fingerprint())
	require.Equal(t, c.Fingerprint(), baseCandidate().Fingerprint())
}

func TestFingerprint_DiffersByURL(t *testing.T) {
	a := baseCandidate()
	b := baseCandidate()
	b.URL = "https://example.com/b"
	require.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_DiffersByTitle(t *testing.T) {
	a := baseCandidate()
	b := baseCandidate()
	b.Title = "Goodbye"
	require.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_DiffersByPublishedAt(t *testing.T) {
	a := baseCandidate()
	b := baseCandidate()
	b.PublishedAt = a.PublishedAt.Add(time.Second)
	require.NotEqual(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_IgnoresUnrelatedFields(t *testing.T) {
	a := baseCandidate()
	b := baseCandidate()
	b.BatchID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	b.SourceAbbr = "src-1"
	b.TraceID = "trace-2"
	b.Description = "different"
	b.IngestionMethod = "MANUAL"
	b.DiscoveredAt = a.DiscoveredAt.Add(72 * time.Hour)
	b.Metadata = map[string]any{"b": 2}
	require.Equal(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_TimezoneNormalizedToUTC(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Taipei")
	require.NoError(t, err)
	a := baseCandidate()
	b := baseCandidate()
	// Same instant, different zone — UTC normalization should collapse.
	b.PublishedAt = a.PublishedAt.In(loc)
	require.Equal(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_SubSecondPrecisionDropped(t *testing.T) {
	a := baseCandidate()
	b := baseCandidate()
	// time.DateTime format ("2006-01-02 15:04:05") is second-resolution; nanos drop.
	b.PublishedAt = a.PublishedAt.Add(500 * time.Millisecond)
	require.Equal(t, a.Fingerprint(), b.Fingerprint())
}

func TestFingerprint_EmptyCandidate(t *testing.T) {
	var c model.Candidates
	// Must not panic and must return a stable non-empty string.
	fp := c.Fingerprint()
	require.NotEmpty(t, fp)
	require.Equal(t, fp, model.Candidates{}.Fingerprint())
}

func TestFingerprint_HexEncoding(t *testing.T) {
	fp := baseCandidate().Fingerprint()
	// sha256[:16] = 16 bytes → hex always emits 32 chars, matching CHAR(32).
	require.Len(t, fp, 32)
	for _, r := range fp {
		require.True(t,
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'),
			"non-hex char in fingerprint: %q", r)
	}
}
