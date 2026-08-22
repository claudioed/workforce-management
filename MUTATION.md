# Mutation Testing — internal/domain

Tool: [gremlins](https://github.com/go-gremlins/gremlins) v0.6.0, scoped to
`internal/domain/...` only (aggregate invariants — the highest-signal area for
mutation testing in this codebase).

## Command

```sh
gremlins unleash ./internal/domain --timeout-coefficient 30 --workers 2
```

Note on flags: `gremlins unleash ./internal/domain/...` (the `...` wildcard
suggested by QUALITY.md, mirroring `go test` package syntax) yields "No
results to report" — gremlins expects a plain directory path, not a Go
package pattern, so the trailing `/...` must be dropped. The default timeout
coefficient was also too tight for this machine: every mutant reported `TIMED
OUT` rather than `KILLED`/`LIVED` at the default coefficient (and still
mostly timed out at `--timeout-coefficient 10`); `--timeout-coefficient 30
--workers 2` eliminated all timeouts and produced real kill/live verdicts.

## Final results

```
Mutation testing completed in 7 seconds 42 milliseconds
Killed: 23, Lived: 0, Not covered: 0
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 100.00%
Mutator coverage: 100.00%
```

23/23 mutants generated across `internal/domain/assignment`,
`internal/domain/associate`, `internal/domain/shared`, and
`internal/domain/shiftplan` were killed by the existing (and newly added,
see Stage 2) unit test suite. Mutator coverage is 100% — every mutation
point gremlins can generate in this package tree is exercised by at least
one test.

## Survived mutants triaged

The run above is the *final* state, after triage. The first run (same
command) surfaced exactly one LIVED mutant:

- **`shiftplan/shift_plan.go:71:24` — `CONDITIONALS_BOUNDARY`** on
  `if line.PlannedHeads > installed`, mutated to `>=`. This survived because
  no test exercised the exact-equal boundary
  (`plannedHeads == installedStations`) — the existing tests only checked
  `plannedHeads < installed` (accepted) and `plannedHeads > installed`
  (rejected). This is a genuine gap, not an equivalent mutant: CLAUDE.md's
  invariant is explicitly `plannedHeads(path) ≤ installedStations(path)`, so
  the boundary case (`==`) must be *accepted*, and QUALITY.md's Stage 2
  spec calls out exactly this boundary
  (`plannedHeads == installedStations`) as required coverage. Fixed by
  adding `TestCommitShiftPlan_AllowsPlannedHeadsExactlyEqualToInstalledStations`
  to `internal/domain/shiftplan/shift_plan_test.go`, asserting a plan with
  `PlannedHeads == installed` commits successfully. Re-running gremlins
  after this addition kills the mutant (see final results above).

No other mutants required triage — the re-run reports 0 LIVED, so there is
nothing further to chase or document as an accepted equivalent mutant.
