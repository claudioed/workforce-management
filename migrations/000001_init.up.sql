CREATE TABLE associate_shift (
    associate_id     TEXT PRIMARY KEY,
    certifications   TEXT[] NOT NULL DEFAULT '{}',
    on_break         BOOLEAN NOT NULL DEFAULT FALSE,
    hours_logged     DOUBLE PRECISION NOT NULL DEFAULT 0,
    ended            BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE shift_plan (
    building_id TEXT NOT NULL,
    shift_id    TEXT NOT NULL,
    PRIMARY KEY (building_id, shift_id)
);

CREATE TABLE path_plan (
    building_id     TEXT NOT NULL,
    shift_id        TEXT NOT NULL,
    path_id         TEXT NOT NULL,
    planned_heads   INTEGER NOT NULL,
    planned_rate    DOUBLE PRECISION NOT NULL,
    planned_hours   DOUBLE PRECISION NOT NULL,
    PRIMARY KEY (building_id, shift_id, path_id),
    FOREIGN KEY (building_id, shift_id) REFERENCES shift_plan (building_id, shift_id) ON DELETE CASCADE
);

CREATE TABLE labor_assignment (
    associate_id TEXT PRIMARY KEY,
    active_path_id TEXT,
    active_start    TIMESTAMPTZ
);

CREATE TABLE labor_assignment_history (
    id              BIGSERIAL PRIMARY KEY,
    associate_id    TEXT NOT NULL REFERENCES labor_assignment (associate_id) ON DELETE CASCADE,
    path_id         TEXT NOT NULL,
    interval_start  TIMESTAMPTZ NOT NULL,
    interval_end    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_labor_assignment_active_path ON labor_assignment (active_path_id);

CREATE TABLE domain_event (
    id           BIGSERIAL PRIMARY KEY,
    event_name   TEXT NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    payload      JSONB NOT NULL
);
