# Logging

The Workforce Management service uses Go's standard library `log/slog`
package for structured logging.

## Why `log/slog`

- **Zero new dependency.** `log/slog` shipped in Go 1.21 and this module
  targets a much newer Go toolchain, so structured logging is available for
  free — no `zerolog`/`zap` import to vet, pin, or patch.
- **Go ecosystem standard.** `slog` is the community-converged structured
  logging API; libraries and tooling increasingly accept or emit
  `*slog.Logger` / `slog.Handler` directly.
- **OTel-bridge ready.** OpenTelemetry's Go logs bridge (and most
  observability vendors) can consume an `slog.Handler`, so this service can
  gain trace-correlated log export later without changing call sites.
- `zerolog` and `zap` were considered and rejected: this is a
  moderate-throughput CRUD/event-driven service, not a hot-path log
  producer, so their extra allocation-avoidance machinery is unneeded
  dependency weight here.

## Configuration

| Env var     | Default | Values                          |
|-------------|---------|----------------------------------|
| `LOG_LEVEL` | `info`  | `debug`, `info`, `warn`, `error` (case-insensitive) |

Set `LOG_LEVEL=debug` for verbose local development; leave unset (or `info`)
in production.

## Output format

Logs are emitted as single-line JSON to stdout via
`slog.NewJSONHandler(os.Stdout, ...)`, e.g.:

```json
{"time":"2026-08-23T12:00:00Z","level":"INFO","msg":"http server listening","addr":":8080"}
{"time":"2026-08-23T12:00:01Z","level":"INFO","msg":"http request","method":"GET","path":"/healthz","status":200,"duration_ms":0,"bytes":16,"request_id":"abc123"}
{"time":"2026-08-23T12:00:02Z","level":"INFO","msg":"domain event published","event_name":"AssociateShiftStarted","occurred_at":"2026-08-23T12:00:02Z"}
```

JSON-to-stdout is the standard shape for container/Kubernetes log
collection (Fluent Bit, Vector, CloudWatch, etc.) without any extra
formatting step.

## What gets logged

- **HTTP requests** — a chi middleware (`internal/adapters/inbound/http/logging.go`)
  logs method, path, status, `duration_ms`, response `bytes`, and the chi
  `request_id` for every request. Responses with a `5xx` status are logged
  at `Error`; everything else at `Info`.
- **Domain events** — `events.LogPublisher` (used when
  `EVENT_PUBLISHER=log`, the default) logs each published domain event's
  name and occurrence time at `Info`. A `nil` `*slog.Logger` disables event
  logging while still buffering events in memory (used by tests).
- **Startup/shutdown and config errors** — missing required env vars,
  invalid env var values, and the Kafka publisher's configuration/close
  errors are logged via `slog` before the process exits.
