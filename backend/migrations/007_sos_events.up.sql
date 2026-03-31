CREATE TABLE sos_events (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id       UUID NOT NULL REFERENCES wearers(id),
  triggered_by    TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  lat             DOUBLE PRECISION,
  lng             DOUBLE PRECISION,
  heart_rate      INT,
  spo2            INT,
  bp_systolic     INT,
  bp_diastolic    INT,
  steps_today     INT,
  battery_level   INT,
  fall_detected   BOOLEAN NOT NULL DEFAULT FALSE,
  triggered_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at     TIMESTAMPTZ
);

CREATE TABLE sos_escalation_log (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sos_event_id    UUID NOT NULL REFERENCES sos_events(id),
  contact_id      UUID REFERENCES emergency_contacts(id),
  phone           TEXT NOT NULL,
  tier            INT NOT NULL,
  twilio_call_sid TEXT,
  status          TEXT NOT NULL,
  called_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  answered_at     TIMESTAMPTZ
);

CREATE TABLE fall_events (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  sos_event_id  UUID REFERENCES sos_events(id),
  severity      TEXT NOT NULL,
  g_force       FLOAT,
  confirmed     BOOLEAN,
  detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sos_events_wearer ON sos_events(wearer_id, triggered_at DESC);
