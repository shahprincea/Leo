CREATE TABLE wellness_settings (
  wearer_id         UUID PRIMARY KEY REFERENCES wearers(id) ON DELETE CASCADE,
  prompt_time       TIME NOT NULL DEFAULT '08:00',
  alert_time        TIME NOT NULL DEFAULT '09:00',
  timezone          TEXT NOT NULL DEFAULT 'UTC'
);

CREATE TABLE wellness_checkins (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id    UUID NOT NULL REFERENCES wearers(id),
  date         DATE NOT NULL,
  response     TEXT,
  responded_at TIMESTAMPTZ,
  alert_sent   BOOLEAN NOT NULL DEFAULT FALSE,
  UNIQUE (wearer_id, date)
);
