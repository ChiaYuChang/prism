package pg

import (
	"fmt"
	"time"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func dbCreateTaskRowToRepoTask(row CreateTaskRow) repo.Task {
	return repo.Task{
		ID:             row.ID,
		BatchID:        row.BatchID,
		PreviousTaskID: pgconv.PgUUIDToUUIDPtr(row.PreviousTaskID),
		NextTaskID:     pgconv.PgUUIDToUUIDPtr(row.NextTaskID),
		LogicalKey:     pgconv.PgTextToStringPtr(row.LogicalKey),
		TraceID:        row.TraceID,
		Kind:           string(row.Kind),
		SourceType:     string(row.SourceType),
		SourceAbbr:     row.SourceAbbr,
		URL:            row.Url,
		Payload:        row.Payload,
		PayloadHash:    pgconv.PgTextToStringPtr(row.PayloadHash),
		Meta:           row.Meta,
		NextRunAt:      *pgconv.PgTimestamptzToTimePtr(row.NextRunAt),
		ExpiresAt:      pgconv.PgTimestamptzToTimePtr(row.ExpiresAt),
		Status:         repo.TaskStatus(row.Status),
		RetryCount:     int(row.RetryCount),
		FailureMessage: pgconv.PgTextToStringPtr(row.FailureMessage),
		LastRunAt:      pgconv.PgTimestamptzToTimePtr(row.LastRunAt),
		CreatedAt:      *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		UpdatedAt:      *pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
	}
}

func dbTaskToRepoTask(task Task) repo.Task {
	return repo.Task{
		ID:             task.ID,
		BatchID:        task.BatchID,
		PreviousTaskID: pgconv.PgUUIDToUUIDPtr(task.PreviousTaskID),
		NextTaskID:     pgconv.PgUUIDToUUIDPtr(task.NextTaskID),
		LogicalKey:     pgconv.PgTextToStringPtr(task.LogicalKey),
		TraceID:        task.TraceID,
		Kind:           string(task.Kind),
		SourceType:     string(task.SourceType),
		SourceAbbr:     task.SourceAbbr,
		URL:            task.Url,
		Payload:        task.Payload,
		PayloadHash:    pgconv.PgTextToStringPtr(task.PayloadHash),
		Meta:           task.Meta,
		NextRunAt:      *pgconv.PgTimestamptzToTimePtr(task.NextRunAt),
		ExpiresAt:      pgconv.PgTimestamptzToTimePtr(task.ExpiresAt),
		Status:         repo.TaskStatus(task.Status),
		RetryCount:     int(task.RetryCount),
		FailureMessage: pgconv.PgTextToStringPtr(task.FailureMessage),
		LastRunAt:      pgconv.PgTimestamptzToTimePtr(task.LastRunAt),
		CreatedAt:      *pgconv.PgTimestamptzToTimePtr(task.CreatedAt),
		UpdatedAt:      *pgconv.PgTimestamptzToTimePtr(task.UpdatedAt),
	}
}

func dbRetryFailedTaskRowToRepoTask(row RetryFailedTaskRow) repo.Task {
	return repo.Task{
		ID:             row.ID,
		BatchID:        row.BatchID,
		PreviousTaskID: pgconv.PgUUIDToUUIDPtr(row.PreviousTaskID),
		NextTaskID:     pgconv.PgUUIDToUUIDPtr(row.NextTaskID),
		LogicalKey:     pgconv.PgTextToStringPtr(row.LogicalKey),
		TraceID:        row.TraceID,
		Kind:           string(row.Kind),
		SourceType:     string(row.SourceType),
		SourceAbbr:     row.SourceAbbr,
		URL:            row.Url,
		Payload:        row.Payload,
		PayloadHash:    pgconv.PgTextToStringPtr(row.PayloadHash),
		Meta:           row.Meta,
		NextRunAt:      *pgconv.PgTimestamptzToTimePtr(row.NextRunAt),
		ExpiresAt:      pgconv.PgTimestamptzToTimePtr(row.ExpiresAt),
		Status:         repo.TaskStatus(row.Status),
		RetryCount:     int(row.RetryCount),
		FailureMessage: pgconv.PgTextToStringPtr(row.FailureMessage),
		LastRunAt:      pgconv.PgTimestamptzToTimePtr(row.LastRunAt),
		CreatedAt:      *pgconv.PgTimestamptzToTimePtr(row.CreatedAt),
		UpdatedAt:      *pgconv.PgTimestamptzToTimePtr(row.UpdatedAt),
	}
}

func dbScheduleToRepoSchedule(s Schedule) repo.Schedule {
	return repo.Schedule{
		ID:                     s.ID,
		Name:                   s.Name,
		Enabled:                s.Enabled,
		ConfigPresent:          s.ConfigPresent,
		ConfigHash:             s.ConfigHash,
		Kind:                   string(s.Kind),
		SourceType:             string(s.SourceType),
		SourceAbbr:             s.SourceAbbr,
		URL:                    s.Url,
		Payload:                s.Payload,
		Meta:                   s.Meta,
		RunOnInsert:            s.RunOnInsert,
		NextFireAt:             *pgconv.PgTimestamptzToTimePtr(s.NextFireAt),
		LastFireAt:             pgconv.PgTimestamptzToTimePtr(s.LastFireAt),
		LastMaterializedAt:     pgconv.PgTimestamptzToTimePtr(s.LastMaterializedAt),
		LastMaterializedTaskID: pgconv.PgUUIDToUUIDPtr(s.LastMaterializedTaskID),
		LastError:              pgconv.PgTextToStringPtr(s.LastError),
		CreatedAt:              *pgconv.PgTimestamptzToTimePtr(s.CreatedAt),
		UpdatedAt:              *pgconv.PgTimestamptzToTimePtr(s.UpdatedAt),
	}
}

func dbBatchToRepoBatch(
	id uuid.UUID,
	parentID *uuid.UUID,
	nSubtasks *int32,
	parentTaskID *uuid.UUID,
	succeeded *bool,
	sourceType string,
	traceID *string,
	createdAt time.Time,
	updatedAt time.Time,
	completedAt *time.Time,
	publishedAt *time.Time,
	lastPublishAttemptAt *time.Time,
	publishRetryCount int32,
	publishError *string,
	stalledAt *time.Time,
) repo.Batch {
	return repo.Batch{
		ID:                   id,
		ParentID:             parentID,
		NSubtasks:            nSubtasks,
		ParentTaskID:         parentTaskID,
		Succeeded:            succeeded,
		SourceType:           sourceType,
		TraceID:              traceID,
		CreatedAt:            createdAt,
		UpdatedAt:            updatedAt,
		CompletedAt:          completedAt,
		PublishedAt:          publishedAt,
		LastPublishAttemptAt: lastPublishAttemptAt,
		PublishRetryCount:    int(publishRetryCount),
		PublishError:         publishError,
		StalledAt:            stalledAt,
	}
}

func dbSourceToRepoSource(s Source) repo.Source {
	return repo.Source{
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
		BatchID:         pgconv.PgUUIDToUUID(c.BatchID),
		Fingerprint:     c.Fingerprint,
		SourceAbbr:      c.SourceAbbr,
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
		BatchID:     pgconv.PgUUIDToUUID(c.BatchID),
		Type:        string(c.Type),
		SourceAbbr:  c.SourceAbbr,
		CandidateID: pgconv.PgUUIDToUUID(c.CandidateID),
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
		PublishDate: pgDateToTimePtr(m.PublishDate),
		URL:         pgconv.PgTextToStringPtr(m.Url),
		Tag:         pgconv.PgTextToStringPtr(m.Tag),
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(m.CreatedAt),
		DeletedAt:   pgconv.PgTimestamptzToTimePtr(m.DeletedAt),
	}
}

func dbPromptVersionRowToRepoPromptVersion(
	id uuid.UUID,
	name string,
	version int32,
	hash string,
	sizeBytes int64,
	createdAt pgtype.Timestamptz,
) repo.PromptVersion {
	return repo.PromptVersion{
		ID:        id,
		Name:      name,
		Version:   version,
		Hash:      hash,
		SizeBytes: sizeBytes,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(createdAt),
	}
}

func dbTokenToRepoToken(t Token) (repo.Token, error) {
	if t.Permissions < 0 || t.Permissions > 255 {
		return repo.Token{}, fmt.Errorf("token %s has invalid permissions %d", t.ID, t.Permissions)
	}
	return repo.Token{
		ID:            t.ID,
		Type:          t.Type,
		Name:          t.Name,
		Permissions:   uint8(t.Permissions),
		HashAlgorithm: t.HashAlgorithm,
		TokenHash:     t.TokenHash,
		CreatedAt:     *pgconv.PgTimestamptzToTimePtr(t.CreatedAt),
		ExpiresAt:     *pgconv.PgTimestamptzToTimePtr(t.ExpiresAt),
		LastUsedAt:    pgconv.PgTimestamptzToTimePtr(t.LastUsedAt),
		RenewedAt:     pgconv.PgTimestamptzToTimePtr(t.RenewedAt),
		RotatedAt:     pgconv.PgTimestamptzToTimePtr(t.RotatedAt),
		RevokedAt:     pgconv.PgTimestamptzToTimePtr(t.RevokedAt),
	}, nil
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
		InputHash:   e.InputHash,
		TraceID:     e.TraceID,
		CreatedAt:   *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}

func dbContentEmbeddingToRepoContentEmbedding(e ContentEmbeddingsGemma2025) repo.ContentEmbedding {
	return repo.ContentEmbedding{
		ID:        e.ID,
		ContentID: e.ContentID,
		ModelID:   e.ModelID,
		InputHash: e.InputHash,
		TraceID:   e.TraceID,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
		DeletedAt: pgconv.PgTimestamptzToTimePtr(e.DeletedAt),
	}
}

func dbCandidateEmbeddingRowToRepoEmbeddingRecord(e ListCandidateEmbeddingsGemma2025Row) repo.EmbeddingRecord {
	return repo.EmbeddingRecord{
		ID:        e.ID,
		TargetID:  e.CandidateID,
		ModelID:   e.ModelID,
		Category:  string(e.Category),
		InputHash: e.InputHash,
		TraceID:   e.TraceID,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}

func dbContentEmbeddingRowToRepoEmbeddingRecord(e ListContentEmbeddingsGemma2025Row) repo.EmbeddingRecord {
	return repo.EmbeddingRecord{
		ID:        e.ID,
		TargetID:  e.ContentID,
		ModelID:   e.ModelID,
		InputHash: e.InputHash,
		TraceID:   e.TraceID,
		CreatedAt: *pgconv.PgTimestamptzToTimePtr(e.CreatedAt),
	}
}

func pgDateToTimePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}
