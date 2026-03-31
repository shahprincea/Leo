CREATE TABLE preset_messages (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  key       TEXT NOT NULL,
  label     TEXT NOT NULL,
  position  INT NOT NULL DEFAULT 0,
  UNIQUE (wearer_id, key)
);

CREATE INDEX idx_preset_messages_wearer ON preset_messages(wearer_id, position);
