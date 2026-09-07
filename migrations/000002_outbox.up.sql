-- Transactional outbox (ADR 0016). One row per already-encoded Kafka
-- message: a single domain event fans out into one row per topic
-- (warehouse.workforce.events for the integration contract,
-- warehouse.workforce.analytics for the analytics data product), so a single
-- in-process relay drains both streams from this one table.
CREATE TABLE outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    topic        TEXT        NOT NULL,
    event_type   TEXT        NOT NULL,
    key          BYTEA,
    value        BYTEA       NOT NULL,
    headers      JSONB       NOT NULL DEFAULT '[]',   -- [{"key":..,"value":..}] (W3C trace headers)
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    last_error   TEXT
);

-- The relay only ever scans the unpublished tail; keep that scan tiny.
CREATE INDEX idx_outbox_events_unpublished ON outbox_events (id) WHERE published_at IS NULL;
