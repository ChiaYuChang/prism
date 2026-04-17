package archivecodec_test

import (
	"errors"
	"testing"

	"github.com/ChiaYuChang/prism/pkg/archivecodec"
	"github.com/stretchr/testify/require"
)

func TestGzipBase64RoundTrip(t *testing.T) {
	blob, err := archivecodec.GzipBase64.PackString("Project Prism")
	require.NoError(t, err)
	require.Equal(t, archivecodec.CompressionGzip, blob.CompressionMethod)
	require.Equal(t, archivecodec.EncodingBase64, blob.Encoding)

	got, err := blob.UnpackString()
	require.NoError(t, err)
	require.Equal(t, "Project Prism", got)
}

func TestDeflateBase64RoundTrip(t *testing.T) {
	blob, err := archivecodec.DeflateBase64.PackString("Project Prism")
	require.NoError(t, err)
	require.Equal(t, archivecodec.CompressionDeflate, blob.CompressionMethod)
	require.Equal(t, archivecodec.EncodingBase64, blob.Encoding)

	got, err := blob.UnpackString()
	require.NoError(t, err)
	require.Equal(t, "Project Prism", got)
}

func TestNoneRawRoundTrip(t *testing.T) {
	codec := archivecodec.Codec{
		CompressionMethod: archivecodec.CompressionNone,
		Encoding:          archivecodec.EncodingRaw,
	}

	blob, err := codec.PackString("Project Prism")
	require.NoError(t, err)
	require.Equal(t, archivecodec.CompressionNone, blob.CompressionMethod)
	require.Equal(t, archivecodec.EncodingRaw, blob.Encoding)

	got, err := blob.UnpackString()
	require.NoError(t, err)
	require.Equal(t, "Project Prism", got)
}

func TestBlobUnpackString_UnsupportedEncoding(t *testing.T) {
	blob := &archivecodec.Blob{
		CompressionMethod: archivecodec.CompressionGzip,
		Encoding:          "hex",
		Content:           "abc",
	}

	_, err := blob.UnpackString()
	require.Error(t, err)
	require.True(t, errors.Is(err, archivecodec.ErrUnsupportedEncoding))
}

func TestBlobUnpackString_InvalidBase64(t *testing.T) {
	blob := &archivecodec.Blob{
		CompressionMethod: archivecodec.CompressionGzip,
		Encoding:          archivecodec.EncodingBase64,
		Content:           "invalid base64 content!!!",
	}

	_, err := blob.UnpackString()
	require.Error(t, err)
}

func TestBlobUnpackString_InvalidGzip(t *testing.T) {
	blob := &archivecodec.Blob{
		CompressionMethod: archivecodec.CompressionGzip,
		Encoding:          archivecodec.EncodingBase64,
		Content:           "SGVsbG8gV29ybGQ=",
	}

	_, err := blob.UnpackString()
	require.Error(t, err)
}

func TestBlobUnpackString_UnsupportedCompressionMethod(t *testing.T) {
	blob := &archivecodec.Blob{
		CompressionMethod: "brotli",
		Encoding:          archivecodec.EncodingRaw,
		Content:           "abc",
	}

	_, err := blob.UnpackString()
	require.Error(t, err)
	require.True(t, errors.Is(err, archivecodec.ErrUnsupportedCompressionMethod))
}

func TestBlobUnpackString_InvalidDeflate(t *testing.T) {
	blob := &archivecodec.Blob{
		CompressionMethod: archivecodec.CompressionDeflate,
		Encoding:          archivecodec.EncodingBase64,
		Content:           "SGVsbG8gV29ybGQ=",
	}

	_, err := blob.UnpackString()
	require.Error(t, err)
}
