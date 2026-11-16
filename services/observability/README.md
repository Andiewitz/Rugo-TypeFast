# Observability

Grafana, Prometheus, Loki, and Promtail for the Rugo services.

## Start

```bash
docker compose --profile observability up -d
```

## Where things are

| What | Where |
| --- | --- |
| Grafana | http://localhost:3000 |
| Prometheus | http://localhost:9090 |
| Loki | http://localhost:3100 |
| Dashboards | Grafana folder `Rugo` |
| Logs | Loki query `{service="user"}` |

## The metrics endpoint

The user service serves Prometheus metrics on its own listener, not on the API port:

```bash
curl -s http://localhost:9091/metrics | head
```

`METRICS_ADDR` controls it (default `127.0.0.1:9091`, `off` disables it). Prometheus scrapes `user:9091` from inside the Compose network. `http://localhost:3001/metrics` returns 404 on purpose — that port faces users.

All observability ports bind to `127.0.0.1`, so nothing here is reachable from another machine.

How the service produces these numbers is in [`docs/systems/observability.md`](../../docs/systems/observability.md).
