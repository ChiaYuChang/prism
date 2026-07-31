package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

// WorkTaskSpec is the complete durable description of one ordinary stage task.
type WorkTaskSpec struct {
	LogicalKey string
	Kind       string
	SourceType string
	SourceAbbr string
	URL        string
	Payload    []byte
	Meta       []byte
}

// WorkSetInput is the immutable data made available to a stage work-set builder.
type WorkSetInput struct {
	CandidateIDs        []uuid.UUID
	ContentIDs          []uuid.UUID
	CandidateSourceAbbr map[uuid.UUID]string
	ContentSourceAbbr   map[uuid.UUID]string
}

// WorkSetBuilder builds ordinary tasks for one registered task type.
type WorkSetBuilder interface {
	TaskType() string
	Build(stage string, config map[string]any, input WorkSetInput) ([]WorkTaskSpec, error)
}

// Registry selects typed work-set builders by task_type.
type Registry struct {
	builders map[string]WorkSetBuilder
}

// NewRegistry creates a registry and rejects duplicate task types.
func NewRegistry(builders ...WorkSetBuilder) (*Registry, error) {
	r := &Registry{builders: make(map[string]WorkSetBuilder, len(builders))}
	for _, builder := range builders {
		if builder == nil || builder.TaskType() == "" {
			return nil, fmt.Errorf("work-set builder is invalid")
		}
		if _, exists := r.builders[builder.TaskType()]; exists {
			return nil, fmt.Errorf("duplicate work-set builder %q", builder.TaskType())
		}
		r.builders[builder.TaskType()] = builder
	}
	return r, nil
}

// Build delegates to the registered builder selected by config.task_type.
func (r *Registry) Build(stage string, config map[string]any, input WorkSetInput) ([]WorkTaskSpec, error) {
	raw, ok := config["task_type"]
	if !ok {
		return nil, fmt.Errorf("stage %q config.task_type is required", stage)
	}
	taskType, ok := raw.(string)
	if !ok || taskType == "" {
		return nil, fmt.Errorf("stage %q config.task_type must be a non-empty string", stage)
	}
	builder, ok := r.builders[taskType]
	if !ok {
		return nil, fmt.Errorf("stage %q has unsupported task_type %q", stage, taskType)
	}
	return builder.Build(stage, config, input)
}

type fanOutConfig struct {
	FanOut string `json:"fan_out"`
}

// EmbedCandidateBuilder builds one EMBED_CANDIDATE task per candidate.
type EmbedCandidateBuilder struct{}

func (EmbedCandidateBuilder) TaskType() string { return repo.TaskKindEmbedCandidate }

func (EmbedCandidateBuilder) Build(stage string, config map[string]any, input WorkSetInput) ([]WorkTaskSpec, error) {
	var typed fanOutConfig
	if err := decodeConfig(config, &typed); err != nil {
		return nil, fmt.Errorf("stage %q: %w", stage, err)
	}
	if typed.FanOut != "candidate_ids" {
		return nil, fmt.Errorf("stage %q EMBED_CANDIDATE requires fan_out=candidate_ids", stage)
	}
	ids := append([]uuid.UUID(nil), input.CandidateIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return embedTasks(stage, repo.TaskKindEmbedCandidate, "candidate_id", ids, input.CandidateSourceAbbr)
}

// EmbedContentBuilder builds one EMBED_CONTENT task per content.
type EmbedContentBuilder struct{}

func (EmbedContentBuilder) TaskType() string { return repo.TaskKindEmbedContent }

func (EmbedContentBuilder) Build(stage string, config map[string]any, input WorkSetInput) ([]WorkTaskSpec, error) {
	var typed fanOutConfig
	if err := decodeConfig(config, &typed); err != nil {
		return nil, fmt.Errorf("stage %q: %w", stage, err)
	}
	if typed.FanOut != "content_ids" {
		return nil, fmt.Errorf("stage %q EMBED_CONTENT requires fan_out=content_ids", stage)
	}
	ids := append([]uuid.UUID(nil), input.ContentIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return embedTasks(stage, repo.TaskKindEmbedContent, "content_id", ids, input.ContentSourceAbbr)
}

func embedTasks(stage, kind, field string, ids []uuid.UUID, sourceAbbr map[uuid.UUID]string) ([]WorkTaskSpec, error) {
	tasks := make([]WorkTaskSpec, 0, len(ids))
	for _, id := range ids {
		abbr := sourceAbbr[id]
		if abbr == "" {
			return nil, fmt.Errorf("stage %q has no source abbreviation for %s %s", stage, field, id)
		}
		payload, err := json.Marshal(map[string]string{field: id.String()})
		if err != nil {
			return nil, fmt.Errorf("marshal %s task: %w", kind, err)
		}
		tasks = append(tasks, WorkTaskSpec{
			LogicalKey: stage + ":" + id.String(),
			Kind:       kind,
			SourceType: repo.SourceTypeParty,
			SourceAbbr: abbr,
			URL:        "pipeline://" + stage + "/" + id.String(),
			Payload:    payload,
		})
	}
	return tasks, nil
}

func decodeConfig(config map[string]any, output any) error {
	payload := make(map[string]any, len(config))
	for key, value := range config {
		if key != "task_type" {
			payload[key] = value
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
}
