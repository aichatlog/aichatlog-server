# Claude Code usage telemetry → Prometheus → Grafana (optional)

A real-time, push-based view of Claude Code token usage and cost, built on
Claude Code's **native OpenTelemetry metrics**. This is the optional companion
to aichatlog-server's historical usage analytics (`GET /api/stats/usage` +
the dashboard **Usage** tab):

| | aichatlog-server usage analytics | this OTEL stack |
|---|---|---|
| Source | JSONL transcripts synced by the plugin | Claude Code OTEL metrics (live) |
| Latency | per-conversation (on Stop hook) | ~seconds (export interval) |
| Strength | history, search, per-project/device, exact per-model cache split | real-time dashboards + alerting |
| Cost | run nothing extra (already syncing) | run this collector+Prometheus+Grafana |

Use the OTEL stack when you want live dashboards or Prometheus alerting; use the
server analytics for the durable, queryable record. They are independent.

## Run it

```bash
cd deploy/otel
docker compose up -d
# Grafana:    http://localhost:3000   (anonymous viewer; admin/admin to edit)
# Prometheus: http://localhost:9090
```

Open Grafana → dashboard **“Claude Code — Usage & Cost”** (auto-provisioned).

## Point Claude Code at it

On each machine that runs Claude Code, export the vars in
[`env.claude-code.example`](env.claude-code.example), then start `claude`:

```bash
export CLAUDE_CODE_ENABLE_TELEMETRY=1
export OTEL_METRICS_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
export OTEL_EXPORTER_OTLP_ENDPOINT=http://<collector-host>:4317
export OTEL_METRIC_EXPORT_INTERVAL=10000
export OTEL_RESOURCE_ATTRIBUTES=host.name=my-server-1   # so hosts are distinguishable
```

Metrics appear within one or two export intervals. Verify the pipeline:

```bash
curl -s localhost:8889/metrics | grep claude_code       # raw collector output
curl -s 'localhost:9090/api/v1/query?query=claude_code_token_usage' | jq .
```

## What you get

- **Metrics**: `claude_code_token_usage` (labels `type`=input|output|cacheRead|cacheCreation,
  `model`, `terminal_type`, …) and `claude_code_cost_usage` (USD), plus session/
  LOC/commit/PR counters Claude Code emits.
- **Dashboard panels**: total tokens & cost over range, cache-read share, token
  rate by type and by model, cost/hour by model.

### Metric naming

The collector's Prometheus exporter runs with `add_metric_suffixes: false`, so
names are the clean dotless form (`claude_code_token_usage`, not
`claude_code_token_usage_total`). Flip that in
[`otel-collector-config.yaml`](otel-collector-config.yaml) if you prefer the
`_total` convention — update the dashboard queries to match.

### Cardinality

Claude Code tags every datapoint with `session.id`, `user.id`, and `user.email`
(unbounded). The collector **drops** these (see the `transform` processor) to
protect Prometheus. Keep them only if you have a cardinality-tolerant backend.

## Caveats

- **Cost is a list-price estimate**, not a bill. `claude_code.cost.usage` is
  computed by Claude Code from public rates; for Pro/Max/Team subscriptions your
  real spend is the subscription fee (usage included). Authoritative billing:
  the Anthropic Console / Usage & Cost API.
- Counters are cumulative; the dashboard uses `rate()`/`increase()`, so a panel
  needs ≥2 scrapes before it shows data.
- This stack has no auth beyond Grafana's — don't expose the ports publicly.

## Versions

Pinned images: otel/opentelemetry-collector-contrib `0.117.0`, prom/prometheus
`v3.1.0`, grafana/grafana `11.4.0`.
