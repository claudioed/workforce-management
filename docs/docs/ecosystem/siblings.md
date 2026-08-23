---
id: siblings
title: The five services
sidebar_label: The five services
sidebar_position: 3
description: What each bounded context in warehouse-systems owns, in its own vocabulary.
---

# The five services

All five are Go services with the same hexagonal layering, the same
`golangci-lint` config, the same RFC 7807 error format and the same
Postgres + Kafka stack. They share no code and no database.

## inventory-storage — WMS tier, **Core**

The authoritative record of **what is held where, and what portion is usable**.

Implements Amazon-style **chaotic (random) stow**: no fixed product location, an
item goes to any free bin, and the system records the exact bin. A stow is
invalid without *both* an item-scan and a location-scan — skipping either is
precisely how inventory becomes "lost."

Its defining design decision is the **revocable reservation**: allocation binds
a quantity to demand with a timeout, and can be released and re-allocated
against a different holding, because physical delivery genuinely fails (pod
blocked, tote lost, chute jam, short pick). It exposes **usable inventory** —
on-hand minus active reservations minus held/damaged — explicitly, because
usable, not total, is what constrains release.

Aggregates: `StockUnit`, `Bin`/`Location`, `Reservation`.

## wes-work-planning — WES tier, **Core**, the conductor

Turns a shift's **charge** (volume due by each CPT) into a **plan** (rate ×
heads per process path), then **releases work continuously** — waveless — and
performs **flow balancing** using live buffer telemetry, Drum-Buffer-Rope with
CPT as the drum.

This is the service that consumes this context's `ShiftPlanCommitted`. It
deliberately does not merge it into its own `ShiftPlan` aggregate; it projects
it into `LaborPlanObserved`, keyed by `path_id`, and optionally surfaces it as
extra context on a `RebalanceDecision`.

Aggregates: `ChargeForecast`, `ShiftPlan`/`PathPlan`, `WorkPool`, `WorkUnit`.

## workforce-management — **Supporting** — this service

Shift-start headcount planning per process path, plus intra-shift labor
assignment. Stops at the path boundary.

Aggregates: `AssociateShift`, `ShiftPlan`, `LaborAssignment`.

## fulfillment-execution — WES tier, **Core**

The **task lifecycle** for Pick, Pack and SLAM. Downstream of Work Planning,
upstream of WCS/equipment.

Its defining design rule is **pull, not push**: a station calls
`claimNext(stationId, capabilities)` and the system selects work — never
workers. There is deliberately no `assign(task, station)` operation. A claim
carries a **lease**; if it is not confirmed before expiry the task returns to
the pool rather than vanishing.

This is the context that owns everything past this service's path boundary. It
reads certifications as capabilities to gate a claim; it never writes to
`AssociateShift`.

Aggregates: `Task`, `Station`, `Package` (including the SLAM weigh-check that
diverts a package when actual weight deviates from expected beyond tolerance).

## facility-layout — **Generic**, Open Host Service

The physical warehouse map: the structural hierarchy of the building and the
coded storage slots inside it, on the industry-standard code shape
**Site → Area → Zone → Aisle → Bay → Level → Position**, e.g.
`WH1-STOR-AMB-A07-03-02-B`.

It owns whether a coded location **exists, is active, and is legal for a given
kind of storage unit** — the map other contexts read but never write. It does
*not* own occupancy or stock; that stays in `inventory-storage`.

Extracted as its own Generic subdomain for the same reason the platform DDD
reference gives for Cartonization: rather than implementing location validity
separately in `inventory-storage` (to accept a stow) and in the WES tier (for
travel-path and congestion reasoning), model it once and have both call into
it.

The newest of the five, and currently standalone — no service consumes it yet.

Aggregates: `Site`, `Zone`, `Aisle`, `LocationSlot`, `PlacementRule`.

## The tiering, at a glance

| | Horizon | Answers | Services |
| --- | --- | --- | --- |
| **WMS** | minutes → days | What needs to happen, and why | `inventory-storage` |
| **WES** | seconds → minutes | Who does it, right now, in what order | `wes-work-planning`, `fulfillment-execution`, and this service on the "who" half |
| **WCS** | ms → seconds | How the machine performs the next physical step | *not modelled in this platform* |
| **Generic** | — | Shared, non-differentiating truth | `facility-layout` |
