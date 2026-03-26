package llm_test

import (
	"encoding/json"
	"testing"

	"github.com/ChiaYuChang/prism/internal/llm"
	pkgschema "github.com/ChiaYuChang/prism/pkg/schema"
	"github.com/ChiaYuChang/prism/pkg/utils"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSkeleton(t *testing.T) {
	type TestStruct struct {
		Int    int    `json:"int"`
		String string `json:"string"`
	}

	s := pkgschema.NewSkeleton[TestStruct]("test_struct", 1)

	t.Run("Skeleton", func(t *testing.T) {
		require.NotNil(t, s.Schema)
		require.Equal(t, "object", s.Type)
		require.Equal(t, "integer", s.Properties["int"].Type)
		require.Equal(t, "string", s.Properties["string"].Type)
		require.ElementsMatch(t, []string{"int", "string"}, s.Required)
	})

	t.Run("ToOpenai", func(t *testing.T) {
		m1, err := s.ToOpenAI()
		require.NoError(t, err)
		require.NotNil(t, m1)

		m2 := s.MustToOpenAI()
		require.Equal(t, m1, m2)

		require.Equal(t, "object", m1["type"])

		props, ok := m1["properties"].(map[string]any)
		require.True(t, ok)

		intProp, ok := props["int"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "integer", intProp["type"])

		stringProp, ok := props["string"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "string", stringProp["type"])

		required, ok := m1["required"].([]any)
		require.True(t, ok)
		assert.ElementsMatch(t, []any{"int", "string"}, required)
	})

	t.Run("ToGemini", func(t *testing.T) {
		schema, err := s.ToGemini()
		require.NoError(t, err)
		require.NotNil(t, schema)
	})

}

func TestInvalidSchema(t *testing.T) {
	type TestStruct struct {
		Int int `json:"int"`
	}

	t.Run("InvalidDefaultValue", func(t *testing.T) {
		s := pkgschema.NewSkeleton[TestStruct]("test_struct", 1)
		s.Properties["int"].Default = json.RawMessage(`"invalid"`)

		text := `{}`
		var out TestStruct
		err := llm.DecodeJsonSchema(s, text, &out)
		require.ErrorIs(t, err, llm.ErrInvalidSchemaDefaults)
	})

	t.Run("NilSchema", func(t *testing.T) {
		s := pkgschema.NewSkeleton[TestStruct]("test_struct", 1)
		s.Schema = nil

		var err error
		err = s.Resolve(nil)
		assert.ErrorIs(t, err, pkgschema.ErrMissingSchema)

		err = s.ApplyDefaults(map[string]any{})
		assert.ErrorIs(t, err, pkgschema.ErrMissingSchema)

		err = s.Validate(map[string]any{})
		assert.ErrorIs(t, err, pkgschema.ErrMissingSchema)

		_, err = s.ToOpenAI()
		assert.ErrorIs(t, err, pkgschema.ErrMissingSchema)

		_, err = s.ToGemini()
		assert.ErrorIs(t, err, pkgschema.ErrMissingSchema)

		var out TestStruct
		err = llm.DecodeJsonSchema(s, `{}`, &out)
		assert.ErrorIs(t, err, llm.ErrMissingResponseSchema)
	})
}

func TestDecodeJsonSchema(t *testing.T) {
	type Inner struct {
		Int    int    `json:"int"`
		String string `json:"string"`
	}

	type TestStruct struct {
		ID     int     `json:"id"`
		Int    int     `json:"int"`
		String string  `json:"string"`
		Float  float32 `json:"float"`
		Bool   bool    `json:"bool"`
		Ptr    *int    `json:"ptr"`
		Inner  Inner   `json:"inner"`
		Arr    []int   `json:"arr"`
	}

	s := pkgschema.NewSkeleton[TestStruct]("test_struct", 1)
	s.Required = []string{"id"}
	s.Properties["int"].Default = json.RawMessage("1")
	s.Properties["string"].Default = json.RawMessage(`"hello"`)
	s.Properties["float"].Default = json.RawMessage("1.23")
	s.Properties["bool"].Default = json.RawMessage("true")
	s.Properties["ptr"].Default = json.RawMessage("1")
	s.Properties["inner"].Default = json.RawMessage(`{"int": 1, "string": "hello again"}`)
	s.Properties["arr"].MinItems = utils.Ptr(1)
	s.Properties["arr"].MaxItems = utils.Ptr(3)
	s.Properties["arr"].Items = &jsonschema.Schema{
		Type:    "integer",
		Minimum: utils.Ptr(0.0),
		Maximum: utils.Ptr(10.0),
	}

	b, err := json.MarshalIndent(s, "", "  ")
	require.NoError(t, err)
	require.NotNil(t, b)

	cases := []struct {
		name     string
		text     string
		err      error
		expected TestStruct
	}{
		{
			name: "OK",
			text: `{
				"id": 100,
				"int": 2,
				"string": "hello",
				"float": 1.23,
				"bool": true,
				"arr": [1, 2, 3],
				"inner": {
					"int": 1,
					"string": "hello"
				},
				"ptr": 1
			}`,
			expected: TestStruct{
				ID:     100,
				Int:    2,
				String: "hello",
				Float:  1.23,
				Bool:   true,
				Arr:    []int{1, 2, 3},
				Inner: Inner{
					Int:    1,
					String: "hello",
				},
				Ptr: utils.Ptr(1),
			},
		},
		{
			name: "ArrayField",
			text: `{
				"id": 150,
				"arr": [7, 8]
			}`,
			expected: TestStruct{
				ID:     150,
				Int:    1,
				String: "hello",
				Float:  1.23,
				Bool:   true,
				Arr:    []int{7, 8},
				Inner: Inner{
					Int:    1,
					String: "hello again",
				},
				Ptr: utils.Ptr(1),
			},
		},
		{
			name: "ApplyDefaults",
			text: `{
				"id": 200
			}`,
			expected: TestStruct{
				ID:     200,
				Int:    1,
				String: "hello",
				Float:  1.23,
				Bool:   true,
				Inner: Inner{
					Int:    1,
					String: "hello again",
				},
				Ptr: utils.Ptr(1),
			},
		},
		{
			name: "MissingRequired",
			text: `{
				"int": 2,
				"string": "hello"
			}`,
			err: llm.ErrStructuredResponseValidation,
		},
		{
			name: "InvalidJSON",
			text: `{"int":`,
			err:  llm.ErrStructuredResponsePayload,
		},
		{
			name: "SchemaValidationFailed",
			text: `{
				"id": 300,
				"int": "wrong type",
				"string": "hello",
				"float": 1.23,
				"bool": true,
				"inner": {
					"int": 1,
					"string": "hello"
				},
				"ptr": 1
			}`,
			err: llm.ErrStructuredResponseValidation,
		},
		{
			name: "ArrayValidationFailed",
			text: `{
				"id": 350,
				"arr": [1, 2, 3, 4]
			}`,
			err: llm.ErrStructuredResponseValidation,
		},
		{
			name: "ArrayItemValidationFailed",
			text: `{
				"id": 360,
				"arr": [11]
			}`,
			err: llm.ErrStructuredResponseValidation,
		},
		{
			name: "DecodeFailed",
			text: `{
				"id": 400,
				"int": 2,
				"string": "hello",
				"float": 1.23,
				"bool": true,
				"inner": {
					"int": 1,
					"string": "hello"
				},
				"ptr": 1
			}`,
			err: llm.ErrStructuredResponseDecode,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var out any
			if c.name == "DecodeFailed" {
				out = (*int)(nil)
			} else {
				out = &TestStruct{}
			}

			err := llm.DecodeJsonSchema(s, c.text, out)
			if c.err != nil {
				require.ErrorIs(t, err, c.err, "unexpected error")
			} else {
				require.NoError(t, err)
				decoded, ok := out.(*TestStruct)
				require.True(t, ok)
				require.Equal(t, c.expected, *decoded)
			}
		})
	}

	t.Run("NilOut", func(t *testing.T) {
		err := llm.DecodeJsonSchema(s, `{"id": 500}`, nil)
		require.ErrorIs(t, err, llm.ErrStructuredResponseDecode)
	})

	t.Run("ErrResultStruct", func(t *testing.T) {
		var out *int
		err := llm.DecodeJsonSchema(s, `{"id": 700}`, out)
		require.ErrorIs(t, err, llm.ErrStructuredResponseDecode)
	})
}
