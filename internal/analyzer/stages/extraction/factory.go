package extraction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"io"

	"github.com/ChiaYuChang/prism/internal/analyzer/llmapi"
	"github.com/ChiaYuChang/prism/internal/analyzer/stageparams"
)

var ErrUnsupportedParameterVersion = errors.New("unsupported extraction parameter version")

func New(ctx context.Context, deps Dependencies, stored stageparams.Stored) (llmapi.Stage[Input, any, Output], error) {
	if stored.Stage != "extraction" {
		return nil, fmt.Errorf("expected stage 'extraction', got '%s'", stored.Stage)
	}

	switch stored.Version {
	case 1:
		var p V1Parameters
		if err := decodeStrict(stored.Body, &p); err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, err
		}
		return NewV1(ctx, deps, p)
	default:
		return nil, fmt.Errorf("%w: extraction v%d", ErrUnsupportedParameterVersion, stored.Version)
	}
}

func decodeStrict(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}

	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}

	return nil
}
