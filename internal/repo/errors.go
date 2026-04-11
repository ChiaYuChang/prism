package repo

import "errors"

// ErrTaskAlreadyActive is returned by CreateTask when a task with the same
// (source_id, kind, payload_hash) already exists in PENDING or RUNNING state.
// Callers should treat this as an idempotent no-op, optionally extending the
// existing task's expires_at via ExtendActiveTaskExpiry.
var ErrTaskAlreadyActive = errors.New("task already active")
