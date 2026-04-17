package archivecodec

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type CompressionMethod string

const (
	CompressionNone    CompressionMethod = "none"
	CompressionGzip    CompressionMethod = "gzip"
	CompressionDeflate CompressionMethod = "deflate"
)

type Encoding string

const (
	EncodingRaw    Encoding = "raw"
	EncodingBase64 Encoding = "base64"
)

var (
	ErrUnsupportedCompressionMethod = errors.New("unsupported compression method")
	ErrUnsupportedEncoding          = errors.New("unsupported encoding")
)

// Blob is a self-describing container for encoded archive payloads.
type Blob struct {
	CompressionMethod CompressionMethod `json:"compression_method"`
	Encoding          Encoding          `json:"encoding"`
	OriginalSize      int               `json:"original_size"`
	Content           string            `json:"content"`
}

// Codec defines how archive payloads are compressed and encoded.
type Codec struct {
	CompressionMethod CompressionMethod
	Encoding          Encoding
}

var GzipBase64 = Codec{
	CompressionMethod: CompressionGzip,
	Encoding:          EncodingBase64,
}

var DeflateBase64 = Codec{
	CompressionMethod: CompressionDeflate,
	Encoding:          EncodingBase64,
}

func (c Codec) PackString(data string) (*Blob, error) {
	raw := []byte(data)

	compressed, err := c.compress(raw)
	if err != nil {
		return nil, err
	}

	encoded, err := c.encode(compressed)
	if err != nil {
		return nil, err
	}

	return &Blob{
		CompressionMethod: c.CompressionMethod,
		Encoding:          c.Encoding,
		OriginalSize:      len(raw),
		Content:           encoded,
	}, nil
}

func (c Codec) PackJSON(js json.Marshaler) (*Blob, error) {
	data, err := js.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("json marshal error: %w", err)
	}
	return c.PackString(string(data))
}

func (b *Blob) UnpackString() (string, error) {
	if b == nil {
		return "", fmt.Errorf("blob is nil")
	}

	codec := Codec{
		CompressionMethod: b.CompressionMethod,
		Encoding:          b.Encoding,
	}

	decoded, err := codec.decode(b.Content)
	if err != nil {
		return "", err
	}

	raw, err := codec.decompress(decoded)
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func (c Codec) compress(data []byte) ([]byte, error) {
	switch c.CompressionMethod {
	case CompressionNone:
		return data, nil
	case CompressionGzip:
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)

		if _, err := gz.Write(data); err != nil {
			return nil, fmt.Errorf("gzip write error: %w", err)
		}
		if err := gz.Close(); err != nil {
			return nil, fmt.Errorf("gzip close error: %w", err)
		}
		return buf.Bytes(), nil
	case CompressionDeflate:
		var buf bytes.Buffer
		w, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			return nil, fmt.Errorf("deflate writer error: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return nil, fmt.Errorf("deflate write error: %w", err)
		}
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("deflate close error: %w", err)
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCompressionMethod, c.CompressionMethod)
	}
}

func (c Codec) decompress(data []byte) ([]byte, error) {
	switch c.CompressionMethod {
	case CompressionNone:
		return data, nil
	case CompressionGzip:
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("gzip reader error: %w", err)
		}
		defer func() { _ = gz.Close() }()

		out, err := io.ReadAll(gz)
		if err != nil {
			return nil, fmt.Errorf("gzip read error: %w", err)
		}
		return out, nil
	case CompressionDeflate:
		r := flate.NewReader(bytes.NewReader(data))
		defer func() { _ = r.Close() }()

		out, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("deflate read error: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCompressionMethod, c.CompressionMethod)
	}
}

func (c Codec) encode(data []byte) (string, error) {
	switch c.Encoding {
	case EncodingRaw:
		return string(data), nil
	case EncodingBase64:
		return base64.StdEncoding.EncodeToString(data), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedEncoding, c.Encoding)
	}
}

func (c Codec) decode(data string) ([]byte, error) {
	switch c.Encoding {
	case EncodingRaw:
		return []byte(data), nil
	case EncodingBase64:
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("base64 decode error: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEncoding, c.Encoding)
	}
}
