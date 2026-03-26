package llm_test

import (
	"testing"

	"github.com/ChiaYuChang/prism/internal/llm"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerateRequest(t *testing.T) {
	model := "awsome-llm"
	system := "system prompt"
	prompt := "user prompt"
	req := llm.NewGenerateRequest(model, system, prompt)

	require.NotNil(t, req)
	assert.Equal(t, model, req.Model)
	assert.Equal(t, system, req.SystemInstruction)
	assert.Equal(t, prompt, req.Prompt)

	// optional provider sampling parameters should remain nil so provider defaults apply.
	assert.Nil(t, req.Temperature)
	assert.Nil(t, req.TopP)
	assert.Nil(t, req.TopK)
	assert.Nil(t, req.MaxTokens)
	assert.Nil(t, req.Meta)
	assert.Equal(t, llm.ResponseFormat(""), req.Format)
	assert.Nil(t, req.JSONSchema.Schema)
}

func TestGenerateResponseDecodeJSONSchema(t *testing.T) {
	type TestStruct struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	schema := pkgschema.NewSkeleton[TestStruct]("test_struct", 1)
	cases := []struct {
		name string
		resp *llm.GenerateResponse
		err  error
		want TestStruct
	}{
		{
			name: "OK",
			resp: &llm.GenerateResponse{
				Text:       `{"id":1,"name":"prism"}`,
				JsonSchema: schema,
			},
			want: TestStruct{
				ID:   1,
				Name: "prism",
			},
		},
		{
			name: "NilResponse",
			resp: nil,
			err:  llm.ErrNilGenerateResponse,
		},
		{
			name: "InvalidPayload",
			resp: &llm.GenerateResponse{
				Text:       "",
				JsonSchema: schema,
			},
			err: llm.ErrEmptyResponsePayload,
		},
		{
			name: "MissingSchema",
			resp: &llm.GenerateResponse{
				Text:       `{"id":1,"name":"prism"}`,
				JsonSchema: pkgschema.JSONSchema{},
			},
			err: llm.ErrMissingResponseSchema,
		},
		{
			name: "SchemaValidationFailed",
			resp: &llm.GenerateResponse{
				Text:       `{"id":"wrong","name":"prism"}`,
				JsonSchema: schema,
			},
			err: llm.ErrStructuredResponseValidation,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var out TestStruct
			err := c.resp.DecodeJSONSchema(&out)
			if c.err == nil {
				require.NoError(t, err)
				assert.Equal(t, c.want, out)
			} else {
				require.ErrorIs(t, err, c.err)
			}
		})
	}
}
