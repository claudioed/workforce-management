---
id: integration
title: Integration
sidebar_label: Integration
sidebar_position: 4
description: The topics this service publishes and consumes, its two synchronous sibling calls, the envelope on the wire, and how to smoke-test it.
---

# Integration

One integration topic out (plus a separate analytics topic). Up to two topics
in and up to two synchronous calls to siblings, each selected by an env var
and off by default. The table below lists every edge in
`cmd/workforce/main.go`.

| Direction | Counterpart | Mechanism | Selected by | Default |
| --- | --- | --- | --- | --- |
| Out | `wes-work-planning` | Kafka `warehouse.workforce.events` (`ShiftPlanCommitted`) | `EVENT_PUBLISHER=kafka` | `log` (no broker) |
| Out | own analytics projector | Kafka `warehouse.workforce.analytics` (every domain event) | `EVENT_PUBLISHER=kafka` | `log` |
| In (sync) | `fulfillment-execution` | `GET /capacity/{capability}` on every `CommitShiftPlan` ([ADR 0014](../adr/0014-installed-capacity-ceiling.md)) | `INSTALLED_CAPACITY_MODE=http` + `FULFILLMENT_EXECUTION_BASE_URL` | `permissive` = **every commit fails with 503** |
| In (sync) | `labor-performance` | `GET /task-types/{taskType}/performance` when `ProposePathPlan` has no caller rate ([ADR 0012](../adr/0012-measured-rate-feed-for-propose-path-plan.md)) | `LABOR_PERFORMANCE_MODE=http` + `LABOR_PERFORMANCE_BASE_URL` | `permissive` = no measured rate (fail-open) |
| In (async) | `labor-performance` | Kafka `warehouse.labor-performance.events` (`TaskPerformanceRecorded`) into an in-memory cache ([ADR 0019](../adr/0019-labor-performance-cache-consumer.md), [ADR 0020](../adr/0020-idle-share-staffing-signal.md)) | `LABOR_PERFORMANCE_MODE=kafka-cache` + `KAFKA_BROKERS` | off |
| In (async) | `process-path-management` | Kafka `warehouse.process-path-management.events` (`ProcessPathCreated`/`Updated`/`Deactivated`) into the in-memory process-path catalogue | `PATH_CATALOGUE_SOURCE=kafka` + `KAFKA_BROKERS` | `file` (`PATH_CATALOGUE_FILE`) |

The `warehouse-infra` kind cluster sets `INSTALLED_CAPACITY_MODE=http` and
`LABOR_PERFORMANCE_MODE=kafka-cache`, and sets `PATH_CATALOGUE_SOURCE=kafka`
when its `deploy_process_path_kafka_source` flag is on. All of these edges are
therefore live there.

## What is published

| | |
| --- | --- |
| **Topic** | `warehouse.workforce.events` |
| **Event** | `ShiftPlanCommitted` — and only this one, today |
| **Trigger** | a successful `CommitShiftPlan` |
| **Fan-out** | **one message per `PathPlan` line** |
| **Client** | `github.com/segmentio/kafka-go` |
| **Adapter** | `internal/adapters/outbound/kafka/publisher.go` |
| **Consumer** | `wes-work-planning`, into its `LaborPlanObserved` read model, keyed by `path_id` |

## Selecting the publisher

The Kafka publisher is off by default. Both publishers implement the same
`ports.EventPublisher` interface, so nothing above the adapter layer knows the
difference.

| Variable | Default | Meaning |
| --- | --- | --- |
| `EVENT_PUBLISHER` | `log` | `log` (in-memory/log publisher) or `kafka` |
| `KAFKA_BROKERS` | `localhost:9092` | comma-separated broker list, used when `EVENT_PUBLISHER=kafka` |

The default keeps local runs and the whole test suite free of any broker
dependency.

## The fan-out

A `ShiftPlan` has multiple `PathPlan` lines. `CommitShiftPlan` with three path
lines publishes **three** Kafka messages, one per line, each carrying that
single line's `planned_heads`/`planned_rate`/`planned_hours` alongside the
plan's `building_id` and `shift_id`.

This matches how the consumer keys its read model: `LaborPlanObserved` is one
row per path. Consumers must expect N messages per commit and must not assume a
message carries the whole plan.

The domain event carries only the `ShiftPlan`'s identity (`buildingId`,
`shiftId`). The adapter loads the committed plan through the `ShiftPlanRepo` to
expand it. That keeps fan-out an integration concern: the domain has no opinion
about message granularity.

## The envelope on the wire

The Kafka adapter writes the **flat cross-service envelope** that every
`warehouse-systems` service shares, exactly as specified in this repo's
`INTEGRATION.md`:

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

