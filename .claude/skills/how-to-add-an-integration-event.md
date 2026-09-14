# How to add an integration event (publish and consume)

Use when asked to publish a new cross-context integration event, or
consume one from a sibling bounded context. This fleet's Kafka is ONE
broker platform-wide — every design decision below exists because that
shared-broker reality has already caused a real incident once
(wes-work-planning#67).

## Publishing a new integration event

### 1. Is it actually cross-service?

Not every domain event this service raises belongs on the wire. This
service publishes to TWO topics, not one — check
`internal/adapters/outbound/kafka/publisher.go`'s doc comment
(`warehouse.workforce.events`, integration — forwards only
`ShiftPlanCommitted` today) versus
`internal/adapters/outbound/kafka/analytics_publisher.go`
(`warehouse.workforce.analytics`, ADR-0010's separate analytics data
product). Before adding a new event to either publisher, confirm a
sibling context genuinely needs to react to it on the integration topic,
or that it belongs in the Labor Utilization & Staffing report on the
analytics topic — they are not the same audience.

### 2. Envelope: this repo's own cross-service shape

Every message on `warehouse.workforce.events` uses the shape documented
in `INTEGRATION.md` and implemented in `publisher.go`'s `envelope`
struct:

```json
{
  "event_id": "<uuid>",
  "event_type": "ShiftPlanCommitted",
  "occurred_at": "<RFC3339>",
  "source": "workforce-management",
  "data": { "building_id": "...", "shift_id": "...", "path_id": "...",
            "planned_heads": 0, "planned_rate": 0, "planned_hours": 0 }
}
```

`event_type` here is the bare past-tense event name (`ShiftPlanCommitted`)
— the wire code's `envelope` struct has no `type`/`specversion` fields at
all. This is narrower than what `apis/asyncapi.yaml` documents: the spec
frames the channel as full CloudEvents 1.0 with a reverse-DNS `type`
context attribute (`com.warehouse.wes.workforce-management.shiftplan.ShiftPlanCommitted`),
and says so explicitly in its own intro ("This document is the complete
reference catalog ... deliberately broader than what leaves the process
today"). Don't take the AsyncAPI spec's CloudEvents framing as proof of
what's literally on the wire today — read `publisher.go`'s `envelope`
struct for the ACTUAL current field names before writing a consumer
against this topic; the spec is the target/contract, not always a
byte-for-byte mirror of the current adapter.

### 3. Implementation: encode inside the outbox, not a bare Publish

**This is the part that differs from a simple direct-publish service.**
Since ADR-0016 (transactional outbox), every publishing use case commits
its event's wire form to a Postgres `outbox_events` table in the SAME
transaction as the aggregate, not by calling a Kafka writer directly. A
new integration event follows the existing `Encoder` shape rather than
adding logic to `Publish`:

- Add the event struct to `internal/domain/<aggregate>/` — it should
  already exist as a domain event the aggregate raises (`shared.NewShiftPlanCommitted`,
  etc.); publishing wires an EXISTING domain event onto Kafka, it doesn't
  invent a new payload shape at the adapter layer.
- Add the event's case to `Publisher.Encode` in
  `internal/adapters/outbound/kafka/publisher.go` — this is where the
  envelope is built and marshaled, `Publisher` implements both
  `ports.EventPublisher` (direct call) and `kafka.Encoder` (outbox call);
  both `Publish` and `postgres.OutboxPublisher` route through the same
  `Encode`, so they can never disagree on wire format.
- If `Encode` needs to read a repository to fan one domain event out into
  several messages (as `ShiftPlanCommitted` does — one message per
  `PathPlan` line, loaded via `p.shiftPlans.FindByBuildingAndShift`),
  that read runs INSIDE the use case's transaction because
  `postgres.OutboxPublisher` calls `Encode` before commit — it must see
  the row the use case just saved, not a stale one.
- Give the message a partition key that keeps ordering where it matters,
  if this event needs one (the current `ShiftPlanCommitted` messages
  carry no key, matching the pre-existing integration contract —
  `INTEGRATION.md` is the source of truth, don't add a key unilaterally).
- Every publishing use case wraps its `Save`s and `Publish` in one
  `atomically(ctx, uc.UnitOfWork, func(ctx) error {...})` scope
  (`internal/application/usecases/unit_of_work.go`) — see
  `ProposePathPlan.Execute` and `GetStaffingGap.Execute` for the pattern
  even on a use case that saves nothing, publish-only use cases still
  wrap the `Publish` call.

### 4. Contract + docs

- Add the message to `apis/asyncapi.yaml` under this service's channel,
  matching the entity-grouping convention already there.
- This repo's analytics topic has no generated HTML reference; its
  narrative counterpart is `docs/docs/ecosystem/integration.md` — update
  both together if the analytics envelope changes shape.
- `docs-api-drift` CI only checks the REST OpenAPI-generated tree
  (`docs/api-reference/rest`); it does NOT catch AsyncAPI drift in this
  repo today — don't rely on CI to catch a stale `asyncapi.yaml`.

### 5. Test

Unit test the marshal shape against a fake `Writer` (see
`internal/adapters/outbound/kafka/encode_test.go` for both `Publisher.Encode`
and `AnalyticsPublisher.Encode` — never a real broker in a unit test).
If a new outbox path needs coverage that the atomic-transaction guarantee
actually holds, add it to
`internal/adapters/outbound/postgres/outbox_integration_test.go`, which
already asserts commit-together (aggregate + integration + analytics
rows) and rollback-on-encoder-failure via **testcontainers Postgres**
(`postgres:16-alpine`), never an external `DATABASE_URL`.

## Consuming an integration event from a sibling context

### 1. Never import the sibling's Go packages

This service knows a sibling's topic name and payload shape ONLY — never
its Go types. Both real consumers in this repo document this explicitly:
`internal/adapters/outbound/kafkacatalog/consumer.go`'s package doc
comment ("This service has no business knowing anything else about that
service beyond this topic name and the envelope/payload shape below") and
`internal/adapters/outbound/laborperformancecache/consumer.go`'s
identical framing for `labor-performance`. Hand-mirror the payload struct
locally (`pathData`, `taskPerformanceData`); do not add a Go module
dependency on the sibling repo — `internal/architecture/`'s fitness tests
enforce this.

### 2. Choose the right consumer-group pattern — this is the part that bites

Two DIFFERENT correct patterns exist. Picking the wrong one for your use
case is THE most common integration-event mistake in this fleet, and it
was learned from a real incident (wes-work-planning#67).

**Pattern A — long-lived, single-instance consumer group (a named
constant).** Use when exactly ONE instance of this consumer ever runs at
a time. This repo doesn't have a live example of pattern A on the
consuming side today (its outbox relay is the closer analog: a
single-process draining loop with no consumer-group semantics at all).

**Pattern B — per-process-unique consumer group (a generated id).** Use
when this consumer rebuilds a complete read model from a topic's FULL
history on every start (an event-sourced local cache, not a work queue).
**This repo has TWO real examples of Pattern B, and they are
byte-for-byte identical in design:**

- `internal/adapters/outbound/kafkacatalog/consumer.go` — replays
  `warehouse.process-path-management.events` to build the local
  process-path catalogue (replaced the old boot-time YAML file read).
- `internal/adapters/outbound/laborperformancecache/consumer.go` — replays
  `warehouse.labor-performance.events` to build a running-mean measured
  rate + idle-share cache (replaced the old synchronous HTTP call to
  labor-performance).

Both define an unexported `consumerGroupPrefix` constant
(`"workforce-management-process-path-catalogue"` /
`"workforce-management-labor-performance-cache"`) and both build the
actual `GroupID` via an identical `uniqueConsumerGroup()` helper:

```go
// uniqueConsumerGroup builds a group id unique to this process instance
// (hostname + PID + a nanosecond timestamp) — see the package doc
// comment for why per-process uniqueness is a correctness requirement,
// not a cosmetic choice.
func uniqueConsumerGroup() string {
    host, err := os.Hostname()
    if err != nil || host == "" {
        host = "unknown-host"
    }
    return fmt.Sprintf("%s-%s-%d-%d", consumerGroupPrefix, host, os.Getpid(), time.Now().UnixNano())
}
```

The group id MUST be unique per process instance, NEVER a fixed shared
string. Consumer group offsets are shared infrastructure state: a
brand-new process joining a group an EARLIER instance already consumed
resumes from that instance's committed offset, so the new process gets
marked "ready" with an empty local cache having replayed nothing — a
silent correctness bug, not a crash. Copy one of these two packages'
structure (not just the group-id helper) for a third such consumer —
`NewConsumer`/`NewConsumerForTopic`, `newTargetOffsets`, `Ready`/`WaitReady`,
and `checkReady` are all part of the same correctness story, described in
full below.

**Never do this** (the actual incident): a fixed shared consumer group id
on a consumer meant to run as exactly one instance per environment. Fix:
make the group id env-configurable or process-unique, never hardcode it
as a literal string.

### 3. Readiness gate — the specific bug this repo's two consumers both avoid

If the consumer replays a topic's full history to build a cache other
code depends on, expose a `Ready()`/`WaitReady(ctx)` gate the composition
root consults before starting real work (see `cmd/workforce/main.go`'s
`WaitReadyTimeout` usage for `kafkacatalog.Consumer`). Both real
consumers here compute a `target` (per-partition "last offset at startup"
watermark, via `newTargetOffsets`) ONCE at construction, and `checkReady`
marks ready when every tracked partition has been drained past its
target — NOT by waiting for a brand-new message to arrive. A readiness
check that only re-evaluates on a NEW message deadlocks forever on an
ordinary restart where a shared/already-caught-up group never gets a new
message to trigger it; capturing the target watermark up front sidesteps
that whole class of bug, and is simpler than an `OffsetFetch`-based
alternative.

## Verify before opening the PR

```bash
make check-all    # includes arch-test — will catch a sibling-package import
```

For a Kafka-touching change, also confirm the relevant `-tags=integration`
test actually starts its own broker via testcontainers — see how-to-test.md.
