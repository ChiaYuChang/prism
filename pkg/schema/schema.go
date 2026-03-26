package schema

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

var ErrMissingSchema = errors.New("schema is missing")

// JSONSchema wraps google/jsonschema-go/jsonschema.Schema and carries contract metadata.
type JSONSchema struct {
	Name    string
	Version int
	*jsonschema.Schema
	rs *jsonschema.Resolved
}

// NewSkeleton creates a schema contract skeleton based on type T.
func NewSkeleton[T any](name string, version int) JSONSchema {
	s, _ := jsonschema.For[T](nil)
	return JSONSchema{
		Name:    name,
		Version: version,
		Schema:  s,
	}
}

// Resolve prepares the schema for validation and default application.
func (s *JSONSchema) Resolve(opts *jsonschema.ResolveOptions) error {
	if s.rs != nil {
		return nil
	}

	if s.Schema == nil {
		return ErrMissingSchema
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
func (s *JSONSchema) ApplyDefaults(m map[string]any) error {
	if err := s.Resolve(nil); err != nil {
		return err
	}
	return s.rs.ApplyDefaults(&m)
}

// Validate validates the map against the schema.
func (s *JSONSchema) Validate(m map[string]any) error {
	if err := s.Resolve(nil); err != nil {
		return err
	}
	return s.rs.Validate(m)
}

func (s JSONSchema) ToGemini() (*jsonschema.Schema, error) {
	if s.Schema == nil {
		return nil, ErrMissingSchema
	}
	return s.Schema, nil
}

func (s JSONSchema) ToOpenAI() (map[string]any, error) {
	if s.Schema == nil {
		return nil, ErrMissingSchema
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

func (s JSONSchema) MustToOpenAI() map[string]any {
	m, err := s.ToOpenAI()
	if err != nil {
		panic(err)
	}
	return m
}
