---
id: index
slug: /ecosystem/
title: Ecosystem
sidebar_label: Overview
sidebar_position: 1
description: Where this service sits among the five warehouse-systems bounded contexts.
---

# Ecosystem

`workforce-management` is one of **five** Go services in the
`warehouse-systems` platform. Each is a bounded context with its own model, its
own database and its own deployment.

| Page | Contents |
| --- | --- |
| [Context map](./context-map.md) | The Mermaid diagram — what is actually wired, and what is only strategically related |
| [The five services](./siblings.md) | What each sibling owns, in its own vocabulary |
| [Integration](./integration.md) | Topics, envelopes, env vars, and how to smoke-test the edge |

## The one-paragraph version

This service **publishes** `ShiftPlanCommitted` to
`warehouse.workforce.events`, one message per `PathPlan` line. One sibling
consumes it: `wes-work-planning`, which projects it into a read model called
`LaborPlanObserved`, keyed by `path_id`. That is the **entire** live
cross-service surface — this service consumes nothing from anyone, and calls
nobody synchronously.

Most notably it has **no** integration with `fulfillment-execution`, which is a
design decision rather than a gap. See
[the path boundary](../business-context/path-boundary.md).
