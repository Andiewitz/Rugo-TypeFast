# Observability

- Location: `services/observability`
- Runtime: Docker Compose, profile `observability`
- Stack: Prometheus 3.15, Grafana 13, Loki 3.7, Promtail 3.6

## What it is for

Metrics, logs, and dashboards for the services. Users never see this stack; it is where you go when something looks wrong.

> Promtail reached end of life on March 2, 2026. This stack pins the last release, 3.6.11. When it needs replacing, move log collection to Grafana Alloy.

## Start it

```bash
docker compose --profile observability up -d
```

| Component | Endpoint |
| --- | --- |
| Grafana | http://localhost:3000 |
| Prometheus | http://localhost:9090 |
| Loki | http://localhost:3100 |
| Dashboard folder | `Rugo` |
| Metrics | user service `http://localhost:9091/metrics` |

Every port binds to `127.0.0.1`, so none of this is reachable from another machine.

## How the three pieces fit

1. The user service counts things and exposes them at `/metrics` on its own listener.
2. Prometheus scrapes that endpoint every 15 seconds and stores the series.
3. Grafana reads Prometheus for graphs and Loki for log lines.

## Logs and errors

The service tells users little and tells operators everything.

- HTTP problem responses never include stack traces.
- Panics and unexpected failures are logged with a `stack_trace` field.
- Every log line carries the request ID, so one failed request can be traced end to end.
- Prometheus shows how often errors happen; Loki holds the detail.

## Layout

```text
services/observability/
  prometheus/     scrape configuration
  loki/           log storage configuration
  promtail/       log collection from container logs
  grafana/        provisioning and dashboards
```
