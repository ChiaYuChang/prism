# Security — Threat Model and Secret Handling

This document captures the threat model, audit surfaces, and operational
runbooks for secret handling in Prism. Companion to the compose
restructure landed in commits `38716c4` → `9418956` (Phase 4 of
`docs/integration-test-plan.md`).

## 1. Threat model

### 1.1 Actors

| Actor | Capability | In scope? |
|---|---|---|
| Same-uid local process (dev box) | Read `/proc/<pid>/environ`, `ps -ef` argv, `~/.secrets/`, docker socket | dev: accepted (single-user FDE laptop); prod: N/A |
| Other-uid local user | Blocked by FS perms (`chmod 0600` on baked outputs, `umask 077` in scripts) | dev + prod |
| Container co-tenant on same daemon | Docker socket = root-equivalent; can read any container env via `docker inspect` | prod: workers must use docker secrets / `*_FILE`, not env |
| Network attacker | TLS for Postgres / NATS / Valkey at boundary | prod |
| Git history reader | Anyone with repo read access | dev + prod (gitignore enforced) |
| Build-context reader (image layers) | Anyone with image pull access | prod (`.dockerignore` enforced) |
| Log aggregator / stdout reader | Reads worker logs | dev + prod (mask via `LogValue()`) |
| Backup / disk-image holder | Reads disk at rest | out of scope (FDE assumed dev; KMS prod) |

### 1.2 Trust boundaries

1. **Host FS ↔ same-uid process** — trusted in dev (single-user FDE
   laptop). Not trusted in prod (no shared hosts).
2. **Process ↔ child process argv** — untrusted everywhere. Argv is
   visible to any uid via `/proc/<pid>/cmdline`. Never pass secrets as
   flags.
3. **Process ↔ env** — trusted same-uid only. Prod never carries
   secrets in env; uses `*_FILE` + docker secret mount.
4. **Repo ↔ git remote** — untrusted. All secret files gitignored;
   `.secrets/`, `env/local/`, `.*.docker-compose.merged.yaml` are
   covered by single-directory rules in `.gitignore` so new files added
   under those paths are excluded automatically.
5. **Build context ↔ image** — untrusted. `.dockerignore` excludes
   `.secrets`, `env/local`, `.git`, `*.pem`, `*.key`, baked compose
   files, `tmp/`, `logs/`.
6. **Worker ↔ log sink** — untrusted. Secrets must never reach `slog`
   output. DSN-bearing structs implement `LogValue()` / `String()` with
   redaction. Audited by grepping for raw secret literals in logs.

### 1.3 Assets

| Asset | Storage today | Status |
|---|---|---|
| `POSTGRES_APP_PASSWORD` | `.secrets/pg-prism` | bake-managed via `secrets-bake.sh` (planned migration of bake map) |
| `POSTGRES_ADMIN_PASSWORD` | `.secrets/pg-admin` | file-based, read via `cat $POSTGRES_ADMIN_PASSWORD_FILE` |
| `NATS_AUTH_TOKEN` | `.secrets/nats-auth-token` | file-based, read via `cat $NATS_AUTH_TOKEN_FILE` |
| `VALKEY_APP_PASSWORD` | `.secrets/valkey_prism` → bake → `env/local/<env>.local.env` | bake-managed (in `secrets-bake.sh` MAP) |
| `VALKEY_ADMIN_PASSWORD` | `.secrets/valkey_admin` | file-based |
| `SEAWEEDFS_*` | `.secrets/seaweedfs` | file-based |
| LLM provider API keys (Gemini / OpenAI) | `.secrets/` | file-based |
| `BRAVE_SEARCH_API` | env var (`env/local/<env>.user.env`) | deferred — should move to `.secrets/` |
| `PGADMIN_PASSWORD` | env var | deferred — same |

### 1.4 Attacks considered + mitigation

