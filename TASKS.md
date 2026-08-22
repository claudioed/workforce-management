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

## Task 7 — Cross-service integration (additive, see INTEGRATION.md)
- Read INTEGRATION.md in this repo first.
- Add `github.com/segmentio/kafka-go` dependency.
- New Kafka outbound publisher adapter implementing the existing EventPublisher
  port, publishing one ShiftPlanCommitted message per PathPlan line to
  warehouse.workforce.events, selected via EVENT_PUBLISHER env (default "log").
- Unit test the envelope shape + the one-message-per-path-line fan-out.
- README gains an Integration section. REAL smoke test against the shared
  broker (docker-compose.kafka.yml in ~/warehouse-systems, localhost:9092):
  call POST /shift-plans with 2+ path lines and confirm 2+ messages land on
  the topic before declaring done.
- Full existing suite (build/vet/test/-race) must still be green afterward.

## Task 10 — Quality engineering: linting, coverage, integration tests, mutation tests, CI
Full spec in QUALITY.md at the repo root. Five ordered stages, each gates the
next: (1) golangci-lint clean via the committed .golangci.yml, (2) unit test
coverage >= 90% on internal/domain/... + internal/application/... combined,
(3) real integration tests against live Postgres for every outbound Postgres
adapter, (4) gremlins mutation testing on internal/domain/... only
(exploratory, triaged not gated), (5) .github/workflows/ci.yml — lint+unit+
integration blocking on every push/PR, mutation testing on a weekly schedule/
manual dispatch only, never blocking PRs. Do not stop until every stage's
Definition of Done in QUALITY.md is met, then report the final numbers.
