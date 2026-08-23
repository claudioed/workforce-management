---
id: ubiquitous-language
title: Ubiquitous language
sidebar_label: Ubiquitous language
sidebar_position: 5
description: The exact vocabulary of this bounded context, with the definitions the code implements.
---

# Ubiquitous language

These are the exact names used in the domain model, the API, the events and
this documentation. Synonyms are not accepted: there is no "worker," no
"employee," no "job," no "workstation assignment."

## Core terms

| Term | Definition |
| --- | --- |
| **ShiftPlan** | The committed split of headcount across paths for one shift. **One per building per shift.** Contains `PathPlan` lines. Committed by a human — the software proposes, a human commits. |
| **PathPlan** | One line of a `ShiftPlan`: `pathId`, `plannedHeads`, `plannedRate`, `plannedHours`. |
| **AssociateShift** | Who is on, their certifications, their breaks, their logged hours. Owned here, referenced everywhere else. Other contexts read certifications to gate station claims; none of them write here. |
| **LaborAssignment** | One associate on one path for an interval. Exactly one ACTIVE assignment per associate at a time. Must satisfy the path's certification requirement, or it is rejected. |
| **Certification** | A named qualification — `pack`, `hazmat`, `pick`. An associate untrained on a path cannot be assigned to it. Training is itself a path that consumes hours; it is not special-cased, because the gate lives on assignment. |
| **PathUnderstaffed** | A **flag, not a decision**: `plannedHeads(path)` is not currently met by active assignments. Surfacing the gap is this context's job; moving people is a human call, recorded via `AssignLabor`. |
| **Process path** | A named station type that owns a queue — `pack`, `pick`, `stow`, `SLAM`. Not a workflow step. The finest granularity this context addresses. |
| **Charge** | The volume that must clear on a path. Input to `ProposePathPlan`; never stored here. Owned upstream by `wes-work-planning`. |
| **Planned rate** | Expected throughput per head per hour on a path. Input to the proposal arithmetic. |
| **Installed stations** | How many physical positions a path has. Supplied by the caller on `CommitShiftPlan`; the ceiling on `plannedHeads`. |
| **Direct vs indirect hours** | Direct hours are spent on a production path; indirect hours are everything else (training, breaks, meetings). Both consume the shift's hour budget. |
| **Break** | A logged, explicitly-started and explicitly-ended interval during which an associate cannot be assigned. |

## Terms this context deliberately does not have

| Absent term | Owned by | Why not here |
| --- | --- | --- |
| **Task** | `fulfillment-execution` | This context stops at the path boundary. See [The path boundary](./path-boundary.md). |
| **Station** (as an occupiable position) | `fulfillment-execution` | Here, `installedStations` is only a **count** used as a capacity ceiling — never an entity with an occupant. |
| **Work unit / release** | `wes-work-planning` | What work exists and when it is released is a different context entirely. |
| **Bin, SKU, reservation** | `inventory-storage` | Stock truth. |
| **Zone, aisle, location code** | `facility-layout` | Physical geography. |

## Same word, different model

`ShiftPlan` exists in **both** this context and `wes-work-planning`, and they
are **different models**. This is the classic DDD "same term, different bounded
context" situation, and the platform handles it explicitly rather than by
sharing a type:

- **Here**, `ShiftPlan` is the labor commitment — it is the authoritative record
  that a human committed *these heads to these paths*.
- **In `wes-work-planning`**, `ShiftPlan` is that service's own planning
  artefact, derived from charge and CPT.

When `wes-work-planning` consumes this context's `ShiftPlanCommitted` event, it
explicitly does **not** feed it into its own `ShiftPlan` aggregate. It projects
it into a separate read model called `LaborPlanObserved`, keyed by `path_id`.
Conflating the two would be exactly the trap the platform DDD reference warns
about:

> Same English word, two different models. Do not share the class across
> contexts.

## Ubiquitous language in the code

Every term above appears verbatim as a Go identifier. The mapping is
one-to-one:

| Term | Go |
| --- | --- |
| ShiftPlan / PathPlan | `internal/domain/shiftplan.ShiftPlan`, `.PathPlan` |
| AssociateShift | `internal/domain/associate.AssociateShift` |
| LaborAssignment | `internal/domain/assignment.LaborAssignment` |
| Certification | `internal/domain/shared.Certification` |
| PathId / AssociateId | `internal/domain/shared.PathId`, `.AssociateId` |
| PathUnderstaffed | `internal/domain/shared.PathUnderstaffed` |
