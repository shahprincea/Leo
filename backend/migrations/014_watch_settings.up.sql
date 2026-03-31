CREATE TABLE watch_settings (
  wearer_id        UUID PRIMARY KEY REFERENCES wearers(id) ON DELETE CASCADE,
  night_mode_start TIME NOT NULL DEFAULT '22:00',
  night_mode_end   TIME NOT NULL DEFAULT '07:00',
  timezone         TEXT NOT NULL DEFAULT 'UTC'
);
