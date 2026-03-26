package pg

import (
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
)

func dbTaskToRepoTask(task Task) repo.Task {
	return repo.Task{
		ID:         task.ID,
		BatchID:    task.BatchID,
		TraceID:    task.TraceID,
		Kind:       string(task.Kind),
		SourceType: string(task.SourceType),
		SourceID:   task.SourceID,
		URL:        task.Url,
		Payload:    task.Payload,
		NextRunAt:  *pgconv.PgTimestamptzToTimePtr(task.NextRunAt),
		ExpiresAt:  pgconv.PgTimestamptzToTimePtr(task.ExpiresAt),
		Status:     string(task.Status),
		RetryCount: int(task.RetryCount),
		LastRunAt:  pgconv.PgTimestamptzToTimePtr(task.LastRunAt),
		CreatedAt:  *pgconv.PgTimestamptzToTimePtr(task.CreatedAt),
		UpdatedAt:  *pgconv.PgTimestamptzToTimePtr(task.UpdatedAt),
	}
}

func dbSourceToRepoSource(s Source) repo.Source {
	return repo.Source{
		ID:        s.ID,
		Abbr:      s.Abbr,
		Name:      s.Name,
		Type:      string(s.Type),
		BaseURL:   s.BaseUrl,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(s.CreatedAt),
		DeletedAt: pgconv.PgTimestamptzToTimePtr(s.DeletedAt),
	}
}

func dbCandidateToRepoCandidate(c Candidate) repo.Candidate {
	return repo.Candidate{
		ID:              c.ID,
		BatchID:         pgconv.PgUUIDToUUIDPtr(c.BatchID),
		Fingerprint:     c.Fingerprint,
		SourceID:        c.SourceID,
		Title:           c.Title,
		URL:             c.Url,
		Description:     pgconv.PgTextToStringPtr(c.Description),
		PublishedAt:     pgconv.PgTimestamptzToTimePtr(c.PublishedAt),
		DiscoveredAt:    *pgconv.PgTimestamptzToTimePtr(c.DiscoveredAt),
		TraceID:         c.TraceID,
		IngestionMethod: string(c.IngestionMethod),
		Metadata:        c.Metadata,
		CreatedAt:       *pgconv.PgTimestamptzToTimePtr(c.CreatedAt),
	}
}

func dbContentToRepoContent(c Content) repo.Content {
	return repo.Content{
		ID:          c.ID,
		BatchID:     pgconv.PgUUIDToUUIDPtr(c.BatchID),
		Type:        string(c.Type),
		SourceID:    c.SourceID,
		CandidateID: pgconv.PgUUIDToUUIDPtr(c.CandidateID),
		URL:         c.Url,
		Title:       c.Title,
		Content:     c.Content,
		Author:      pgconv.PgTextToStringPtr(c.Author),
		TraceID:     c.TraceID,
		PublishedAt: *pgconv.PgTimestamptzToTimePtr(c.PublishedAt),
		FetchedAt:   *pgconv.PgTimestamptzToTimePtr(c.FetchedAt),
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(c.CreatedAt),
		DeletedAt:   pgconv.PgTimestamptzToTimePtr(c.DeletedAt),
		Metadata:    c.Metadata,
	}
}

func dbModelToRepoModel(m Model) repo.Model {
	return repo.Model{
		ID:          m.ID,
		Name:        m.Name,
		Provider:    m.Provider,
		Type:        string(m.Type),
		PublishDate: nil,
		URL:         pgconv.PgTextToStringPtr(m.Url),
		Tag:         pgconv.PgTextToStringPtr(m.Tag),
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(m.CreatedAt),
		DeletedAt:   pgconv.PgTimestamptzToTimePtr(m.DeletedAt),
	}
}

func dbPromptToRepoPrompt(p Prompt) repo.Prompt {
	return repo.Prompt{
		ID:        p.ID,
		Hash:      p.Hash,
		Path:      p.Path,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(p.CreatedAt),
	}
}

func dbContentExtractionToRepoContentExtraction(c ContentExtraction) repo.ContentExtraction {
	return repo.ContentExtraction{
		ID:            c.ID,
		ContentID:     c.ContentID,
		ModelID:       c.ModelID,
		PromptID:      c.PromptID,
		SchemaName:    c.SchemaName,
		SchemaVersion: c.SchemaVersion,
		Title:         c.Title,
		Summary:       c.Summary,
		RawResult:     c.RawResult,
		TraceID:       c.TraceID,
		CreatedAt:     *pgconv.PgTimestamptzToTimePtr(c.CreatedAt),
	}
}

func dbEntityToRepoEntity(e Entity) repo.Entity {
	return repo.Entity{
		ID:        e.ID,
		Canonical: e.Canonical,
		Type:      string(e.Type),
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}

func dbCandidateEmbeddingToRepoCandidateEmbedding(e CandidateEmbeddingsGemma2025) repo.CandidateEmbedding {
	return repo.CandidateEmbedding{
		ID:          e.ID,
		CandidateID: e.CandidateID,
		ModelID:     e.ModelID,
		Category:    string(e.Category),
		TraceID:     e.TraceID,
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}

func dbContentEmbeddingToRepoContentEmbedding(e ContentEmbeddingsGemma2025) repo.ContentEmbedding {
	return repo.ContentEmbedding{
		ID:        e.ID,
		ContentID: e.ContentID,
		ModelID:   e.ModelID,
		Category:  string(e.Category),
		TraceID:   e.TraceID,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}
