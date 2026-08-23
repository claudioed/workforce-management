---
id: 0003-certification-gated-single-active-assignment
title: 0003. Certification-gated assignment with exactly one active assignment per associate
sidebar_label: 0003. Certification gate + single active assignment
sidebar_position: 4
description: Two invariants, one keyed by aggregate identity so it cannot be violated at all.
---

# 0003. Certification-gated assignment with exactly one active assignment per associate

## Status

Accepted. Established with the initial implementation of
`internal/domain/assignment`.

## Context

Two rules govern putting an associate on a path.

**Exactly one ACTIVE assignment per associate.** A person cannot staff two paths
at once. If this leaks, every headcount number downstream silently inflates, and
the staffing gap — the primary operational output of this service — becomes
wrong in a way nobody notices until a path is short-staffed in reality but
looks fine on screen.

**An assignment requires the path's certification.** Putting an untrained
associate on a path is a safety and quality problem, not a data-quality one.
Training is itself a path that consumes hours, and it should not need a special
case.

The modelling question for the first rule is where it is enforced. The obvious
shape — one aggregate per assignment, keyed by an `AssignmentId` — makes "exactly
one active per associate" a **cross-aggregate** rule. Enforcing it then means
querying for the associate's other active assignments before writing, and hoping
nothing races between the query and the write. That is a rule enforced by
diligence, at a consistency boundary that does not contain it.

There is also a behavioural question. When an associate who already has an
active assignment is assigned to a new path, does the call **reject** or
**supersede**? Both preserve the invariant. `TASKS.md` explicitly left it open:
"pick one behavior and document it in the README; either is defensible, but the
double-booking invariant must hold."

For the second rule, the question is where "which certification does path X
require?" comes from. A `PathRequirements` port would be the general answer, but
this context's specification has no such port and no such concept — introducing
one would add a cross-aggregate lookup for a single string comparison.

## Decision

**Key the `LaborAssignment` aggregate by `AssociateId`, and give it a single
optional active interval.**

```go
type LaborAssignment struct {
    associateId shared.AssociateId
    active      *Interval   // ← there is only one of these
    history     []Interval
    events      []shared.DomainEvent
}
```

From the package doc comment:

> The aggregate root is keyed by associate so that "exactly one ACTIVE
> assignment per associate" is a structural invariant — there is only ever one
> active-interval field to hold it in.

The invariant is not checked. It is **unrepresentable if violated**: there is no
second field to put a second active assignment in, so there is no code path,
no race and no repair script that can produce a double-booking.

**Assignment supersedes rather than rejects.** `Assign` on an aggregate with an
active interval closes the old one — logging its hours against the
`AssociateShift` — and opens the new one, raising `LaborReassigned` instead of
`LaborAssigned`. This matches the floor: a supervisor moves someone, they do not
first "unassign" them.

**The certification gate is checked in the domain**, before any state changes:

```go
if !hasCertification {
    return ErrCertificationRequired
}
```

The aggregate takes a `hasCertification bool` rather than the certification set,
so it depends on the *answer*, not on how the answer was obtained.

**A path's required certification is, by convention, the `Certification` with
the same name as the `PathId`.** Path `pack` requires certification `pack`. This
is a documented naming convention, not a modelled relationship.

## Consequences

**Easier**

- The double-booking invariant needs no locking, no unique index, no
  read-then-write, and no test for a race that cannot occur.
- `AssignLabor` reads as one linear sequence with no conflict branch.
- Supersede-on-assign means the API matches how rebalancing actually happens;
  callers never need a paired "unassign" call.
- `LaborReassigned` carries `fromPathId` and `toPathId`, so a move is a single
  event rather than a close/open pair a consumer has to correlate.
- The certification gate needs no port, no cross-context call and no cache.

**Harder**

- **Assignment history lives inside the associate's aggregate**, so it grows for
  the life of the record. Fine at shift scope; it would need revisiting if this
  aggregate ever spanned weeks.
- **You cannot address an assignment by its own identity.** There is no
  `GET /assignments/{id}`. Assignments are addressed via the associate.
- **Supersede hides accidents.** Assigning an already-assigned associate
  succeeds. A client that meant to catch a conflict gets a `201` and a
  `LaborReassigned` event instead of an error. This is the accepted cost of
  matching floor behaviour, and it is documented in the README, the OpenAPI
  description for `assignLabor`, and the operation's own docs page.
- **The path-to-certification naming convention is invisible in the type
  system.** Renaming a path to something with no matching certification breaks
  assignment at runtime, not at compile time. Mitigated by documenting it in
  three places and covering it with acceptance specs.

**Now true**

- Four failing-path tests pin both rules down:
  `assignment.TestAssign_RejectsMissingCertification`,
  `usecases.TestAssignLabor_RejectsMissingCertification`,
  `http.TestAssignLabor_RejectsMissingCertification`, and
  `assignment.TestAssign_SecondAssignmentEndsPriorAndRaisesReassigned`.
- A future refactor to per-assignment identity would silently downgrade a
  structural invariant to a hopeful query. This ADR exists to make that visible
  before it happens.
