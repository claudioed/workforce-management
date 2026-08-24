---
id: index
slug: /adr
title: Architecture Decision Records
sidebar_label: About ADRs
sidebar_position: 1
description: Why these records exist, the template they follow, and how to propose a new one.
---

# Architecture Decision Records

An **Architecture Decision Record** captures one architecturally significant
decision: the forces that produced it, the decision itself, and what it costs.
Not a design document, not a spec — a record of a choice, written once, never
edited after acceptance.

## Why they exist

Code shows *what* was built. Git history shows *when*. Neither reliably shows
*why*, and "why" is what determines whether a future change is safe.

Two examples from this repo where the reasoning is genuinely non-obvious from
the code:

- `LaborAssignment` is keyed by `AssociateId`, not by an `AssignmentId`. Reading
  the aggregate, that looks like a modelling shortcut. It is the mechanism that
  makes "exactly one active assignment per associate" structurally
  impossible to violate. Without
  [ADR 0003](./0003-certification-gated-single-active-assignment.md), a future
  refactor to "proper" per-assignment identity would silently downgrade a hard
  invariant into a hopeful query.
- There is no `taskId` anywhere in this service. That reads as incompleteness.
  It is [ADR 0002](./0002-stop-at-the-path-boundary.md), and it is the most
  consequential decision in the codebase.

## The template

These records use [Michael Nygard's
format](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions),
the de facto standard: one Markdown file per decision, numbered `0001-`,
`0002-`, …, with five sections.

```markdown
# NNNN. Short noun phrase naming the decision

## Status
Proposed | Accepted | Deprecated | Superseded by [ADR-NNNN](./nnnn-....md)

## Context
The forces at play: technical, organisational, domain. Written in
value-neutral language — describe the situation, do not argue for the
outcome yet.

## Decision
"We will ..." — active voice, one clear choice.

## Consequences
What becomes easier, what becomes harder, what is now true that was not
before. Include the costs. An ADR with only benefits is marketing.
```

Records are **immutable once accepted**. A decision that no longer holds is not
edited — it is marked `Superseded by ADR-NNNN`, and the new record explains what
changed.

## Proposing a new one

1. Copy the template above into `docs/docs/adr/NNNN-short-title.md`, taking the
   next free number.
2. Write it with status `Proposed`.
3. Add it to the `Architecture Decision Records` category in
   `docs/sidebars.ts`.
4. Open a pull request. The discussion happens on the PR, not in the document.
5. On merge, flip the status to `Accepted`.

If a decision turns out to be wrong, write a new ADR that supersedes it. Do not
quietly delete the old one — the fact that it was tried is itself information.

## The records

| # | Title | Status |
| --- | --- | --- |
| [0001](./0001-hexagonal-ports-and-adapters.md) | Hexagonal (ports and adapters) architecture | Accepted |
| [0002](./0002-stop-at-the-path-boundary.md) | Stop at the path boundary — no associate-to-task link | Accepted |
| [0003](./0003-certification-gated-single-active-assignment.md) | Certification-gated assignment with exactly one active assignment per associate | Accepted |
| [0004](./0004-kafka-integration-events-and-cloudevents-catalog.md) | Kafka for integration events, with a CloudEvents catalog ahead of the wire format | Accepted |
| [0005](./0005-rfc-7807-problem-details.md) | RFC 7807 Problem Details for every error response | Accepted |
| [0006](./0006-godog-bdd-acceptance-tests.md) | godog acceptance specs driven through the real HTTP surface | Accepted |
| [0007](./0007-arch-go-architecture-fitness-tests.md) | arch-go fitness tests to make the layering rule executable | Accepted |
| [0008](./0008-mcp-inbound-adapter.md) | Model Context Protocol as an inbound adapter, not a new service | Accepted |
