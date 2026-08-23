---
id: 0002-stop-at-the-path-boundary
title: 0002. Stop at the path boundary — no associate-to-task link
sidebar_label: 0002. Stop at the path boundary
sidebar_position: 3
description: LaborAssignment ends at "this associate is on this path." There is no taskId in this service.
---

# 0002. Stop at the path boundary — no associate-to-task link

## Status

Accepted. Established with the initial bounded-context definition and stated
explicitly in this repo's `CLAUDE.md` and `README.md`.

## Context

Workforce Management knows who is on shift, what they are certified for, and
which process path they are working. `fulfillment-execution` knows what
individual tasks exist and which station is doing which one. Both are, in a
loose sense, "assigning work to people," and the obvious question is whether
they should be one model.

The forces:

**Cadence differs by three orders of magnitude.** Workforce assignment changes
over minutes to hours, as a supervisor rebalances headcount across paths in
response to backlogs. Task dispatch changes over seconds, as stations claim work
off a queue. A single aggregate would have to hold a consistency boundary
spanning both.

**The decision-makers differ.** Moving an associate from `pick` to `pack` is a
human judgement, informed by things no system can see — which aisle is blocked,
who is mid-tote, what was promised to the pack line ten minutes ago. Handing the
next task to a station is a policy computation with no human in the loop.

**The lifecycles differ.** A path assignment is an interval of a shift. A task
claim is at-most-once, carries a **lease**, and returns to the pool if it is not
confirmed before expiry. Nothing about the second concept applies usefully to
the first.

**The platform reference model does not settle it.** It describes a single
WES-tier `Assignment` aggregate — an ephemeral `Task → Resource → Time` binding
recomputed continuously by an `AssignmentOptimizer` domain service. That is a
coherent model when the optimizer is automated. Here it is not: this platform's
rebalancing authority is human.

The cost of fusing them is concrete: every task-dispatch policy change — a new
priority rule, a different lease timeout, a change to `claimNext`'s heuristic —
would touch workforce planning code, even though nothing about the labor picture
changed. And every change to how headcount moves between paths would touch
dispatch code, for the same non-reason.

## Decision

We will **stop this bounded context at the path boundary**.

`LaborAssignment` records *one associate, one path, an interval* — and nothing
finer. There is no `taskId` in this service, no endpoint that hands a unit of
work to a person, and no queue. Dispatch of individual tasks to a claiming
station belongs entirely to `fulfillment-execution`.

The corollary: this context also does not **decide** rebalancing. When active
assignments fall below a path's committed `plannedHeads`, it raises
`PathUnderstaffed` — a **flag, not a decision**. It surfaces the gap. A human
responds, and records the response with `AssignLabor`.

`fulfillment-execution` gets what it needs from this context **read-only**: it
reads certifications to gate a station claim, and may read the staffing-gap read
model (`GetStaffingGap`). It never writes here, and this context never learns
what task anybody is performing.

From `CLAUDE.md`:

> Keeping these apart is deliberate: it lets task-dispatch policy change
> without touching workforce planning, and vice versa, because they change at
> completely different cadences (shifts vs seconds).

## Consequences

**Easier**

- `fulfillment-execution` can change its dispatch model wholesale — push to
  pull, leases to reservations, a new priority function — without a line
  changing here.
- This service's consistency boundary is small enough to be obvious. Each
  aggregate holds one shift-scoped decision.
- The two services deploy, scale and fail independently. A Supporting context is
  not in a Core context's critical path.
- `PathUnderstaffed` stays honest. There is no optimiser to explain, tune or
  defend when it makes a bad call, because there is no optimiser.

**Harder**

- **You cannot answer "what is Alice doing right now?" from this service.** You
  can answer "which path is Alice on?" Getting the task means joining against
  `fulfillment-execution`. This is a real ergonomic cost, accepted knowingly.
- Utilization reporting that needs task-level detail has to span two services.
- The staffing gap is eventually consistent with physical reality: it is derived
  from assignments a human recorded, not from anyone's actual presence.
- Somebody will propose adding a `taskId` "just for reporting." The answer is a
  read model in whichever service already owns the join, not a field here.

**Now true**

- The absence of an edge between `workforce-management` and
  `fulfillment-execution` on the [context map](../ecosystem/context-map.md) is a
  documented decision, not a gap in the diagram.
- If an automated `AssignmentOptimizer` is ever built, this ADR is the first
  thing that needs revisiting — and the [context
  relationships](../ddd/context-relationships.md) page explains what would
  change with it.