| Attack | Vector | Mitigation |
|---|---|---|
| Argv leak | `ps -ef`, shell history, exec audit | Worker binaries read secrets from env (`PRISM_<APP>_*` prefixes) or `*_FILE` only. No flag carries a secret. `migrate` / `psql` argv leak in dev = accepted, deferred. |
| Process env dump | `/proc/<pid>/environ`, `docker inspect` | dev: accepted (same-uid FDE). prod: docker secrets + `*_FILE` mount; env carries no secret. |
| Log leak | stdout / stderr capture, log aggregator | DSN / credential structs implement `LogValue()` / `String()` with redaction. Audit via grep for raw literals. |
| Image layer leak | `docker history`, registry pull | `.dockerignore` excludes secret dirs; multi-stage build with distroless runtime carries no `.git` / build cache. |
| Git history leak | repo clone | `.gitignore` covers `.secrets/`, `env/local/`, `.*.docker-compose.merged.yaml` via directory-level rules. Manual audit on staging. |
| Merged compose plaintext | `.<env>-<profiles>.docker-compose.merged.yaml` could resolve `${VAR}` to plaintext at bake | `compose-bake.sh` runs `docker compose config --no-interpolate` so `${VAR}` references stay literal in the merged file. `umask 077` + final `chmod 0600` as defense-in-depth. Gitignored + dockerignored. |
| Prod accident | `task compose:up ENV=prod` from dev box | `compose-bake.sh` refuses `ENV=prod` without `PRISM_PROD_OK=1`. `secrets-bake.sh` refuses `ENV=prod` outright (prod secrets come from the deployment platform). |
| Rotation-test clobber | Validating the rotation runbook overwrites real `.secrets/` | Rotation tests use an isolated `.secrets-test/` (gitignored under `.secrets/` parent rule? — see §3.3); never mutate real `.secrets/` for procedure validation. |

### 1.5 Out of scope

- Disk-at-rest (assumes FDE on dev laptop; KMS / encrypted EBS in prod).
- Supply-chain (Go module / base image provenance) — separate effort.
- Insider with root on prod host — assumed trusted.
- Side-channel attacks (timing, cache) — not applicable to this app
  surface.

### 1.6 Dev vs prod summary

| Dimension | Dev / test | Prod |
|---|---|---|
| Secret transport | env from `env/local/<env>.local.env` (bake) + direct `*_FILE` reads | docker secret → `*_FILE` mount; never env |
| Argv | clean (no flags) | clean |
| Process env | secrets present (accepted) | secrets absent |
| Compose merged file | `0600`, gitignored, `--no-interpolate` keeps `${VAR}` literal | not used at runtime; secrets via docker secret refs |
| Restart policy | `no` (`docker-compose.test.yaml`) | `unless-stopped` (`docker-compose.prod.yaml`) |
| Host port exposure | loopback only | none |
| Prod gate | N/A | `PRISM_PROD_OK=1` required for `compose-bake.sh ENV=prod` |

## 2. Where secrets live

### 2.1 Source of truth — `.secrets/`

Per-credential files at repo root, owner-readable only. Each file holds
one credential, no trailing newline. Gitignored. Examples:

```
.secrets/pg-admin
.secrets/pg-prism
.secrets/nats-auth-token
.secrets/valkey_admin
.secrets/valkey_prism
.secrets/seaweedfs
```

The Taskfile reads these directly via `sh: cat $POSTGRES_APP_PASSWORD_FILE`
expressions in `vars:` blocks. The path-pointer env vars
(`POSTGRES_APP_PASSWORD_FILE` etc.) come from `env/base.env`.

### 2.2 Bake target — `env/local/<env>.local.env`

`script/secrets-bake.sh` reads selected `.secrets/*` entries (currently
just `valkey_prism → VALKEY_APP_PASSWORD`) and writes them to
`env/local/<env>.local.env` for Taskfile dotenv layering. Gitignored.
Mode `0600`. Regenerate after rotation.

`env/local/` also holds `<env>.user.env` for user-managed inline secrets
(API keys, dev-tool passwords, per-developer session IDs). Gitignored.

### 2.3 Bake target — `.<env>-<profiles>.docker-compose.merged.yaml`

`script/compose-bake.sh` runs `docker compose config --no-interpolate`
across the four-file `-f` list (base + `<env>` overlay + tools +
worker) and writes one merged file per `(ENV, PROFILES)` combo at repo
root. `${VAR}` references stay literal — secrets are resolved at
`compose up` time by the Taskfile env block, not baked in.
Gitignored. Mode `0600`.

### 2.4 Repo-tracked, non-secret defaults

- `env/base.env` — cross-environment defaults (file paths, DB names,
  ports). No credentials.
- `env/test.env` — test-environment non-secret overrides.
- `deployments/docker-compose*.yaml` — base + overlays + tools +
  worker. All reference `${VAR}`; no inline credentials.

`env/prod.env` is intentionally **not** in the repo. Prod values come
from the deployment platform.

## 3. Operational runbooks

### 3.1 Bring up a stack

```bash
# 1. Bake env/local/<env>.local.env from .secrets/*
ENV=test ./script/secrets-bake.sh

# 2. Bake .<env>-<profiles>.docker-compose.merged.yaml
#    (compose:bake task delegates to script/compose-bake.sh)
task compose:bake ENV=test COMPOSE_PROFILES=dev,obs

# 3. Start infra stack
task compose:up ENV=test COMPOSE_PROFILES=dev,obs

# 4. Optionally start containerized workers
task compose:worker
```

