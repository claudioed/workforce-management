---
id: quickstart
title: Run it locally
sidebar_label: Quickstart
sidebar_position: 2
description: Bring up Postgres, run the service, and walk one shift end to end with curl.
---

# Run it locally

The service is a single Go binary. It needs Postgres; Kafka is optional and
off by default.

## 1. Start Postgres and the service

```bash
docker compose up -d   # Postgres 16 on localhost:5432

export DATABASE_URL="postgres://workforce:workforce@localhost:5432/workforce?sslmode=disable"
go run ./cmd/workforce   # applies migrations, then serves on :8080
```

## 2. Configuration

Everything is environment-driven — there is no config file.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | — | Postgres connection string |
| `HTTP_ADDR` | no | `:8080` | listen address |
| `MIGRATIONS_PATH` | no | `migrations` | path to the golang-migrate SQL files |
| `MAX_HOURS_PER_SHIFT` | no | `8` | the configured max-hours-per-shift cap |
| `EVENT_PUBLISHER` | no | `log` | `log` or `kafka` — see [Integration](../ecosystem/integration.md) |
| `KAFKA_BROKERS` | no | `localhost:9092` | comma-separated broker list, used when `EVENT_PUBLISHER=kafka` |

## 3. Walk one shift end to end

```bash
# 1. Someone clocks on, certified to pack
curl -X POST localhost:8080/associates/assoc-1/start-shift \
  -d '{"certifications":["pack"]}'

# 2. Give them a second qualification
curl -X POST localhost:8080/associates/assoc-1/certifications \
  -d '{"certification":"hazmat"}'

# 3. The software proposes: 100 units of charge at 30 units/hour needs 4 heads
curl -X POST localhost:8080/paths/pack/plan/propose \
  -d '{"buildingId":"bldg-1","charge":100,"plannedRate":30}'

# 4. A human commits the split (this is the ShiftPlan)
curl -X POST localhost:8080/shift-plans \
  -d '{"buildingId":"bldg-1","shiftId":"shift-1","lines":[
        {"pathId":"pack","plannedHeads":3,"plannedRate":30,"plannedHours":24,"installedStations":10}
      ]}'

# 5. Put the associate on the pack path
curl -X POST localhost:8080/associates/assoc-1/assignments \
  -d '{"pathId":"pack"}'

# 6. Breaks gate assignment while they are open
curl -X POST localhost:8080/associates/assoc-1/break/start
curl -X POST localhost:8080/associates/assoc-1/break/end

# 7. Is the pack path short of its committed heads?
curl "localhost:8080/paths/pack/staffing-gap?buildingId=bldg-1&shiftId=shift-1"

# 8. Clock off — closes any active assignment first
curl -X POST localhost:8080/associates/assoc-1/end-shift

curl localhost:8080/healthz
```

Step 7 returns the staffing-gap read model — planned versus active, and the
`understaffed` flag:

```json
{"pathId":"pack","plannedHeads":3,"activeHeads":1,"understaffed":true}
```

That flag is the whole point of this context's intra-shift half: it says the
gap exists. It does not move anybody. See
[Why it stops at the path boundary](../business-context/path-boundary.md).

## 4. Test it

```bash
go build ./...
go vet ./...
go test ./...
go test ./... -race
gofmt -l .                      # should print nothing

# Postgres integration tests (build-tagged, skipped without DATABASE_URL)
go test -tags=integration ./internal/adapters/outbound/postgres/...

# Gherkin acceptance specs, driven through the real HTTP surface
go test ./... -run TestFeatures -v
```

## 5. Regenerate this site's API reference

The REST pages under **API Reference → REST API** are generated from
`apis/openapi.yaml`, not hand-written. After changing the spec:

```bash
cd docs
npm run clean-api-docs
npm run gen-api-docs
npm run build
```
