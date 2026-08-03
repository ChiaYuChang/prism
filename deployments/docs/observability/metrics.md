# Metrics

Prism exposes Prometheus-compatible metrics on each service's internal health
server at `/metrics`. The local observability stack runs `vmagent`, which scrapes
these endpoints and remote-writes to VictoriaMetrics.

Database metrics currently come from `pgxprom`:

- `pgx_conn_requests_total` — total database requests by `database` and `db_operation`
- `pgx_conn_request_errors_total` — failed database requests by `database` and `db_operation`
- `pgx_conn_request_duration_seconds` — database request duration histogram by `database` and `db_operation`
- `pgx_pool_acquire_connections` — connections currently being acquired by `database`
- `pgx_pool_canceled_acquires_total` — canceled pool acquires by `database`
- `pgx_pool_constructing_connections` — connections currently being constructed by `database`
- `pgx_pool_empty_acquires_total` — acquires that waited on an empty pool by `database`
- `pgx_pool_idle_connections` — idle pool connections by `database`
- `pgx_pool_max_connections` — configured max pool connections by `database`
- `pgx_pool_total_connections` — total pool connections by `database`
- `pgx_pool_new_connections_total` — newly created pool connections by `database`
- `pgx_pool_max_lifetime_destroys_total` — connections destroyed by max lifetime by `database`
- `pgx_pool_max_idle_destroys_total` — connections destroyed by max idle time by `database`

`db_operation` is derived from SQLC query comments such as `-- name: CreateTask`,
so SQL text and SQL arguments are not emitted to metrics.
