# Project Prism — Plan Index

Plan content is maintained in four focused documents under
`deployments/docs/plan/`.

- [`deployments/docs/plan/spec.md`](deployments/docs/plan/spec.md) — Stable architecture and design decisions.
- [`deployments/docs/plan/todo.md`](deployments/docs/plan/todo.md) — Active server-side work and deployment validation.
- [`deployments/docs/plan/done.md`](deployments/docs/plan/done.md) — Append-only completed-work history.
- [`deployments/docs/plan/future.md`](deployments/docs/plan/future.md) — Deliberately deferred roadmap items.

## Quick links to long-lived design decisions

- Pipeline shape: `deployments/docs/plan/spec.md` §2
- Trigger classes (`schedule` / `resource` / `manual`): `deployments/docs/plan/spec.md` §2.2
- Archive catalog refactor: `deployments/docs/plan/future.md` §Move archive metadata into PG
- Cloud anti-patterns: `deployments/docs/plan/spec.md` §6
- SQS + Lambda dual-mode deployment: `deployments/docs/plan/future.md` §Dual-mode deployment target
