package archiver_test

import (
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/internal/collector/archiver"
	"github.com/ChiaYuChang/prism/pkg/testutils"
	"github.com/stretchr/testify/require"
)

func TestParseURI_FileScheme_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.ParseURI("file://"+dir, testutils.Logger())
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestParseURI_FileScheme_TripleSlash(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.ParseURI("file://"+dir, testutils.Logger())
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestParseURI_BarePath(t *testing.T) {
	dir := t.TempDir()
	a, err := archiver.ParseURI(dir, testutils.Logger())
	require.NoError(t, err)
	require.NotNil(t, a)
}

func TestParseURI_S3_ReturnsError(t *testing.T) {
	// S3 requires an injected client; ParseURI should return a descriptive error.
	_, err := archiver.ParseURI("s3://my-bucket/prefix", testutils.Logger())
	require.Error(t, err)
}

func TestParseURI_UnknownScheme(t *testing.T) {
	_, err := archiver.ParseURI("gcs://my-bucket", testutils.Logger())
	require.Error(t, err)
	require.True(t, errors.Is(err, archiver.ErrUnknownScheme))
}

func TestParseURI_NilLogger(t *testing.T) {
	_, err := archiver.ParseURI("/tmp/test", nil)
	require.Error(t, err)
}

func TestParseURI_EmptyURI(t *testing.T) {
	_, err := archiver.ParseURI("", testutils.Logger())
	require.Error(t, err)
}
