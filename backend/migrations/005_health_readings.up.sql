CREATE TABLE health_readings (
  id          UUID NOT NULL DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  type        TEXT NOT NULL,
  value       JSONB NOT NULL,
  recorded_at TIMESTAMPTZ NOT NULL,
  synced_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE health_readings_2026_01 PARTITION OF health_readings
  FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');

CREATE TABLE health_readings_2026_02 PARTITION OF health_readings
  FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');

CREATE TABLE health_readings_2026_03 PARTITION OF health_readings
  FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE health_readings_2026_04 PARTITION OF health_readings
  FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE TABLE health_readings_2026_05 PARTITION OF health_readings
  FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');

CREATE TABLE health_readings_2026_06 PARTITION OF health_readings
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

CREATE TABLE health_readings_2026_07 PARTITION OF health_readings
  FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE TABLE health_readings_2026_08 PARTITION OF health_readings
  FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE TABLE health_readings_2026_09 PARTITION OF health_readings
  FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');

CREATE TABLE health_readings_2026_10 PARTITION OF health_readings
  FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');

CREATE TABLE health_readings_2026_11 PARTITION OF health_readings
  FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');

CREATE TABLE health_readings_2026_12 PARTITION OF health_readings
  FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

CREATE INDEX idx_health_readings_wearer_type_time
  ON health_readings(wearer_id, type, recorded_at DESC);
