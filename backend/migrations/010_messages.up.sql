CREATE TABLE messages (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  direction   TEXT NOT NULL,
  sender_id   UUID REFERENCES users(id),
  body        TEXT NOT NULL,
  sent_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  read_at     TIMESTAMPTZ
);

CREATE INDEX idx_messages_wearer ON messages(wearer_id, sent_at DESC);
