# Integrations — outbound clients, process-path catalogue, events, CORS

## Outbound HTTP clients (`internal/application/ports/ports.go`)

- **`MeasuredRateClient`** (ADR-0012) — queries `labor-performance` for a
  real, measured mean task duration per path, so `ProposePathPlan` can
  propose headcount against reality instead of always requiring a
  caller-supplied `plannedRate`. **Fail-open**: any failure (unreachable
  service, malformed response, genuinely no data yet) collapses to the
  single sentinel `ErrMeasuredRateUnavailable`, and the use case falls back
  to the caller-supplied rate. Selected via `LABOR_PERFORMANCE_MODE`
  (`http`|`permissive`, default `permissive` — never reaches the network)
  and `LABOR_PERFORMANCE_BASE_URL`.
- **`InstalledCapacityClient`** (ADR-0014) — queries `fulfillment-execution`
  for the real, live count of registered stations holding a path's
  capability, so `CommitShiftPlan` enforces `plannedHeads` against physical
  reality, not just a caller-supplied `installedStations` count. **Fail-LOUD**
  (unlike the measured-rate client): any failure collapses to
  `ErrInstalledCapacityUnavailable` and the ENTIRE commit is rejected (503,
  `installed-capacity-unavailable`) with no fallback, since a commit mutates
  real state. A real `0` capacity is a valid answer, not an error — any
  `plannedHeads > 0` for that path is then rejected (409,
  `exceeds-installed-capacity`). Selected via `INSTALLED_CAPACITY_MODE`
  (`http`|`permissive`, default `permissive` — but permissive here means
  fail-loud-always, the opposite sense of the measured-rate default) and
  `FULFILLMENT_EXECUTION_BASE_URL`.

Both interfaces deliberately expose only one error sentinel each: the use
case has exactly one handling branch per client and never needs to
distinguish failure subtypes.

## Process-path catalogue (ADR-0013)

`internal/domain/pathcatalog` models the fleet's declared process-path
catalogue: which path families are declared and what capabilities each
requires. Lookup is a **case-insensitive prefix match**, not exact — real
`path_id` values carry a station/zone/scenario suffix (e.g. `pick-zone-a`
matches the declared prefix `pick`). This package mirrors
`fulfillment-execution`'s and `wes-work-planning`'s own pathcatalog packages
byte-for-byte in matching semantics, since all three read the same
published-language file (`warehouse-infra/config/process-paths/*.yaml`).

- `outbound/filecatalog` — loads the YAML from `PATH_CATALOGUE_FILE`
  (default `/etc/workforce-management/process-paths.yaml`) once at startup.
  A missing/invalid file is a **fatal boot-time error**.
- `outbound/kafkacatalog` — an alternative, Kafka-sourced catalogue adapter
  implementing the same `ports.PathCatalogue` interface.

## Kafka integration events + transactional outbox (ADR-0004, ADR-0016)

This service publishes `ShiftPlanCommitted` to the shared warehouse-systems
Kafka broker; `wes-work-planning` projects it into its own
`LaborPlanObserved` read model, keyed by `path_id`. This round it only
publishes — it does not consume anything on the integration side.

- **Topic**: `warehouse.workforce.events`. **Broker**: `KAFKA_BROKERS`
  (default `localhost:9092`, shared cluster started via
  `~/warehouse-systems/docker-compose.kafka.yml` — this repo's own
  `docker-compose.yml` only runs Postgres).
- **Selection**: `EVENT_PUBLISHER=kafka` to publish; default `log` keeps
  tests/local runs broker-free.
- **Delivery** (ADR-0016, transactional outbox): with `EVENT_PUBLISHER=kafka`
  the use case inserts the already-encoded message(s) for BOTH the
  integration topic and the analytics topic into the `outbox_events` table
  inside the SAME Postgres transaction as the aggregate. A relay goroutine in
  `cmd/workforce` drains that table onto Kafka every `OUTBOX_RELAY_INTERVAL`
  (default `1s`; a full batch is followed immediately by another pass). The
  store and the topics therefore cannot diverge — delivery is at-least-once,
  per-key ordered.
- **Fan-out**: a `ShiftPlan` has multiple `PathPlan` lines — `CommitShiftPlan`
  with 3 lines publishes 3 Kafka messages, one per line, each carrying that
  line's `planned_heads`/`planned_rate`/`planned_hours` plus the plan's
  `building_id`/`shift_id`. Consumers must expect N messages per commit.
- **Envelope** (flat cross-service shape, shared fleet-wide, see this repo's
  `INTEGRATION.md`):

```json
{
  "event_id": "uuid-v4",
  "event_type": "ShiftPlanCommitted",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "workforce-management",
  "data": {
    "building_id": "bldg-1",
    "shift_id": "shift-1",
    "path_id": "pack",
    "planned_heads": 3,
    "planned_rate": 30,
    "planned_hours": 24
  }
}
```

`event_id` is a UUID v4 generated at publish time; `source` is always this
service's own name; `occurred_at` is RFC 3339 UTC.

The **full CloudEvents domain-event catalog** (all ten events, reverse-DNS
`type` naming `com.warehouse.wes.workforce-management.<entity>.<EventName>`)
lives in `apis/asyncapi.yaml` — deliberately broader than what's published
today; every event besides `ShiftPlanCommitted` is in-process only. Keep
`apis/asyncapi.yaml` and `docs/docs/ecosystem/integration.md` (the narrative
counterpart) in sync when the publication set changes.

## CORS

`go-chi/cors` middleware is enabled on every route, allowing
`CORS_ALLOWED_ORIGINS` (env, default
`http://localhost:5173,http://localhost:5185` — the `warehouse-console`
shell and this service's own `workforce-mfe` remote). This service is not
part of the fleet's cross-service Order Lifecycle read model — CORS exists
solely for `workforce-mfe` (see rules/frontend.md).
