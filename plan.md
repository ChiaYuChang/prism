# Project Prism — Plan Index

Plan content has been split into four focused documents under `docs/plan/`.

- [`docs/plan/spec.md`](docs/plan/spec.md) — Stable: project objective, F-M-T-(S||P) architecture, discovery model, infrastructure mapping, design clarifications.
- [`docs/plan/todo.md`](docs/plan/todo.md) — Current sprint and pending checklist items (Phase 1.x, 2.7–2.9, 3, 4, plus Immediate Next Steps).
- [`docs/plan/done.md`](docs/plan/done.md) — Append-only history of completed work, dated.
- [`docs/plan/future.md`](docs/plan/future.md) — Future Roadmap, deferred refactors, anti-patterns to avoid.

## Quick links to long-lived design decisions

- Pipeline shape: `docs/plan/spec.md` §2 Architecture
- Trigger classes (`schedule` / `resource` / `manual`): `docs/plan/spec.md` §2.2
- Archive catalog refactor (deferred): `docs/plan/future.md` §Move archive metadata into PG
- Cloud anti-patterns: `docs/plan/spec.md` §6 Design Clarifications (under "Avoid cloud anti-patterns by design")
- SQS + Lambda dual-mode deployment: `docs/plan/future.md` §Dual-mode deployment target
