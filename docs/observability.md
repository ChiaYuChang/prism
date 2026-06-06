# Observability

## Logs

Prism writes human-readable text logs to stdout and structured JSON logs to a
shared log-file volume with `slog.NewMultiHandler`. Containers do not push logs
directly from application code. The local `obs` compose profile ships JSON log
files with this path:

```text
app slog text stdout -> docker compose logs for humans
app slog JSON file -> OTel Collector filelog receiver -> VictoriaLogs -> Grafana
```

The collector tails `/logs/*.json`, parses each Prism `slog` JSON line into log
attributes, and sends the result to VictoriaLogs with OTLP HTTP.

Start the default local stack with observability enabled:

```bash
task compose:up
```

Grafana is available at `http://127.0.0.1:${GRAFANA_PORT:-3100}` and is
provisioned with a `VictoriaLogs` datasource. VictoriaLogs is available at
`http://127.0.0.1:${VICTORIA_LOGS_PORT:-9428}`.

Operational notes:

- Stdout logs are optimized for human `docker compose logs` readability.
- JSON file logs are optimized for backend ingestion and querying.
- Stdout and JSON file handlers can use different thresholds with
  `--log-console-level` and `--log-file-level`; empty handler levels fall back
  to global `--log-level`.
- App logs should stay structured: use `slog.String`, `slog.Int`, `slog.Any`, etc.
- Do not log secrets, OTLP headers, raw payloads, or high-cardinality query strings.
- OTLP log SDK export from app code is optional and not the preferred path for Prism containers.
