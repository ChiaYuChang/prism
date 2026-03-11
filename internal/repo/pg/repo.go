package pg

import (
	"context"

	"github.com/ChiaYuChang/prism/internal/model"
	"github.com/google/uuid"
)

type PostgresRepository struct {
	q *Queries
}

func NewPostgresRepository(db DBTX) *PostgresRepository {
	return &PostgresRepository{
		q: New(db),
	}
}

// ClaimSearchTasks implements repo.SearchTaskRepository.
func (r *PostgresRepository) ClaimSearchTasks(ctx context.Context, limit int32) ([]model.SearchTask, error) {
	rows, err := r.q.ClaimSearchTasks(ctx, limit)
	if err != nil {
		return nil, err
	}

	tasks := make([]model.SearchTask, len(rows))
	for i, row := range rows {
		tasks[i] = model.SearchTask{
			ID:         row.ID,
			ContentID:  row.ContentID.Bytes,
			Phrases:    row.Phrases,
			TraceID:    row.TraceID,
			RetryCount: int(row.RetryCount.Int32),
			NextRunAt:  row.NextRunAt.Time,
		}
	}
	return tasks, nil
}

// CompleteSearchTask implements repo.SearchTaskRepository.
func (r *PostgresRepository) CompleteSearchTask(ctx context.Context, id uuid.UUID) error {
	return r.q.CompleteSearchTask(ctx, id)
}

// FailSearchTask implements repo.SearchTaskRepository.
func (r *PostgresRepository) FailSearchTask(ctx context.Context, id uuid.UUID) error {
	return r.q.FailSearchTask(ctx, id)
}
