# Workforce Management

A Supporting bounded context that owns "who is on shift, on which process
path, at what rate; direct vs indirect hours." It covers the shift-start
planning horizon (a human commits a split of headcount across paths) and
intra-shift assignment tracking (moving people between paths as backlogs
deviate — the move itself stays a human call; this context makes the gap
legible, it does not decide).

## Why this context stops at the path boundary

Workforce Management never links an associate to a specific task — it only
tracks which **path** (pack, stow, pick, ...) an associate is currently
assigned to, at the granularity of a shift-length interval. Dispatching an
individual unit of work to a claiming station is a different problem with a
different cadence: workforce assignment changes over minutes to hours as
headcount is rebalanced across paths, while task dispatch changes over
seconds as work is claimed off a queue. Fusing the two into one aggregate
would force task-dispatch policy changes to touch workforce planning code
(and vice versa) even though nothing about the labor picture actually
changed. Keeping the seam here — LaborAssignment ends at "this associate is
on this path," full stop — lets Fulfillment Execution own task dispatch
independently, on its own release cadence, against this context's read model
(`GetStaffingGap`) rather than its write model.

## Architecture

Hexagonal / Ports & Adapters. Domain depends on nothing; application depends
on domain; adapters depend on application/domain.

```
cmd/workforce/                composition root
internal/
  domain/
    associate/                 AssociateShift aggregate
    shiftplan/                 ShiftPlan aggregate
    assignment/                LaborAssignment aggregate
    shared/                    value objects + domain events
  application/
    ports/                     AssociateRepo, ShiftPlanRepo, AssignmentRepo, EventPublisher, Clock
    usecases/                  one struct per use case
  adapters/
    inbound/http/               chi handlers, DTOs, error mapping
    outbound/postgres/          pgxpool repos + golang-migrate migrations
    outbound/memory/            in-memory repos for tests/local
    outbound/events/            log/buffered publisher (kafka-ready interface)
    outbound/clock/             system clock
migrations/                   golang-migrate SQL files
```

## Design decisions worth calling out

- **AssignLabor ends the prior assignment rather than rejecting the call.**
  The "exactly one ACTIVE assignment per associate" invariant is enforced by
  construction: `LaborAssignment` is one aggregate per associate holding a
  single optional active interval, so assigning a second path always closes
  the first (raising `LaborReassigned`) instead of needing a reject-on-conflict
  check. This matches how rebalancing actually happens on the floor — a
  supervisor moves someone, they don't first have to "unassign."
- **Path → required certification is a naming convention**, not a separate
  concept: a path's required certification is the `Certification` with the
  same name as its `PathId` (path `"pack"` requires certification `"pack"`).
  This context has no `AssignLabor`-adjacent port for path requirements in its
  spec, so introducing one would be scope creep; the convention is documented
  here instead.
- **`installedStations` is supplied by the caller** on `CommitShiftPlan`,
  not looked up from another service. Work Planning owns installed-station
  counts; this context enforces `plannedHeads <= installedStations`
  independently (per its own invariant) but has no dependency on that
  service, so the request carries the numbers it needs to validate against.
- **`GetStaffingGap` takes `buildingId`/`shiftId` as query parameters**
  (`GET /paths/{pathId}/staffing-gap?buildingId=&shiftId=`) because
  `ShiftPlan` is keyed by building + shift, and a path's planned heads only
  make sense within one committed plan.
- **`PathPlan.plannedHours <= plannedHeads * maxHoursPerShift`** is how
  "sum of hours valid" is enforced on `ShiftPlan`: a path line can't commit
  more total hours than its planned heads could work within one shift's max.

## Run it

```bash
docker compose up -d                 # Postgres 16 on localhost:5432
export DATABASE_URL="postgres://workforce:workforce@localhost:5432/workforce?sslmode=disable"
go run ./cmd/workforce                # applies migrations, then serves on :8080
```

Env vars:

