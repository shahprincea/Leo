CREATE TABLE locations (
  id          UUID NOT NULL DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  lat         DOUBLE PRECISION NOT NULL,
  lng         DOUBLE PRECISION NOT NULL,
  accuracy_m  FLOAT,
  recorded_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE locations_2026_01 PARTITION OF locations
  FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');

CREATE TABLE locations_2026_02 PARTITION OF locations
  FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');

CREATE TABLE locations_2026_03 PARTITION OF locations
  FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE locations_2026_04 PARTITION OF locations
  FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE TABLE locations_2026_05 PARTITION OF locations
  FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

CREATE TABLE locations_2026_06 PARTITION OF locations
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

CREATE TABLE locations_2026_07 PARTITION OF locations
  FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE TABLE locations_2026_08 PARTITION OF locations
  FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE TABLE locations_2026_09 PARTITION OF locations
  FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

CREATE TABLE locations_2026_10 PARTITION OF locations
  FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');

CREATE TABLE locations_2026_11 PARTITION OF locations
  FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');

CREATE TABLE locations_2026_12 PARTITION OF locations
  FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

CREATE INDEX idx_locations_wearer_time ON locations(wearer_id, recorded_at DESC);

CREATE TABLE geofences (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id    UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  label        TEXT NOT NULL DEFAULT 'Home',
  center_lat   DOUBLE PRECISION NOT NULL,
  center_lng   DOUBLE PRECISION NOT NULL,
  radius_m     INT NOT NULL DEFAULT 200,
  safe_start   TIME,
  safe_end     TIME,
  timezone     TEXT NOT NULL DEFAULT 'UTC',
  is_active    BOOLEAN NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
