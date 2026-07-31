// Package pipeline loads and compiles declarative pipeline definitions.
package pipeline

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrInvalidSpec identifies a malformed or invalid pipeline definition.
	ErrInvalidSpec = errors.New("invalid pipeline spec")
	// ErrCycle identifies a dependency graph that cannot be topologically sorted.
	ErrCycle = errors.New("pipeline dependency cycle")
)

// PipelineSpec is the YAML document containing the configured stages.
type PipelineSpec struct {
	Pipeline []StageSpec `yaml:"pipeline"`
}

// StageSpec describes one pipeline stage and its dependencies.
type StageSpec struct {
	Name      string         `yaml:"name"`
	DependsOn []string       `yaml:"depends_on"`
	Config    map[string]any `yaml:"config"`
}

// Load parses and validates one YAML pipeline document.
func Load(data []byte, filename string) (PipelineSpec, error) {
	return LoadReader(strings.NewReader(string(data)), filename)
}

// LoadFile reads, parses, and validates a pipeline YAML file.
func LoadFile(filename string) (PipelineSpec, error) {
	file, err := os.Open(filename)
	if err != nil {
		return PipelineSpec{}, fmt.Errorf("%s: open: %w", displayFilename(filename), err)
	}
	defer func() { _ = file.Close() }()
	return LoadReader(file, filename)
}

// LoadReader parses and validates one YAML pipeline document from reader.
func LoadReader(reader io.Reader, filename string) (PipelineSpec, error) {
	if reader == nil {
		return PipelineSpec{}, invalid(filename, "reader is nil")
	}

	var spec PipelineSpec
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(&spec); err != nil {
		return PipelineSpec{}, invalid(filename, "decode: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return PipelineSpec{}, invalid(filename, "multiple YAML documents are not supported")
		}
		return PipelineSpec{}, invalid(filename, "decode: %v", err)
	}
	if err := spec.Validate(filename); err != nil {
		return PipelineSpec{}, err
	}
	return spec, nil
}

// Validate checks stage names and dependency references.
func (s PipelineSpec) Validate(filename string) error {
	if len(s.Pipeline) == 0 {
		return invalid(filename, "pipeline must not be empty")
	}

	stages := make(map[string]struct{}, len(s.Pipeline))
	for _, stage := range s.Pipeline {
		if strings.TrimSpace(stage.Name) == "" {
			return invalid(filename, "stage name is empty")
		}
		if _, exists := stages[stage.Name]; exists {
			return invalid(filename, "duplicate stage %q", stage.Name)
		}
		stages[stage.Name] = struct{}{}
	}

	for _, stage := range s.Pipeline {
		dependencies := make(map[string]struct{}, len(stage.DependsOn))
		for _, dependency := range stage.DependsOn {
			if _, duplicate := dependencies[dependency]; duplicate {
				return invalid(filename, "stage %q has duplicate dependency %q", stage.Name, dependency)
			}
			dependencies[dependency] = struct{}{}
			if dependency == stage.Name {
				return invalid(filename, "stage %q depends on itself", stage.Name)
			}
			if _, known := stages[dependency]; !known {
				return invalid(filename, "stage %q depends on unknown stage %q", stage.Name, dependency)
			}
		}
	}
	return nil
}

// Compile validates spec and returns its stages in deterministic topological order.
func Compile(spec PipelineSpec) ([]StageSpec, error) {
	if err := spec.Validate("<pipeline>"); err != nil {
		return nil, err
	}

	byName := make(map[string]StageSpec, len(spec.Pipeline))
	indegree := make(map[string]int, len(spec.Pipeline))
	dependents := make(map[string][]string, len(spec.Pipeline))
	for _, stage := range spec.Pipeline {
		byName[stage.Name] = stage
		indegree[stage.Name] = len(stage.DependsOn)
		for _, dependency := range stage.DependsOn {
			dependents[dependency] = append(dependents[dependency], stage.Name)
		}
	}

	ready := &stageHeap{}
	for name, degree := range indegree {
		if degree == 0 {
			heap.Push(ready, name)
		}
	}

	ordered := make([]StageSpec, 0, len(spec.Pipeline))
	for ready.Len() > 0 {
		name := heap.Pop(ready).(string)
		ordered = append(ordered, byName[name])
		for _, dependent := range dependents[name] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				heap.Push(ready, dependent)
			}
		}
	}

	if len(ordered) != len(spec.Pipeline) {
		remaining := make([]string, 0)
		for name, degree := range indegree {
			if degree > 0 {
				remaining = append(remaining, name)
			}
		}
		sort.Strings(remaining)
		return nil, fmt.Errorf("%w: %s", ErrCycle, strings.Join(remaining, ", "))
	}
	return ordered, nil
}

func invalid(filename, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalidSpec, displayFilename(filename), fmt.Sprintf(format, args...))
}

func displayFilename(filename string) string {
	if filename == "" {
		return "<pipeline>"
	}
	return filename
}

type stageHeap []string

func (h stageHeap) Len() int           { return len(h) }
func (h stageHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h stageHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *stageHeap) Push(value any)    { *h = append(*h, value.(string)) }
func (h *stageHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}
