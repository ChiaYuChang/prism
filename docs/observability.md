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

## Traces And Metrics

Prism commands export traces and metrics to the OTel Collector with OTLP/gRPC
when `--otel-enabled` is set. The local `obs` compose profile ships them with
this path:

```text
app OTLP/gRPC traces -> OTel Collector -> VictoriaTraces -> Grafana Jaeger datasource
app OTLP/gRPC metrics -> OTel Collector -> VictoriaMetrics -> Grafana Prometheus datasource
```

The scheduler worker containers are the first proof-point for this path. They
send OTLP to `otel-collector:4317` and use distinct service names:
`prism.scheduler.slow` and `prism.scheduler.fast`.

Set `PRISM_WORKER_OTEL_ENABLED=false` for worker-only runs without the `obs`
profile.

Start the default local stack with observability enabled:

```bash
task compose:up
```

Grafana is available at `http://127.0.0.1:${GRAFANA_PORT:-3100}` and is
provisioned with `VictoriaLogs`, `VictoriaMetrics`, and `VictoriaTraces`
datasources. Local backend ports:

- VictoriaMetrics: `http://127.0.0.1:${VICTORIA_METRICS_PORT:-8428}`
- VictoriaLogs: `http://127.0.0.1:${VICTORIA_LOGS_PORT:-9428}`
- VictoriaTraces: `http://127.0.0.1:${VICTORIA_TRACES_PORT:-10428}`
- OTel Collector gRPC: `127.0.0.1:${OTEL_COLLECTOR_GRPC_PORT:-4317}`
- OTel Collector HTTP: `127.0.0.1:${OTEL_COLLECTOR_HTTP_PORT:-4318}`

Operational notes:

- Stdout logs are optimized for human `docker compose logs` readability.
- JSON file logs are optimized for backend ingestion and querying.
- Stdout and JSON file handlers can use different thresholds with
  `--log-console-level` and `--log-file-level`; empty handler levels fall back
  to global `--log-level`.
- App logs should stay structured: use `slog.String`, `slog.Int`, `slog.Any`, etc.
- Do not log secrets, OTLP headers, raw payloads, or high-cardinality query strings.
- OTLP log SDK export from app code is optional and not the preferred path for Prism containers.
- Local compose uses insecure OTLP transport inside `prism-net`; production must
  use TLS or an equivalent encrypted network path before sending OTLP headers.
