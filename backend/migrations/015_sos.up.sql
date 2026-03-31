CREATE TYPE sos_status AS ENUM ('active', 'cancelled', 'resolved');

CREATE TABLE sos_events (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    wearer_id    UUID        NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
    status       sos_status  NOT NULL DEFAULT 'active',
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    cancelled_at TIMESTAMPTZ,
    resolved_at  TIMESTAMPTZ
);

-- Per-wearer SOS settings (auto-911, etc.)
CREATE TABLE sos_settings (
    wearer_id UUID    PRIMARY KEY REFERENCES wearers(id) ON DELETE CASCADE,
    auto_911  BOOLEAN NOT NULL DEFAULT false
);

-- Audit trail: one row per call attempt within an SOS escalation chain.
CREATE TABLE sos_escalation_log (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    sos_event_id  UUID        NOT NULL REFERENCES sos_events(id) ON DELETE CASCADE,
    tier          INT         NOT NULL,
    contact_phone TEXT        NOT NULL,
    called_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    answered_at   TIMESTAMPTZ
);

CREATE INDEX idx_sos_events_wearer ON sos_events(wearer_id, triggered_at DESC);
CREATE INDEX idx_sos_events_active  ON sos_events(status) WHERE status = 'active';
CREATE INDEX idx_sos_escalation_sos ON sos_escalation_log(sos_event_id, tier);