| Var | Required | Default | Meaning |
|-----|----------|---------|---------|
| `DATABASE_URL` | yes | — | Postgres connection string |
| `HTTP_ADDR` | no | `:8080` | listen address |
| `MIGRATIONS_PATH` | no | `migrations` | path to golang-migrate SQL files |
| `MAX_HOURS_PER_SHIFT` | no | `8` | the configured max-hours-per-shift cap |
| `EVENT_PUBLISHER` | no | `log` | `log` or `kafka` — see [Integration](#integration) |
| `KAFKA_BROKERS` | no | `localhost:9092` | comma-separated broker list, used when `EVENT_PUBLISHER=kafka` |

## API

All bodies are JSON. `{id}` and `{pathId}` are path parameters.

```bash
# Start an associate's shift with initial certifications
curl -X POST localhost:8080/associates/assoc-1/start-shift \
  -d '{"certifications":["pack"]}'

# Add a certification
curl -X POST localhost:8080/associates/assoc-1/certifications \
  -d '{"certification":"hazmat"}'

# Propose heads for a path (pure computation, ceil(charge/plannedRate); not committed)
curl -X POST localhost:8080/paths/pack/plan/propose \
  -d '{"buildingId":"bldg-1","charge":100,"plannedRate":30}'

# Commit a shift plan (human-committed headcount split across paths)
curl -X POST localhost:8080/shift-plans \
  -d '{"buildingId":"bldg-1","shiftId":"shift-1","lines":[
        {"pathId":"pack","plannedHeads":3,"plannedRate":30,"plannedHours":24,"installedStations":10}
      ]}'

# Assign an associate to a path (ends any prior active assignment)
curl -X POST localhost:8080/associates/assoc-1/assignments \
  -d '{"pathId":"pack"}'

# Break start/end
curl -X POST localhost:8080/associates/assoc-1/break/start
curl -X POST localhost:8080/associates/assoc-1/break/end

# Staffing gap read model for a path within a committed plan
curl "localhost:8080/paths/pack/staffing-gap?buildingId=bldg-1&shiftId=shift-1"

# End an associate's shift (closes any active assignment first)
curl -X POST localhost:8080/associates/assoc-1/end-shift

curl localhost:8080/healthz
```

## Integration

This service can publish `ShiftPlanCommitted` to the shared warehouse-systems
Kafka broker so other bounded contexts (e.g. `wes-work-planning`, which
projects these into its own `LaborPlanObserved` read model, keyed by
`path_id`) can react to committed headcount without calling back into this
service. This round it only publishes — it does not consume anything.

- **Topic**: `warehouse.workforce.events`
- **Broker**: `KAFKA_BROKERS` (default `localhost:9092`) — points at the
  shared broker started separately via
  `~/warehouse-systems/docker-compose.kafka.yml`; this repo's own
  `docker-compose.yml` only runs Postgres.
- **Selection**: `EVENT_PUBLISHER=kafka` to publish to Kafka, `EVENT_PUBLISHER=log`
  (default) to keep publishing to the in-memory/log publisher used by tests
  and local runs that don't need cross-service integration.
- **Fan-out**: a `ShiftPlan` has multiple `PathPlan` lines. `CommitShiftPlan`
  with 3 path lines publishes **3** Kafka messages — one per path line, each
  carrying that single path's `planned_heads`/`planned_rate`/`planned_hours`.
  This matches how `wes-work-planning` keys its read model, by `path_id`.
- **Envelope** (identical shape across all warehouse-systems services):

```json
{
  "event_id": "uuid-v4",
  "event_type": "ShiftPlanCommitted",
  "occurred_at": "2026-08-21T22:00:00Z",
  "source": "workforce-management",
  "data": {
    "building_id": "...",
    "shift_id": "...",
    "path_id": "...",
    "planned_heads": 3,
    "planned_rate": 30,
    "planned_hours": 24
  }
}
```

Smoke test against the shared broker:

```bash
# shared broker already running: ~/warehouse-systems/docker-compose.kafka.yml
export EVENT_PUBLISHER=kafka
export KAFKA_BROKERS=localhost:9092
go run ./cmd/workforce

curl -X POST localhost:8080/shift-plans \
  -d '{"buildingId":"bldg-1","shiftId":"shift-1","lines":[
        {"pathId":"pack","plannedHeads":3,"plannedRate":30,"plannedHours":24,"installedStations":10},
        {"pathId":"pick","plannedHeads":2,"plannedRate":25,"plannedHours":16,"installedStations":10}
      ]}'

# in another terminal, confirm 2 messages landed on the topic:
docker exec warehouse-kafka /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 --topic warehouse.workforce.events --from-beginning --max-messages 2
```

## Test

```bash
go build ./...
go vet ./...
go test ./...
go test ./... -race
gofmt -l .                      # should print nothing

# Postgres integration tests (build-tagged, skipped without DATABASE_URL)
docker compose up -d
export DATABASE_URL="postgres://workforce:workforce@localhost:5432/workforce?sslmode=disable"
go test -tags=integration ./internal/adapters/outbound/postgres/...
```

The four invariants named in this context's Definition of Done each have a
dedicated failing-path test, at both the domain and use-case/HTTP layers:

- `plannedHeads > installedStations` rejected on `ShiftPlan` commit —
  `shiftplan.TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations`,
  `usecases.TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations`,
  `http.TestCommitShiftPlan_RejectsPlannedHeadsExceedingInstalledStations`.
- Double-booking rejected — `assignment.TestAssign_SecondAssignmentEndsPriorAndRaisesReassigned`
  proves the invariant holds by construction (a second active assignment can
  never coexist with the first).
- Assignment without required certification rejected —
  `assignment.TestAssign_RejectsMissingCertification`,
  `usecases.TestAssignLabor_RejectsMissingCertification`,
  `http.TestAssignLabor_RejectsMissingCertification`.
- Assignment while on an active break rejected —
  `associate.TestCanBeAssigned_RejectsWhileOnBreak`,
  `usecases.TestAssignLabor_RejectsWhileOnBreak`,
  `http.TestAssignLabor_RejectsWhileOnBreak`.
