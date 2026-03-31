CREATE TABLE watches (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  device_id     TEXT UNIQUE NOT NULL,
  model         TEXT NOT NULL,
  os_version    TEXT,
  carrier       TEXT,
  is_samsung    BOOLEAN NOT NULL DEFAULT FALSE,
  config_hash   TEXT,
  last_seen_at  TIMESTAMPTZ,
  battery_level INT,
  registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deactivated_at TIMESTAMPTZ
);

CREATE INDEX idx_watches_wearer_id ON watches(wearer_id);
