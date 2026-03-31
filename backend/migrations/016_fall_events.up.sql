-- Track every detected fall for audit, analytics, and false-alarm review.
CREATE TABLE fall_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    wearer_id    UUID        NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
    fall_type    TEXT        NOT NULL CHECK (fall_type IN ('hard', 'soft')),
    -- detected  = waiting for confirmation window (10-sec countdown)
    -- confirmed = SOS was triggered (user tapped CALL NOW or countdown expired)
    -- false_alarm = user tapped I'm OK
    status       TEXT        NOT NULL DEFAULT 'detected'
                             CHECK (status IN ('detected', 'confirmed', 'false_alarm')),
    sos_event_id UUID        REFERENCES sos_events(id),
    detected_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

-- Add fall context to SOS events.
-- triggered_by: 'manual' | 'fall' | 'wellness'
-- fall_event_id: UUID stored as text (no FK to avoid circular dependency)
ALTER TABLE sos_events
    ADD COLUMN triggered_by  TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN fall_event_id TEXT;

CREATE INDEX idx_fall_events_wearer  ON fall_events(wearer_id, detected_at DESC);
CREATE INDEX idx_fall_events_pending ON fall_events(wearer_id, status) WHERE status = 'detected';
