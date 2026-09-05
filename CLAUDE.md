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

## Analytics data product (ADR-0010)

Additive read side built from this service's OWN domain events. The OLTP
domain/application layers are NOT modified and must NOT import the analytics
store (arch-test enforces). `internal/analytics/report/` depends on nothing.
(`ProcessedEvents` in application/ports is the shared idempotency gate, used
only by the analytics projector — the OLTP write path does not depend on it.)

- Events are fanned to a SEPARATE topic `warehouse.workforce.analytics` by a new
  outbound adapter; the integration topic/publisher are untouched. Selected by
  `EVENT_PUBLISHER=kafka` (fan-out alongside the integration publisher).
- Separate analytical Postgres (`ANALYTICS_DATABASE_URL`), own migrations
  (`migrations/analytics/`), read-only reader role.
- Three processes: `cmd/workforce` (OLTP), `cmd/workforce-projector` (the ONLY
  writer; consumes from FirstOffset, idempotent on event_id),
  `cmd/workforce-reports` (read-only reader, `GET /reports/...`). MCP report tool too.
- Report: **Labor Utilization & Staffing**, keyed per path/shift × hour (shifts
  started/ended, break time, labor assigned/reassigned, understaffing).
- `GET /reports/.../freshness` reports projection lag.

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
  gate on assignment). `"hazmat"` is a REAL, in-use value, not hypothetical:
  a path literally named `"hazmat"` requires the associate hold the `"hazmat"`
  certification, via the existing path-name-equals-certification-name
  convention — no new code was needed to support it (ADR-0009). This is the
  independent, path-level half of hazmat handling; `fulfillment-execution`
  separately gates hazmat at the station-capability level for individual task
  claims — different bounded context, different mechanism, same real-world
  concern.
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

CORS middleware (`go-chi/cors`) is enabled on every route, allowing
`CORS_ALLOWED_ORIGINS` (env, default `http://localhost:5173,http://localhost:5185`
— the `warehouse-console` shell and this service's own `workforce-mfe`
remote). This service is not part of the fleet's cross-service Order
Lifecycle read model (see ADR-0002 in `warehouse-ops-agent`'s docs) — no
order-lifecycle stage touches workforce/staffing state — CORS here exists
solely for this service's own `workforce-mfe` screen below.

## Frontend micro-frontend remote (`web/`)

This repo also owns `web/`: `workforce-mfe`, a Vite + React Module
Federation **remote** consumed by the separate `warehouse-console` shell
repo. It is a plain browser client of this service's own REST API above
(staffing-gap-by-path dashboard) — nothing in `web/` talks to any other
bounded context, and nothing in `internal/` knows `web/` exists. `web/`
has its own `package.json`, build, and dev server (`:5185`); it does not
participate in this repo's Go quality gate and is not part of the Go
module.

## Tech & standards

- Go 1.26, modules. Module path: `github.com/claudioed/workforce-management`.
- chi (github.com/go-chi/chi/v5), pgx/v5 + pgxpool, golang-migrate SQL migrations.
- Config via env (DATABASE_URL, HTTP_ADDR). docker-compose.yml for Postgres 16.
- Typed domain errors mapped to HTTP status in the adapter.
- Table-driven tests: domain + application (in-memory adapter); one httptest per
  endpoint; build-tagged Postgres integration test (skipped w/o DATABASE_URL).
- gofmt/go vet clean; every package has a doc comment.

## Local quality gate (run before every commit)

- After making changes and **before committing**, run `make check`. That is the
  fast self-correction loop: `fmt-check`, `vet`, `build`, `lint`, `test`
  (`go test ./... -race`). It needs no database and finishes in about a minute.
- **Before pushing**, run `make check-all` — `check` plus the 90% `coverage`
  gate, `arch-test` (hexagonal fitness) and `bdd` (godog/Gherkin acceptance).
- Run `make vuln` (`govulncheck ./...`) after touching `go.mod`/`go.sum`; it is
  a blocking CI job and it flags known CVEs in the dependency graph and stdlib.
- `make mutation` runs the fast gremlins subset that blocks in CI
  (`./internal/domain/shiftplan`, thresholds in `.gremlins.yaml`);
  `make mutation-full` is the exhaustive scheduled run over `./internal/domain`.
- `make integration` needs a running Postgres and `DATABASE_URL`; it is
  deliberately outside `check`/`check-all`.
- The lefthook git hooks enforce this automatically once you have run
  `lefthook install` locally (pre-commit: fmt-check/vet/lint; pre-push:
  `make check`) — but run `make check` proactively rather than relying on the
  hook, since hooks are per-clone and may not be installed.
- Why: it keeps quality *left* (harness engineering) — the CI sensors are
  available locally so problems are caught and self-corrected before they ever
  reach a human reviewer or the pipeline.

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
