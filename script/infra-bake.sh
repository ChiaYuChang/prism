#!/usr/bin/env bash
#
# Render local runtime configuration under build/infra/ from committed defaults
# and local .secrets/*. Output is gitignored and safe to regenerate.

set -euo pipefail
umask 077

ENV="${ENV:-test}"
SECRETS_DIR="${SECRETS_DIR:-.secrets}"
OUT_DIR="build/infra"

case "$ENV" in
    test|e2e) ;;
    prod)
        echo "ENV=prod refused: prod runtime config must be rendered by the deployment platform." >&2
        exit 1
        ;;
    *)
        echo "ENV must be one of test|e2e|prod, got: $ENV" >&2
        exit 1
        ;;
esac

valkey_password_file="$SECRETS_DIR/valkey_prism"
if [ ! -f "$valkey_password_file" ]; then
    echo "missing $valkey_password_file" >&2
    exit 1
fi

valkey_password="$(tr -d '\r\n' < "$valkey_password_file")"
if [ -z "$valkey_password" ]; then
    echo "$valkey_password_file is empty" >&2
    exit 1
fi

valkey_hash="$(printf '%s' "$valkey_password" | sha256sum | cut -d ' ' -f 1)"

mkdir -p "$OUT_DIR/nats" "$OUT_DIR/valkey" "$OUT_DIR/otel" \
	"$OUT_DIR/grafana" \
	"$OUT_DIR/grafana/provisioning/alerting" \
	"$OUT_DIR/grafana/provisioning/dashboards" \
	"$OUT_DIR/grafana/provisioning/plugins"

cat > "$OUT_DIR/nats/nats.conf" <<'EOF'
# NATS Server Configuration

# General
port: $NATS_CLIENT_PORT
http_port: $NATS_HTTP_MONITORING_PORT
server_name: $NATS_SERVER_NAME

# JetStream
jetstream {
    store_dir: $NATS_JS_STORE_DIR
}

# Authorization
authorization {
    token: $NATS_AUTH_TOKEN
}
EOF

cat > "$OUT_DIR/valkey/valkey.conf" <<'EOF'
bind 0.0.0.0
port 6379

# snapshots
save 3600 1 300 100 60 10000
dbfilename dump.rdb

# memory
maxmemory 256mb
maxmemory-policy allkeys-lru

# ACL
aclfile /run/secrets/valkey_acl
EOF

cat > "$OUT_DIR/valkey/valkey_acl" <<EOF
user default on nopass ~* &* -@all +ping
user prism on #$valkey_hash ~prism:* ~api:* ~fetch:* &* +@read +@write +@scripting +ping -@dangerous -@admin
EOF

cat > "$OUT_DIR/otel/otel.yaml" <<'EOF'
receivers:
  filelog/app:
    include:
      - /logs/*/*.log*
    start_at: beginning
    include_file_path: true
    storage: file_storage
    operators:
      - type: json_parser
        parse_from: body
        parse_to: attributes
      - type: regex_parser
        parse_from: attributes["log.file.path"]
        regex: '^/logs/(?P<service_name>[^/]+)/'
        parse_to: attributes

  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  batch:
  transform/logs:
    log_statements:
      - context: log
        statements:
          - set(resource.attributes["service.name"], log.attributes["service_name"])
          - delete_key(log.attributes, "service_name")

extensions:
  health_check:
    endpoint: 0.0.0.0:13133
  file_storage:
    directory: /var/lib/otelcol/file_storage
    create_directory: true

exporters:
  otlphttp/victorialogs:
    logs_endpoint: http://victoria-logs:9428/insert/opentelemetry/v1/logs
    compression: gzip
    encoding: proto

  file/local_backup:
    path: /var/log/app/otel_logs.json
    rotation:
      max_megabytes: 100
      max_backups: 5

  otlphttp/victoriametrics:
    metrics_endpoint: http://victoria-metrics:8428/opentelemetry/v1/metrics
    compression: gzip
    encoding: proto

  otlphttp/victoriatraces:
    traces_endpoint: http://victoria-traces:10428/insert/opentelemetry/v1/traces
    compression: gzip
    encoding: proto

service:
  extensions: [health_check, file_storage]

  pipelines:
    logs:
      receivers: [filelog/app]
      processors: [transform/logs, batch]
      exporters: [file/local_backup, otlphttp/victorialogs]

    metrics:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/victoriametrics]

    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/victoriatraces]
EOF

cat > "$OUT_DIR/grafana/custom.ini" <<'EOF'
[security]
admin_user = grafana
admin_password = $__file{/run/secrets/grafana}
EOF

chmod 0644 "$OUT_DIR/nats/nats.conf" "$OUT_DIR/valkey/valkey.conf" \
	"$OUT_DIR/valkey/valkey_acl" "$OUT_DIR/otel/otel.yaml" \
	"$OUT_DIR/grafana/custom.ini"

echo "wrote $OUT_DIR/{nats,valkey,otel,grafana} runtime config"
