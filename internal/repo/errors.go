package repo

import "errors"

// DefaultTaskRetryMax is the number of total task attempts before terminal failure.
const DefaultTaskRetryMax = 3

// ErrTaskAlreadyActive is returned by CreateTask when a task with the same
// (source_id, kind, payload_hash) already exists in PENDING or RUNNING state.
// Callers should treat this as an idempotent no-op, optionally extending the
// existing task's expires_at via ExtendActiveTaskExpiry.
var ErrTaskAlreadyActive = errors.New("task already active")

// ErrTaskNotFailed is returned when an operator attempts to retry a task that
// is not currently FAILED.
var ErrTaskNotFailed = errors.New("task is not failed")

// ErrPipelineInputNotTerminal is returned when pipeline creation references a
// source batch that has not finished collection yet.
var ErrPipelineInputNotTerminal = errors.New("pipeline input batch is not terminal")

// ErrPipelineInputNotFound is returned when pipeline creation references an
// unknown source batch.
var ErrPipelineInputNotFound = errors.New("pipeline input batch not found")
