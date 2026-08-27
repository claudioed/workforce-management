-- Workforce Labor Utilization & Staffing analytics read model (ADR-0010).
--
-- This is the ANALYTICAL database, separate from the OLTP database. It is
-- written only by cmd/workforce-projector and read (read-only) by
-- cmd/workforce-reports. The tables here are projections derived from the
-- analytics event stream, not sources of truth.

-- Idempotency + freshness: every applied analytics event id is recorded here
-- exactly once. applied_at is wall-clock insert time; occurred_at is the
-- event's business time, used to compute the projection's freshness lag.
CREATE TABLE analytics_processed_events (
    event_id    TEXT PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_analytics_processed_events_occurred_at
    ON analytics_processed_events (occurred_at DESC);

-- Consumer-level dedupe set, used by the inbound consumer's
-- ports.ProcessedEvents gate. It is kept SEPARATE from
-- analytics_processed_events (which the projection UPSERT claims) so the two
-- idempotency layers do not race to claim the same event_id: the consumer gate
-- admits the event, the projection then records its effect.
CREATE TABLE analytics_consumed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Pending breaks: the start time of a break not yet ended, so a later
-- AssociateBreakEnded can derive the break duration. Keyed by associate, since
-- an associate has at most one open break at a time (a break cannot start while
-- on break — OLTP invariant).
CREATE TABLE analytics_pending_breaks (
    associate_id TEXT PRIMARY KEY,
    started_at   TIMESTAMPTZ NOT NULL
);

-- The labor rollup fact table: one row per (path_id, hour_bucket). Counters and
-- the running break-duration sum are UPSERTed as events arrive; the average
-- break duration is derived at read time from the two break_* columns.
--
-- Associate-scoped events (shift start/end, breaks, certifications) do not
-- carry a path — this context stops at the path boundary — and land under the
-- empty path_id '' (a building-wide bucket). Path-scoped events (labor
-- assigned/reassigned, understaffed) land under their path_id.
CREATE TABLE labor_rollup (
    path_id              TEXT NOT NULL,
    hour_bucket          TIMESTAMPTZ NOT NULL,
    shifts_started       BIGINT NOT NULL DEFAULT 0,
    shifts_ended         BIGINT NOT NULL DEFAULT 0,
    breaks               BIGINT NOT NULL DEFAULT 0,
    certifications       BIGINT NOT NULL DEFAULT 0,
    labor_assigned       BIGINT NOT NULL DEFAULT 0,
    labor_reassigned     BIGINT NOT NULL DEFAULT 0,
    understaffing_events BIGINT NOT NULL DEFAULT 0,
    break_seconds        DOUBLE PRECISION NOT NULL DEFAULT 0,
    breaks_with_duration BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (path_id, hour_bucket)
);

CREATE INDEX idx_labor_rollup_hour_bucket
    ON labor_rollup (hour_bucket);
