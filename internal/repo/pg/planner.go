package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
)

// PGPlanner persists a complete planner batch in one database transaction.
type PGPlanner struct {
	db DBTX
	q  *Queries
}

func (r *PGRepository) Planner() repo.PlannerResults {
	return &PGPlanner{db: r.db, q: r.q}
}

func (r *PGPlanner) PersistPlannerResult(ctx context.Context, arg repo.PersistPlannerResultParams) (repo.PlannerResult, error) {
	beginner, ok := r.db.(pgBeginner)
	if !ok {
		return repo.PlannerResult{}, fmt.Errorf("postgres repository does not support transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return repo.PlannerResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)
	result := repo.PlannerResult{}
	for _, extraction := range arg.Extractions {
		row, err := qtx.CreateContentExtraction(ctx, CreateContentExtractionParams{
			ContentID:     extraction.ContentID,
			ModelID:       arg.ModelID,
			PromptID:      arg.PromptID,
			SchemaName:    arg.SchemaName,
			SchemaVersion: arg.SchemaVersion,
			Title:         extraction.Title,
			Summary:       extraction.Summary,
			RawResult:     extraction.RawResult,
			TraceID:       arg.TraceID,
		})
		if err != nil {
			return repo.PlannerResult{}, fmt.Errorf("create extraction for content %s: %w", extraction.ContentID, err)
		}
		for _, entity := range extraction.Entities {
			entityRow, err := qtx.UpsertEntity(ctx, UpsertEntityParams{Canonical: entity.Canonical, Type: EntityType(entity.Type)})
			if err != nil {
				return repo.PlannerResult{}, fmt.Errorf("upsert entity %q: %w", entity.Canonical, err)
			}
			if err := qtx.CreateContentExtractionEntity(ctx, CreateContentExtractionEntityParams{
				ExtractionID: row.ID,
				EntityID:     entityRow.ID,
				Surface:      entity.Surface,
				Ordinal:      pgconv.Int16PtrToPgInt2(entity.Ordinal),
			}); err != nil {
				return repo.PlannerResult{}, fmt.Errorf("create extraction entity %q: %w", entity.Canonical, err)
			}
		}
		if err := qtx.ReplaceContentExtractionTopics(ctx, ReplaceContentExtractionTopicsParams{ExtractionID: row.ID, Column2: extraction.Topics}); err != nil {
			return repo.PlannerResult{}, fmt.Errorf("create extraction topics: %w", err)
		}
		if err := qtx.ReplaceContentExtractionPhrases(ctx, ReplaceContentExtractionPhrasesParams{ExtractionID: row.ID, Column2: extraction.Phrases}); err != nil {
			return repo.PlannerResult{}, fmt.Errorf("create extraction phrases: %w", err)
		}
		result.Extractions++
	}

	for _, task := range arg.Tasks {
		if _, err := createTaskRepo(ctx, qtx, task); err != nil {
			if errors.Is(err, repo.ErrTaskAlreadyActive) {
				if task.ExpiresAt != nil {
					if extendErr := qtx.ExtendActiveTaskExpiry(ctx, ExtendActiveTaskExpiryParams{
						SourceAbbr:  task.SourceAbbr,
						Kind:        TaskKind(task.Kind),
						PayloadHash: pgconv.StringPtrToPgText(task.PayloadHash),
						ExpiresAt:   pgconv.TimePtrToPgTimestamptz(task.ExpiresAt),
					}); extendErr != nil {
						return repo.PlannerResult{}, fmt.Errorf("extend task expiry: %w", extendErr)
					}
				}
				continue
			}
			return repo.PlannerResult{}, fmt.Errorf("create planner task: %w", err)
		}
		result.TasksCreated++
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.PlannerResult{}, err
	}
	return result, nil
}

var _ repo.PlannerResults = (*PGPlanner)(nil)
