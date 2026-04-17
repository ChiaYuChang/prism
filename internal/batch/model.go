package batch

import (
	"errors"

	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/google/uuid"
)

var (
	ErrParamMissing = errors.New("param missing")
)

type CompletedBatch struct {
	BatchID    uuid.UUID
	SourceType string
	TraceID    string
}

type BatchProgress struct {
	BatchID         uuid.UUID
	SourceType      string
	TraceID         string
	TotalTasks      int
	CompletedTasks  int
	CandidateCount  int64
	ContentCount    int
	TaskIDsByStatus map[repo.TaskStatus][]uuid.UUID
}

func (p BatchProgress) IsCompleted() bool {
	return p.TotalTasks > 0 &&
		p.TotalTasks == p.CompletedTasks &&
		p.CandidateCount > 0 &&
		int64(p.ContentCount) >= p.CandidateCount
}

func (p BatchProgress) GetStatus() string {
	if p.TotalTasks == 0 {
		return "PENDING"
	}
	if p.IsCompleted() {
		return "COMPLETED"
	}
	return "IN_PROGRESS"
}
