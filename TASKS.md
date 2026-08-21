# Build Tasks — Workforce Management

Build the full bounded context described in CLAUDE.md, in order. Keep
`go build ./...` and `go test ./...` green throughout. Read
/Users/claudioed/docs/amazon-fulfillment-ddd.md for the domain model first.

## Task 0 — Skeleton
- `go mod init github.com/claudioed/workforce-management`; create the layout;
  .gitignore (bin/, .env); add chi + pgx deps.

## Task 1 — Domain (pure Go)
- shared: AssociateId, PathId, Certification value objects; DomainEvent + the
  10 named events.
- associate: AssociateShift aggregate (roster entry, certifications set, break
  state, hours logged; cannot assign while on break; hours <= max-per-shift).
- shiftplan: ShiftPlan aggregate with PathPlan lines (plannedHeads <=
  installedStations invariant; sum of hours valid).
- assignment: LaborAssignment aggregate (one associate, one path, an interval;
  exactly one ACTIVE assignment per associate; requires certification match).
- Unit tests for EVERY invariant, including the failing paths (four named in
  CLAUDE.md's Definition of Done).

## Task 2 — Application
- ports: AssociateRepo, ShiftPlanRepo, AssignmentRepo, EventPublisher, Clock.
- usecases: the 8 use cases from CLAUDE.md, depending only on domain + ports.
  ProposePathPlan is a pure computation (ceil(charge/rate)) with no persistence.
  AssignLabor must end any prior ACTIVE assignment for that associate before
  creating the new one (or reject — pick one behavior and document it in the
  README; either is defensible, but the double-booking invariant must hold).
- Unit-test against the in-memory adapter.

## Task 3 — Outbound adapters
- memory: thread-safe implementations of every port.
- postgres: pgxpool-backed repos + migrations (associate_shift, shift_plan,
  path_plan, labor_assignment, events tables). Build-tagged integration test
  (skip w/o DATABASE_URL).
- events: log/buffered EventPublisher; leave a kafka-ready interface.

## Task 4 — Inbound HTTP
- chi router, handlers for every endpoint in CLAUDE.md, request/response DTOs,
  domain-error -> HTTP status mapping, /healthz.
- httptest integration test per endpoint wired to in-memory repos.

## Task 5 — Composition root & ops
- cmd/workforce/main.go wires config (env) -> adapters -> use cases -> router.
- docker-compose.yml (Postgres 16). README.md with run steps + curl examples,
  plus the explicit "stops at the path boundary" design note.

## Task 6 — Verify
- `go build ./...`, `go vet ./...`, `go test ./...`, `go test ./... -race` all
  green. `gofmt -l .` empty. Confirm the four named invariants each have a
  red-path test. Do the smoke test: run the compiled binary and curl /healthz
  plus at least one write endpoint before declaring done. Do not stop until the
  Definition of Done in CLAUDE.md is met.
