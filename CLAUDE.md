# Project: Workforce Management (Supporting Bounded Context)

Owns "who is on shift, on which process path, at what rate; direct vs indirect
hours." This is the **shift-start planning horizon** (a human commits a split of
headcount across paths) plus **intra-shift assignment tracking** (moving people
between paths as backlogs deviate — the move itself is still a human call; this
context makes the gap legible, it does not decide). It stops at the **path
boundary**: it never links an associate to a specific task — dispatch of
individual tasks to a claiming station belongs to Fulfillment Execution. Keeping
these apart is deliberate: it lets task-dispatch policy change without touching
workforce planning, and vice versa, because they change at completely different
cadences (shifts vs seconds).

Source of truth for the domain model: `/Users/claudioed/docs/amazon-fulfillment-ddd.md`
and `/Users/claudioed/warehouse-systems-ddd.md`. Honor that ubiquitous language.

## Architecture (NON-NEGOTIABLE)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on application/domain.**
No framework or SQL types in the domain layer.

```
cmd/workforce/                main.go — composition root
internal/
  domain/
    associate/                 AssociateShift aggregate (roster, certifications, breaks)
    shiftplan/                 ShiftPlan aggregate (committed headcount split across paths)
    assignment/                LaborAssignment aggregate (one associate, one path, an interval)
    shared/                    value objects: AssociateId, PathId, Certification, events
  application/
    ports/                     OUT: AssociateRepo, ShiftPlanRepo, AssignmentRepo, EventPublisher, Clock
    usecases/                  one struct per use case
  adapters/
    inbound/http/               chi handlers, DTOs, error mapping
    outbound/postgres/          pgxpool repos + migrations
    outbound/memory/            in-memory repos for tests/local
    outbound/events/            log/buffered publisher (kafka-ready iface)
migrations/                   golang-migrate SQL files
```

## Ubiquitous Language (use these exact names — do not invent synonyms)

- **ShiftPlan** — the committed split of headcount across paths for one shift.
  ONE per building per shift. Contains PathPlan lines: path, plannedHeads,
  plannedRate, plannedHours. Committed by a human; the software proposes
  (charge per path / planned rate = heads needed), a human commits it.
- **AssociateShift** — who is on, their certifications, their breaks. Owned here,
  referenced everywhere else (e.g. Fulfillment Execution reads certifications to
  gate station claims, but never writes here).
- **LaborAssignment** — one associate on one path for an interval. INVARIANT:
  exactly one ACTIVE assignment per associate at a time. The assignment MUST
  satisfy the path's certification requirement (reject if uncertified).
- **Certification** — a named qualification (e.g. "pack", "hazmat", "pick").
  An associate untrained on a path cannot be assigned to it; training is itself
  a path that consumes hours (do not special-case that here — just enforce the
  gate on assignment).
- **PathUnderstaffed** — a flag, not a decision: plannedHeads(path) not currently
  met by active assignments. Surfacing the gap, not moving anyone, is this
  context's job — moving people is a human call recorded via CommitAssignment.
- What this context explicitly does NOT do: it does not link an associate to a
  task, does not dispatch work, and does not decide rebalancing — it only makes
  the labor picture legible and enforces the two hard invariants below.

## Aggregates & invariants (enforce in domain, unit-tested)

- **ShiftPlan**: plannedHeads(path) ≤ installedStations(path) — the same
  invariant Work Planning enforces on its own PathPlan; enforce it here too,
  independently, since this is the aggregate that actually commits headcount.
  Sum of plannedHours per associate must not exceed a shift's max hours.
- **LaborAssignment**: exactly ONE active assignment per associate at a time
  (no double-booking across paths); assignment requires the associate holds the
  path's required certification, or it is rejected.
- **AssociateShift**: cannot be assigned while on a logged break; hours logged
  must not exceed a configured max-hours-per-shift limit.
- Read models (heads-planned-vs-active per path, per-associate utilization) are
  PROJECTIONS built from events — NOT state stored redundantly on aggregates.

## Domain events (past tense — use these exact names)

ShiftPlanProposed, ShiftPlanCommitted, AssociateShiftStarted, AssociateCertified,
AssociateBreakStarted, AssociateBreakEnded, LaborAssigned, LaborReassigned,
PathUnderstaffed, AssociateShiftEnded.

## Use cases (application layer)

1. StartAssociateShift(associateId, certifications) -> AssociateShift
2. CertifyAssociate(associateId, certification) -> adds a certification
3. ProposePathPlan(buildingId, charge-per-path, plannedRate) -> proposed heads
   (pure computation: heads = ceil(charge / rate); does not commit)
4. CommitShiftPlan(buildingId, pathPlans) -> ShiftPlan (validates plannedHeads
   <= installedStations; a human-initiated commit, not automatic)
5. AssignLabor(associateId, pathId) -> LaborAssignment (validates certification,
   validates no other ACTIVE assignment for this associate; ends prior assignment)
6. StartBreak(associateId) / EndBreak(associateId)
7. GetStaffingGap(pathId) -> plannedHeads vs activeAssignments read model; may
   raise PathUnderstaffed
8. EndAssociateShift(associateId) -> closes all active assignments, AssociateShiftEnded

## REST API (inbound adapter)

- POST /associates/{id}/start-shift              -> StartAssociateShift
- POST /associates/{id}/certifications            -> CertifyAssociate
- POST /paths/{pathId}/plan/propose               -> ProposePathPlan
- POST /shift-plans                               -> CommitShiftPlan
- POST /associates/{id}/assignments               -> AssignLabor
- POST /associates/{id}/break/start               -> StartBreak
- POST /associates/{id}/break/end                 -> EndBreak
- GET  /paths/{pathId}/staffing-gap                -> GetStaffingGap
- POST /associates/{id}/end-shift                  -> EndAssociateShift
- GET  /healthz

JSON DTOs live in the http adapter; never leak domain structs directly.

## Tech & standards

- Go 1.26, modules. Module path: `github.com/claudioed/workforce-management`.
- chi (github.com/go-chi/chi/v5), pgx/v5 + pgxpool, golang-migrate SQL migrations.
- Config via env (DATABASE_URL, HTTP_ADDR). docker-compose.yml for Postgres 16.
- Typed domain errors mapped to HTTP status in the adapter.
- Table-driven tests: domain + application (in-memory adapter); one httptest per
  endpoint; build-tagged Postgres integration test (skipped w/o DATABASE_URL).
- gofmt/go vet clean; every package has a doc comment.

## Definition of done

- `go build ./...`, `go vet ./...`, `go test ./...` (and `-race`) all green.
- gofmt clean.
- README.md: run steps (compose/migrate/go run), endpoints w/ curl examples, a
  layering note, and a short explicit note on the "stops at the path boundary"
  design decision (why no associate-to-task link exists here).
- These invariants each have a failing-path test: plannedHeads > installedStations
  rejected on ShiftPlan commit; double-booking (second ACTIVE assignment for the
  same associate) rejected; assignment without required certification rejected;
  assignment while on an active break rejected.
