# Analyzer Pipeline Runtime

Analyzer pipeline Roots are created from terminal collection batches only. A
source batch with `succeeded = false` is rejected; completed batches with no
failure flag are accepted.

The deployed pipeline definition is fixed to `configs/llm_pipeline.yaml`. The
API rejects request-specific definition paths, and the worker rejects a
different configured path, so the persisted definition hash is executable by
the worker that receives the P0 task.

Root creation snapshots candidate and content membership atomically with the
Root and `PIPELINE_INIT` task. Stages read those Root-keyed snapshots, so later
source mutations or soft deletes cannot change an existing pipeline run.

Use the `Idempotency-Key` request header when retrying pipeline creation. The
key is scoped to the input batch and pipeline definition hash. An identical
retry returns the existing `PIPELINE_INIT` task; reusing the key with a
different semantic request returns `409 Conflict`. Requests without a key
create independent runs.

Batch purposes distinguish `COLLECTION`, `ANALYZER_PIPELINE_ROOT`, and
`ANALYZER_PIPELINE_STAGE`. Collection and analyzer finishers only process the
purpose they own.
