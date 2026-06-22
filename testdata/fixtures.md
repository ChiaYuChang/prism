# Integration Test Fixtures

`testdata/synthetic/` holds committed, hand-written fixtures with original fake text. These are safe for CI and preserve only the DOM/feed shapes needed by parser and scout tests.

`testdata/real/` holds local mirrored HTML/XML from each source, used by Phase 2+ of `docs/integration-test-plan.md` to run the full pipeline against local files (no real-site traffic). Real captures are **gitignored** because they may contain third-party copyrighted content. Rebuild them with `cmd/dev/downloader`.

## Layout

```
testdata/
  synthetic/
    collector/parser/...
    discovery/scout/...
  real/
    <host>/<url-path>[?query-suffix]
```

`mirrorSaver` maps each fetched URL to `<host>/<path>` under the base directory, so a Phase 2 fixture server can serve it with a plain `http.FileServer`.

## Rebuild

One source at a time (default output is `testdata/real`):

```bash
go run ./cmd/dev/downloader -s dpp -n 3      # 3 directory pages of DPP
go run ./cmd/dev/downloader -s tpp -n 2
go run ./cmd/dev/downloader -s kmt -n 2
go run ./cmd/dev/downloader -s pts -n 1
go run ./cmd/dev/downloader -s cna -n 1
```

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `-s, --source` | `dpp` | Source name (must exist in `scouts.yaml`) |
| `-n, --max-pages` | `1` | Directory pages to crawl |
| `-o, --output` | `testdata/real` | Base directory for local-only mirrored files |
| `-c, --config` | `configs/worker/discovery/scouts.yaml` | Scout config |
| `--start-url` | (per-source) | Override start URL template |
| `--step` / `--first` | (per-source) | Pager step and first index |

## Rough size

A full rebuild of all 5 sources at the commands above is ~50 MB / ~360 files. For a lighter smoke-test fixture, use `-n 1` and a single source.

## Notes

- The downloader deliberately fails the minify stage and captures the raw body from `collector.StageError.Intermediate`. This is a dev-only trick; production code never takes that path.
- Files are saved without extensions when the URL path has none, so `http.FileServer` will serve them as `application/octet-stream`. Consumers that care about MIME should sniff the body.