### 3.2 Rotate a credential

For a real credential change (e.g. compromised token, scheduled rotation):

```bash
# 1. Replace the secret on disk
printf '%s' "$NEW_VALUE" > .secrets/<file>
chmod 0600 .secrets/<file>

# 2. Re-bake the env layer
ENV=test ./script/secrets-bake.sh

# 3. Restart workers so they re-read env
task compose:worker:down
task compose:worker
```

For DB-side rotation (Postgres, Valkey, NATS), update the credential in
the service first (or restart the service so the file-based init
re-applies), then rotate the worker side as above.

### 3.3 Validate the rotation procedure

> ⚠️ Never mutate real `.secrets/` to test the rotation procedure. The
> live values are the only source of truth and overwriting them risks
> breaking dev / prod stacks.

Use an isolated dir:

```bash
# 1. Snapshot current secrets to a throwaway dir
mkdir -m 0700 .secrets-test
cp .secrets/* .secrets-test/

# 2. Point the bake script at it (script currently hard-codes
#    SECRETS_DIR=.secrets — apply a one-line override or copy the
#    script). A future patch should accept SECRETS_DIR from env.
SECRETS_DIR=.secrets-test ENV=test ./script/secrets-bake.sh

# 3. Mutate .secrets-test/<file>, re-run, verify env/local/test.local.env
#    reflects the new value. Real .secrets/ is untouched.

# 4. Tear down
rm -rf .secrets-test
ENV=test ./script/secrets-bake.sh    # restore real env/local/test.local.env
```

`.secrets-test/` is covered by the `.secrets*` glob in `.gitignore` /
`.dockerignore` and never shipped.

### 3.4 Audit checklist

Run periodically and before any release:

```bash
# 1. argv hygiene
ps -ef | grep -E '(scheduler|discovery|collector)' | grep -E '(password|token|api[_-]?key)'
# expect: no matches

# 2. process env hygiene (dev: matches expected; prod: should be empty)
sudo cat /proc/$(pgrep -f '^./bin/scheduler')/environ | tr '\0' '\n' | grep -i -E '(password|token)'

# 3. log redaction
grep -i -E '(password|token|api[_-]?key)=[A-Za-z0-9]' logs/*.log
# expect: no raw values, only LogValue()-masked forms

# 4. git history
git log --all -p -- .secrets/ env/local/ .*.docker-compose.merged.yaml
# expect: no commits

# 5. build context
docker build -f deployments/Dockerfile.worker --no-cache -t prism-audit . 2>&1 | grep -i -E '(.secrets|env/local)'
# expect: no matches (dockerignore excluded)
```

## 4. Migration notes (this refactor)

The compose / secret layout was reorganized over commits
`38716c4` → `9418956`:

- **`38716c4`** — workers profile / file renamed singular (`worker`).
- **`540d37c`** — `script/compose-bake.sh` introduced; per-`(ENV,
  PROFILES)` merged file path; `PRISM_PROD_OK=1` gate; teardown closes
  worker + tool profiles before `compose:clean`.
- **`6b3c70e`** — discovery worker ignores unsupported task kinds
  (was failing PAGE_FETCH it does not own).
- **`9418956`** — `CompleteTask` / `FailTask` only flip rows still in
  `RUNNING`; prevents late status clobbering.

Earlier commits (`security(workers): keep secrets off argv`,
`security(workers): file-based secret loading`,
`feat(env): layered dotenv + secrets-bake`,
`refactor(compose): split base/test/prod overlays`) are documented in
`journal/202605050100.md` and `journal/202605050147.md`.

Phase 4 of `docs/integration-test-plan.md` is closed. Verification on
2026-05-05: DIRECTORY_FETCH=3 COMPLETED, PAGE_FETCH=26 COMPLETED,
contents dpp=10 / kmt=10 / tpp=6.

## 5. Future work (deferred, tracked in `docs/plan/todo.md` / `future.md`)

- Move `BRAVE_SEARCH_API` and `PGADMIN_PASSWORD` from env vars into
  `.secrets/`.
- Resolve `migrate` / `psql` argv leak (dev-only, low priority).
- Patch `secrets-bake.sh` to accept `SECRETS_DIR` env var override (so
  rotation-procedure validation does not require editing the script).
- Phase 5 — testcontainers-go + GH Actions CI; prod path end-to-end
  validation (gated by `PRISM_PROD_OK=1`).
