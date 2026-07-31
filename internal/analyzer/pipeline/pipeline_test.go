package pipeline

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "empty pipeline", yaml: "pipeline: []", want: "pipeline must not be empty"},
		{name: "empty stage name", yaml: "pipeline:\n  - name: '  '\n", want: "stage name is empty"},
		{name: "duplicate stage", yaml: "pipeline:\n  - name: same\n  - name: same\n", want: `duplicate stage "same"`},
		{name: "unknown dependency", yaml: "pipeline:\n  - name: child\n    depends_on: [missing]\n", want: `stage "child" depends on unknown stage "missing"`},
		{name: "self dependency", yaml: "pipeline:\n  - name: loop\n    depends_on: [loop]\n", want: `stage "loop" depends on itself`},
		{name: "duplicate dependency", yaml: "pipeline:\n  - name: child\n    depends_on: [root, root]\n  - name: root\n", want: `stage "child" has duplicate dependency "root"`},
		{name: "malformed YAML", yaml: "pipeline: [\n", want: "decode:"},
		{name: "unknown top-level field", yaml: "stages: []\n", want: "field stages not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.yaml), "configs/test-pipeline.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "configs/test-pipeline.yaml") {
				t.Fatalf("Load() error = %v, want filename", err)
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Fatalf("Load() error = %v, want ErrInvalidSpec", err)
			}
		})
	}
}

func TestLoadAndCompile(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		order []string
	}{
		{
			name:  "linear",
			yaml:  "pipeline:\n  - name: first\n    config: {task_type: first}\n  - name: second\n    depends_on: [first]\n",
			order: []string{"first", "second"},
		},
		{
			name:  "branch and join",
			yaml:  "pipeline:\n  - name: summarize\n    depends_on: [branch_b, branch_a]\n  - name: branch_b\n    depends_on: [seed]\n  - name: seed\n  - name: branch_a\n    depends_on: [seed]\n",
			order: []string{"seed", "branch_a", "branch_b", "summarize"},
		},
		{
			name:  "lexicographical tie breaking",
			yaml:  "pipeline:\n  - name: zulu\n  - name: alpha\n  - name: middle\n  - name: final\n    depends_on: [zulu, alpha, middle]\n",
			order: []string{"alpha", "middle", "zulu", "final"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := Load([]byte(tt.yaml), "pipeline.yaml")
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			ordered, err := Compile(spec)
			if err != nil {
				t.Fatalf("Compile() error = %v", err)
			}
			assertOrder(t, ordered, tt.order)
		})
	}
}

func TestCompileIsIndependentOfMapIterationOrder(t *testing.T) {
	spec := PipelineSpec{Pipeline: []StageSpec{
		{Name: "join", DependsOn: []string{"charlie", "alpha", "bravo"}},
		{Name: "bravo"},
		{Name: "charlie"},
		{Name: "alpha"},
	}}
	for i := 0; i < 100; i++ {
		ordered, err := Compile(spec)
		if err != nil {
			t.Fatalf("Compile() error = %v", err)
		}
		assertOrder(t, ordered, []string{"alpha", "bravo", "charlie", "join"})
	}
}

func TestCompileRejectsCycles(t *testing.T) {
	tests := []struct {
		name string
		spec PipelineSpec
	}{
		{
			name: "simple cycle",
			spec: PipelineSpec{Pipeline: []StageSpec{{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}}}},
		},
		{
			name: "cycle in otherwise valid graph",
			spec: PipelineSpec{Pipeline: []StageSpec{
				{Name: "start"},
				{Name: "a", DependsOn: []string{"b"}},
				{Name: "b", DependsOn: []string{"a"}},
				{Name: "finish", DependsOn: []string{"start"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.spec)
			if !errors.Is(err, ErrCycle) {
				t.Fatalf("Compile() error = %v, want ErrCycle", err)
			}
			if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
				t.Fatalf("Compile() error = %v, want cycle stage names", err)
			}
		})
	}
}

func TestLoadRejectsMultipleDocuments(t *testing.T) {
	_, err := Load([]byte("pipeline:\n  - name: one\n---\npipeline:\n  - name: two\n"), "pipeline.yaml")
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("Load() error = %v, want multiple-document error", err)
	}
}

func assertOrder(t *testing.T, stages []StageSpec, want []string) {
	t.Helper()
	if len(stages) != len(want) {
		t.Fatalf("compiled stage count = %d, want %d", len(stages), len(want))
	}
	for i, stage := range stages {
		if stage.Name != want[i] {
			t.Fatalf("compiled stage %d = %q, want %q", i, stage.Name, want[i])
		}
	}
}
