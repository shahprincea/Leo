CREATE TABLE medications (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  dosage      TEXT NOT NULL,
  notes       TEXT,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE medication_schedules (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  medication_id UUID NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
  time_of_day   TIME NOT NULL,
  days_of_week  INT[],
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE medication_confirmations (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  medication_id UUID NOT NULL REFERENCES medications(id),
  schedule_id   UUID NOT NULL REFERENCES medication_schedules(id),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  due_at        TIMESTAMPTZ NOT NULL,
  confirmed_at  TIMESTAMPTZ,
  missed_alert_sent BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_med_confirmations_due ON medication_confirmations(wearer_id, due_at);
