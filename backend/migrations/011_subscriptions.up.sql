CREATE TABLE subscriptions (
  id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                UUID NOT NULL REFERENCES users(id),
  wearer_id              UUID NOT NULL REFERENCES wearers(id),
  stripe_customer_id     TEXT NOT NULL,
  stripe_subscription_id TEXT UNIQUE,
  plan                   TEXT NOT NULL,
  status                 TEXT NOT NULL,
  trial_ends_at          TIMESTAMPTZ,
  current_period_end     TIMESTAMPTZ,
  canceled_at            TIMESTAMPTZ,
  created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscriptions_wearer ON subscriptions(wearer_id);
