ALTER TABLE sos_events DROP COLUMN IF EXISTS fall_event_id;
ALTER TABLE sos_events DROP COLUMN IF EXISTS triggered_by;
DROP TABLE IF EXISTS fall_events;
