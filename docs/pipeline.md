# Analyzer Pipeline Runtime

Analyzer pipeline Roots are created from terminal collection batches only. A
source batch with `succeeded = false` is rejected; completed batches with no
failure flag are accepted.

The deployed pipeline definition is fixed to `configs/llm_pipeline.yaml`. The
API rejects request-specific definition paths, and the worker rejects a
different configured path, so the persisted definition hash is executable by
the worker that receives the P0 task.

Root creation takes a `REPEATABLE READ` snapshot of the complete candidate and
content values used by analysis, atomically with the Root and `PIPELINE_INIT`
task. Stages embed those Root-keyed values rather than rereading live source
rows, so later metadata/body mutations and soft deletes cannot change an
existing pipeline run. Content already soft-deleted when the Root snapshot is
created is excluded.

Use the `Idempotency-Key` request header when retrying pipeline creation. The
key is scoped to the input batch and pipeline definition hash. An identical
retry returns the existing `PIPELINE_INIT` task; reusing the key with a
different semantic request returns `409 Conflict`. Requests without a key
create independent runs.

Batch purposes distinguish `COLLECTION`, `ANALYZER_PIPELINE_ROOT`, and
`ANALYZER_PIPELINE_STAGE`. Collection and analyzer finishers only process the
purpose they own.
