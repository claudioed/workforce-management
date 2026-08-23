---
id: integration
title: Integration
sidebar_label: Integration
sidebar_position: 4
description: The one topic this service publishes, the envelope on the wire, and how to smoke-test it.
---

# Integration

One topic out. Nothing in. No synchronous call to any sibling.

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

A shared broker runs at `localhost:9092` via
`~/warehouse-systems/docker-compose.kafka.yml`. This repo's own
`docker-compose.yml` only runs Postgres — do not add a broker to it.

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

# in another terminal
docker exec warehouse-kafka /opt/kafka/bin/kafka-console-consumer.sh \
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

Nothing. This service has no inbound Kafka adapter and no consumer group.

That is a real architectural property, not a to-do: with no inbound event
stream there is no idempotency machinery to get right, no `processed_events`
table, and no redelivery semantics to reason about. `wes-work-planning`, which
does consume, carries all of that.
