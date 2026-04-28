package collector

// Pipeline bundles the per-source stage implementations: F, a Minifier slot
// (first transformer whose output is the archive point), zero or more
// post-archive Transformers, and a Parser. Minifier is a role — not a
// distinct interface — distinguished by its position in the struct. See
// docs/pipeline-wiring-design.md.
type Pipeline struct {
	Fetcher      Fetcher
	Minifier     Transformer   // output is archived
	Transformers []Transformer // post-archive chain; may be nil/empty
	Parser       Parser
}

// PipelineRegistry maps source IDs (typically sources.abbr) to Pipelines,
// falling back to a default when no source-specific entry is registered.
// Today every source is HTML, so the fallback covers all traffic; per-source
// entries are added opportunistically as non-HTML sources land.
type PipelineRegistry struct {
	bySource map[string]Pipeline
	fallback Pipeline
}

// NewPipelineRegistry returns a Registry that serves fallback for every
// lookup until Register adds source-specific entries.
func NewPipelineRegistry(fallback Pipeline) *PipelineRegistry {
	return &PipelineRegistry{
		bySource: map[string]Pipeline{},
		fallback: fallback,
	}
}

// Register associates a Pipeline with a source ID. A later call for the same
// ID overwrites the prior entry.
func (r *PipelineRegistry) Register(sourceID string, p Pipeline) {
	r.bySource[sourceID] = p
}

// For returns the Pipeline registered for sourceID, or the fallback if none
// is registered. Empty sourceID always resolves to the fallback.
func (r *PipelineRegistry) For(sourceID string) Pipeline {
	if p, ok := r.bySource[sourceID]; ok {
		return p
	}
	return r.fallback
}
