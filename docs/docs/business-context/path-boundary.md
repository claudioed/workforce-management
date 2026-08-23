---
id: path-boundary
title: Why it stops at the path boundary
sidebar_label: The path boundary
sidebar_position: 3
description: This context never links an associate to a task. That refusal is the design, and here is why.
---

# Why it stops at the path boundary

This is the most consequential decision in the service, so it gets its own
page. Recorded formally as
[ADR 0002](../adr/0002-stop-at-the-path-boundary.md).

## The rule

**Workforce Management never links an associate to a specific task.**

`LaborAssignment` ends at *"this associate is on this path"* — pack, pick,
stow, SLAM — for an interval of shift-length granularity. There is no
`taskId` anywhere in this codebase. There is no endpoint that hands work to a
person. There is no queue.

## The reason: cadence

Workforce assignment and task dispatch look superficially similar — both are
"assign a thing to a person" — and they change at rates that differ by three
orders of magnitude:

| | Workforce assignment (here) | Task dispatch (fulfillment-execution) |
| --- | --- | --- |
| Unit | an associate on a **path** | a **task**: one pick, one pack, one SLAM |
| Cadence of change | minutes to hours | seconds |
| Trigger | a human rebalancing headcount | a station calling `claimNext` |
| Lifetime | an interval of a shift | until confirmed, or until the lease expires |
| Decided by | a supervisor | the dispatch policy, pull-based |

Fusing the two into one aggregate would mean that every change to
task-dispatch policy — a new priority rule, a new lease timeout, a different
`claimNext` heuristic — has to touch workforce planning code, and every change
to how headcount is rebalanced has to touch dispatch code. Nothing about the
labor picture actually changed in the first case; nothing about dispatch
changed in the second. The coupling would be pure accident of packaging.

From this repo's own `CLAUDE.md`:

> Keeping these apart is deliberate: it lets task-dispatch policy change
> without touching workforce planning, and vice versa, because they change at
> completely different cadences (shifts vs seconds).

## What the seam looks like in practice

`fulfillment-execution` needs two things from the labor world, and gets both
without ever writing here:

1. **Certifications**, to gate a station claim. It reads them; it never
   modifies them. `AssociateShift` remains the single writer.
2. **The staffing picture**, if it wants it, via this context's *read* model
   (`GetStaffingGap`) — never its write model.

The important part is the direction: consumption of a read surface, not
invocation of a command. That means `fulfillment-execution` can change its
dispatch model entirely — from push to pull, from leases to reservations —
without a single line changing here.

## Why "PathUnderstaffed" is a flag, not a decision

The same boundary logic applies one level up. When active assignments on a path
fall below its committed `plannedHeads`, this context raises
`PathUnderstaffed`. It does **not**:

- pick a victim path to pull people from;
- rank associates by certification breadth;
- write a `LaborAssignment` on anyone's behalf.

Rebalancing depends on things this context cannot see — which paths are
actually blocked, who is mid-tote, what the shift manager promised the pack
line ten minutes ago. Surfacing the gap is a fact. Choosing the response is a
judgement. This service ships the fact and stops.

## The honest cost

Two costs, stated plainly:

- **You cannot answer "what is Alice doing right now?" from this service.** You
  can answer "which path is Alice on?" To get the task, you join against
  `fulfillment-execution`. That is a real ergonomic cost, accepted knowingly.
- **The staffing gap is eventually consistent with reality**, because it is
  derived from assignments a human recorded, not from physical presence.

Both are cheaper than the alternative: one aggregate whose consistency boundary
has to span a shift-length planning decision *and* a second-granularity
dispatch decision.