:::caution The wire format and the AsyncAPI catalog differ today
`apis/asyncapi.yaml` documents the **CloudEvents 1.0 structured-mode** envelope
(`specversion`/`id`/`source`/`type`/`subject`/`time`/`datacontenttype` at the
top level) as this context's published contract, with reverse-DNS `type` values
like
`com.warehouse.wes.workforce-management.shiftplan.ShiftPlanCommitted`.

The shipped Kafka adapter still writes the older flat envelope shown above,
because that is what `wes-work-planning`'s consumer parses today and what the
cross-service smoke test was verified against. The AsyncAPI catalog is the
**target** contract; the flat envelope is what is **on the wire** as of this
version. Both are documented rather than one being quietly presented as the
other. See the [Events page](../api-reference/events.md) for the full
CloudEvents catalog and [ADR 0004](../adr/0004-kafka-integration-events-and-cloudevents-catalog.md)
for the reasoning and the migration path.
:::

## Smoke-testing the edge

The fleet runs one shared Kafka broker: the in-cluster release in the
`warehouse-infra` kind cluster, reachable from the host at `localhost:9092`.
This repo's own `docker-compose.yml` only runs Postgres — do not add a broker
to it.

```bash
export EVENT_PUBLISHER=kafka
export KAFKA_BROKERS=localhost:9092
go run ./cmd/workforce

# two path lines -> expect two messages
curl -X POST localhost:8080/shift-plans \
  -d '{"buildingId":"bldg-1","shiftId":"shift-1","lines":[
        {"pathId":"pack","plannedHeads":3,"plannedRate":30,"plannedHours":24,"installedStations":10},
        {"pathId":"pick","plannedHeads":2,"plannedRate":25,"plannedHours":16,"installedStations":10}
      ]}'

# in another terminal (any Kafka CLI pointed at the shared broker, e.g.
# kubectl exec into kafka-controller-0 in the warehouse-systems namespace)
kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic warehouse.workforce.events \
  --from-beginning --max-messages 2
```

## What is deliberately not published

`LaborAssigned`, `LaborReassigned` and `PathUnderstaffed` stay in-process.

Publishing individual assignment moves would let a downstream context
reconstruct a per-associate location feed — exactly the picture the
[path boundary](../business-context/path-boundary.md) exists to withhold. If a
real downstream need appears, the right answer is a read-model endpoint with a
defined shape, not a firehose of moves.

The remaining `AssociateShift` events are in-process for the simpler reason
that nobody has asked: no sibling consumes roster or break events today.

## What is consumed

Two sibling topics, both **opt-in** and both used only to build an in-memory
cache that the `cmd/workforce` process rebuilds from the earliest offset on
every start:

- **`warehouse.process-path-management.events`**
  (`internal/adapters/outbound/kafkacatalog`, `PATH_CATALOGUE_SOURCE=kafka`).
  It replaces the boot-time `PATH_CATALOGUE_FILE` read. Path ids on propose,
  commit, assign and staffing-gap requests are validated against this cache
  ([ADR 0013](../adr/0013-process-path-catalogue-validation.md)).
- **`warehouse.labor-performance.events`**
  (`internal/adapters/outbound/laborperformancecache`,
  `LABOR_PERFORMANCE_MODE=kafka-cache`). It supplies measured rates to
  `ProposePathPlan` and the observed idle share to `GetStaffingGap` and
  `ProposePathPlan`.

Both consumers use a **per-process-unique consumer group** (prefix + host +
PID + timestamp), so every process replays the full history. Before serving
traffic, each one waits up to 60s (`WaitReadyTimeout`) for the replay to catch
up. Neither writes to Postgres, so there is no processed-events table on the
OLTP side. The only dedupe table (`analytics_processed_events`) belongs to
the analytics projector
(`cmd/workforce-projector`), which consumes this service's own
`warehouse.workforce.analytics` topic under the fixed group
`workforce-analytics`.

## Synchronous calls to siblings

- **`fulfillment-execution` — `GET /capacity/{capability}`**
  (`internal/adapters/outbound/fulfillmentexecution`). It is called for every
  line of every `CommitShiftPlan` and is **fail-loud**: any failure rejects the
  whole commit with `503 installed-capacity-unavailable`. The default
  `permissive` client always fails, so a commit only succeeds when
  `INSTALLED_CAPACITY_MODE=http`.
- **`labor-performance` — `GET /task-types/{taskType}/performance`**
  (`internal/adapters/outbound/laborperformance`). It is called only when
  `LABOR_PERFORMANCE_MODE=http` and `ProposePathPlan` gets no positive
  `plannedRate`. It is **fail-open**: on any failure the proposal falls back to
  the caller rate.

Both clients send no `Authorization` header
([ADR 0018](../adr/0018-remove-fleet-rest-identity.md)).
