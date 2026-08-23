---
id: 0001-hexagonal-ports-and-adapters
title: 0001. Hexagonal (ports and adapters) architecture
sidebar_label: 0001. Hexagonal architecture
sidebar_position: 2
description: Domain depends on nothing; application depends on domain; adapters depend on both.
---

# 0001. Hexagonal (ports and adapters) architecture

## Status

Accepted. Established with the initial implementation of the bounded context
(commit `875d7b0`), and shared verbatim by all five `warehouse-systems`
services.

## Context

This bounded context holds four rules that are worth enforcing absolutely:
`plannedHeads ≤ installedStations`, exactly one active assignment per
associate, a certification gate on assignment, and no assignment during a
logged break. Each is a statement about the domain, not about HTTP or SQL.

The default Go service shape — handlers that take an `*http.Request`, call into
a service struct that takes a `*sql.DB`, and marshal a row into a response —
puts those four rules somewhere between a handler and a query. That has three
consequences that matter here:

- **The rules become expensive to test.** Asserting that a second active
  assignment is impossible now needs a database, or a mock of one.
- **The rules become bypassable.** A second write path — a batch import, an
  admin endpoint, a repair script — can reach the table without passing the
  check, and nothing structural prevents it.
- **The rules become coupled to infrastructure churn.** Swapping `database/sql`
  for `pgx`, or `net/http` for `chi`, touches code that expresses domain
  policy.

There is also a platform constraint: five services, each owned by a different
context, all needing to be legible to someone who normally works on a different
one. A shared structural convention has value beyond any individual service.

## Decision

We will structure the service as **hexagonal / ports and adapters**, with a
strict dependency rule:

> **domain depends on nothing; application depends on domain; adapters depend
> on application and domain.**

Concretely:

- `internal/domain/` — pure Go. Aggregates, value objects, domain events, typed
  domain errors. **No framework type and no SQL type appears here**, ever.
- `internal/application/ports/` — outbound port interfaces (`AssociateRepo`,
  `ShiftPlanRepo`, `AssignmentRepo`, `EventPublisher`, `Clock`), declared by the
  application in terms of domain types.
- `internal/application/usecases/` — one struct per use case, orchestrating
  domain objects through ports.
- `internal/adapters/inbound/http/` — chi handlers, adapter-local DTOs,
  domain-error-to-HTTP-status mapping.
- `internal/adapters/outbound/{postgres,memory,events,kafka,clock}/` — port
  implementations.
- `cmd/workforce/` — the composition root, and the only place that knows about
  all of the above at once.

`Clock` is a port for the same reason the repositories are: time-dependent
domain behaviour (interval hours, break duration) must be testable without
sleeping.

## Consequences

**Easier**

- Every invariant is unit-testable in pure Go with no test double at all. The
  four named invariants each have a failing-path test at the domain layer,
  running in milliseconds.
- The in-memory adapter set makes the *application* layer equally testable, and
  makes local runs and the acceptance suite free of infrastructure.
- Combined coverage on `internal/domain/...` + `internal/application/...`
  reached 98.2% against a 90% gate — achievable precisely because the code
  under test has no infrastructure to stand up.
- Infrastructure is swappable at the composition root. The Kafka publisher
  (`EVENT_PUBLISHER=kafka`) was added in a later task as a **new adapter only**,
  with no change to any aggregate, invariant or use case.
- The postgres adapters have their own build-tagged integration tests against a
  real Postgres 16, kept entirely separate from the domain suite.

**Harder**

- More packages and more indirection than a flat service. A one-field addition
  touches a domain type, a port, two adapter implementations and a DTO.
- Mapping code — domain ↔ row, domain ↔ DTO — is written by hand and is pure
  overhead in the small.
- The discipline is only as good as its enforcement, which is why it is checked
  by executable tests rather than review
  ([ADR 0007](./0007-arch-go-architecture-fitness-tests.md)).

**Now true**

- Reading `internal/domain/` tells you the entire rulebook of this bounded
  context with no infrastructure noise.
- A layering violation fails CI.
- Anyone who has read one `warehouse-systems` service can navigate the other
  four.
