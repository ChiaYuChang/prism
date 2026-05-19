package pg

import (
	"github.com/ChiaYuChang/prism/internal/repo"
	"github.com/ChiaYuChang/prism/pkg/pgconv"
)

func repoListCandidatesParamsToDB(arg repo.ListCandidatesParams) ListCandidatesParams {
	return ListCandidatesParams{
		Query:      pgconv.StringPtrToPgText(arg.Query),
		SourceAbbr: pgconv.StringPtrToPgText(arg.SourceAbbr),
		Since:      pgconv.TimePtrToPgTimestamptz(arg.Since),
		Until:      pgconv.TimePtrToPgTimestamptz(arg.Until),
		Lim:        arg.Limit,
		Off:        arg.Offset,
	}
}

func repoCreateTaskParamsToEnsureBatchExists(arg repo.CreateTaskParams) EnsureBatchExistsParams {
	return EnsureBatchExistsParams{
		ID:         arg.BatchID,
		SourceType: SourceType(arg.SourceType),
		TraceID:    pgconv.StringPtrToPgText(&arg.TraceID),
	}
}

func repoCreateTaskParamsToDB(arg repo.CreateTaskParams) CreateTaskParams {
	return CreateTaskParams{
		BatchID:     arg.BatchID,
		Kind:        TaskKind(arg.Kind),
		SourceType:  SourceType(arg.SourceType),
		SourceAbbr:  arg.SourceAbbr,
		Url:         arg.URL,
		Payload:     arg.Payload,
		PayloadHash: pgconv.StringPtrToPgText(arg.PayloadHash),
		Meta:        arg.Meta,
		TraceID:     arg.TraceID,
		Frequency:   pgconv.DurationPtrToPgInterval(arg.Frequency),
		NextRunAt:   pgconv.TimePtrToPgTimestamptz(arg.NextRunAt),
		ExpiresAt:   pgconv.TimePtrToPgTimestamptz(arg.ExpiresAt),
	}
}

func repoExtendActiveTaskExpiryParamsToDB(arg repo.ExtendActiveTaskExpiryParams) ExtendActiveTaskExpiryParams {
	return ExtendActiveTaskExpiryParams{
		SourceAbbr:  arg.SourceAbbr,
		Kind:        TaskKind(arg.Kind),
		PayloadHash: pgconv.StringPtrToPgText(&arg.PayloadHash),
		ExpiresAt:   pgconv.TimePtrToPgTimestamptz(arg.ExpiresAt),
	}
}

func repoCreateContentParamsToDB(arg repo.CreateContentParams) CreateContentParams {
	return CreateContentParams{
		BatchID:     pgconv.UUIDToPgUUID(arg.BatchID),
		Type:        ContentType(arg.Type),
		SourceAbbr:  arg.SourceAbbr,
		CandidateID: pgconv.UUIDToPgUUID(arg.CandidateID),
		Url:         arg.URL,
		Title:       arg.Title,
		Content:     arg.Content,
		Author:      pgconv.StringPtrToPgText(arg.Author),
		TraceID:     arg.TraceID,
		PublishedAt: pgconv.TimePtrToPgTimestamptz(&arg.PublishedAt),
		FetchedAt:   pgconv.TimePtrToPgTimestamptz(&arg.FetchedAt),
		Metadata:    arg.Metadata,
	}
}

func repoUpdateContentMetadataParamsToDB(arg repo.UpdateContentMetadataParams) UpdateContentMetadataParams {
	return UpdateContentMetadataParams{
		Author:      pgconv.StringPtrToPgText(arg.Author),
		PublishedAt: pgconv.TimePtrToPgTimestamptz(arg.PublishedAt),
		Metadata:    arg.Metadata,
		ID:          arg.ID,
	}
}
