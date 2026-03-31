CREATE TABLE wearers (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_user_id     UUID NOT NULL REFERENCES users(id),
  full_name         TEXT NOT NULL,
  date_of_birth     DATE,
  photo_url         TEXT,
  pin_hash          TEXT NOT NULL,
  blood_type        TEXT,
  medical_conditions TEXT[],
  allergies         TEXT[],
  notes             TEXT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at        TIMESTAMPTZ
);

CREATE TABLE wearer_members (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'member',
  can_view_location BOOLEAN NOT NULL DEFAULT TRUE,
  invited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  accepted_at TIMESTAMPTZ,
  UNIQUE (wearer_id, user_id)
);
