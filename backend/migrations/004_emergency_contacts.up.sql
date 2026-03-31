CREATE TABLE emergency_contacts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id  UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  user_id    UUID REFERENCES users(id),
  full_name  TEXT NOT NULL,
  phone      TEXT NOT NULL,
  tier       INT NOT NULL,
  timeout_sec INT NOT NULL DEFAULT 20,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_emergency_contacts_wearer_tier ON emergency_contacts(wearer_id, tier);

CREATE TABLE oncall_schedules (
  id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  emergency_contact_id UUID NOT NULL REFERENCES emergency_contacts(id) ON DELETE CASCADE,
  day_of_week          INT[],
  start_time           TIME NOT NULL,
  end_time             TIME NOT NULL,
  timezone             TEXT NOT NULL DEFAULT 'UTC'
);
