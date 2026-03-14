package llm

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// JsonSchema wraps the google/jsonschema-go/jsonschema.Schema.
// It provides a bridge between Go types and LLM structured outputs.
type JsonSchema struct {
	Name string
	*jsonschema.Schema
	rs *jsonschema.Resolved
}

// NewSkeleton creates a basic schema based on type T.
// You can then manually enrich the returned schema.
func NewSkeleton[T any]() JsonSchema {
	s, _ := jsonschema.For[T](nil)
	return JsonSchema{Schema: s}
}

// Resolve prepares the schema for validation and default application.
func (s *JsonSchema) Resolve(opts *jsonschema.ResolveOptions) error {
	if s.rs != nil {
		return nil
	}
	if s.Schema == nil {
		return fmt.Errorf("underlying schema is nil")
	}

	if opts == nil {
		opts = &jsonschema.ResolveOptions{ValidateDefaults: true}
	}

	rs, err := s.Schema.Resolve(opts)
	if err != nil {
		return err
	}
	s.rs = rs
	return nil
}

// ApplyDefaults applies default values to the map.
func (s *JsonSchema) ApplyDefaults(m map[string]any) error {
	if err := s.Resolve(nil); err != nil {
		return err
	}
	return s.rs.ApplyDefaults(&m)
}

// Validate validates the map against the schema.
func (s *JsonSchema) Validate(m map[string]any) error {
	if err := s.Resolve(nil); err != nil {
		return err
	}
	return s.rs.Validate(m)
}

func (s JsonSchema) ToGemini() *jsonschema.Schema {
	return s.Schema
}

func (s JsonSchema) ToOpenAI() (map[string]any, error) {
	if s.Schema == nil {
		return nil, nil
	}
	data, err := json.Marshal(s.Schema)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema: %w", err)
	}
	return m, nil
}

func (s JsonSchema) MustToOpenAI() map[string]any {
	m, err := s.ToOpenAI()
	if err != nil {
		panic(err)
	}
	return m
}
