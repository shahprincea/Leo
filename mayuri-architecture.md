# Mayuri — Technical Architecture

---

## 1. System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        MAYURI SYSTEM                                │
│                                                                     │
│  ┌──────────────┐        ┌──────────────────────────────────────┐  │
│  │  Wear OS App │◄──────►│           AWS Backend                │  │
│  │  (Kotlin)    │  REST/ │                                      │  │
│  │              │  WS    │  ┌────────────┐  ┌────────────────┐  │  │
│  │  - Watch Face│        │  │SOS Routing │  │ Health Data    │  │  │
│  │  - Fall Det. │        │  │  Engine    │  │   Service      │  │  │
│  │  - Health Mon│        │  └─────┬──────┘  └───────┬────────┘  │  │
│  │  - GPS       │        │        │                 │           │  │
│  │  - Calling   │        │  ┌─────▼──────┐  ┌───────▼────────┐  │  │
│  │  - Messaging │        │  │Notification│  │ Location       │  │  │
│  │  - Med Remind│        │  │  Service   │  │   Service      │  │  │
│  │  - Offline   │        │  └─────┬──────┘  └───────┬────────┘  │  │
│  │    Buffer    │        │        │                 │           │  │
│  └──────────────┘        │  ┌─────▼──────┐  ┌───────▼────────┐  │  │
│                          │  │  Device    │  │  Medication    │  │  │
│  ┌──────────────┐        │  │  Mgmt Svc  │  │    Service     │  │  │
│  │  Flutter App │◄──────►│  └────────────┘  └────────────────┘  │  │
│  │  iOS/Android │  REST/ │                                      │  │
│  │  Web         │  WS    │  ┌────────────┐  ┌────────────────┐  │  │
│  │              │        │  │  VoIP      │  │  Wellness      │  │  │
│  │  - Dashboard │        │  │  Signaling │  │  Check-in Svc  │  │  │
│  │  - Alerts    │        │  └────────────┘  └────────────────┘  │  │
│  │  - Config    │        │                                      │  │
│  │  - Billing   │        │  ┌────────────┐  ┌────────────────┐  │  │
│  └──────────────┘        │  │  Auth Svc  │  │  Audit Log Svc │  │  │
│                          │  └────────────┘  └────────────────┘  │  │
│                          │                                      │  │
│                          │  ┌──────────┐  ┌───────┐  ┌───────┐ │  │
│                          │  │ RDS      │  │ Redis │  │  S3   │ │  │
│                          │  │PostgreSQL│  │       │  │       │ │  │
│                          │  └──────────┘  └───────┘  └───────┘ │  │
│                          └──────────────────────────────────────┘  │
│                                                                     │
│  External Services:                                                 │
│  ┌─────────┐  ┌─────────────┐  ┌────────┐  ┌──────────────────┐  │
│  │ Twilio  │  │  Firebase   │  │ Stripe │  │ Google Maps API  │  │
│  │ Voice + │  │    FCM      │  │        │  │                  │  │
│  │   SMS   │  │             │  │        │  │                  │  │
│  └─────────┘  └─────────────┘  └────────┘  └──────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 2. Backend Service Architecture

### Technology Choice: Go

All backend services are written in **Go**. Reasons:
- Low memory footprint — critical for high-frequency health data ingestion
- Excellent concurrency model for real-time SOS routing and WebSocket handling
- Fast cold starts on ECS Fargate
- Strong standard library for HTTP, crypto, and JSON

Services communicate via **REST over HTTP** internally (not event bus) for v1. Simplicity wins — avoid distributed messaging complexity until scale demands it. Redis pub/sub is used only for real-time WebSocket fan-out.

---

### SOS Routing Engine

**Responsibility:** When SOS fires, determine who to call right now and manage escalation.

- Loads on-call schedule from Redis (fast reads, schedule pre-computed on save)
- Determines current on-call contact based on day + time
- Calls Twilio Programmable Voice API to initiate call to contact's phone
- Starts escalation timer in Redis (TTL = tier timeout, default 20 sec)
- On timer expiry with no answer: calls next tier
- On all tiers exhausted: optionally calls 911 (Twilio), always sends SMS via Notification Service
- Stores SOS event with full payload to PostgreSQL
- Emits real-time SOS event to companion app via WebSocket

---

### Health Data Service

**Responsibility:** Receive, store, and serve health readings from watch.

- Accepts batch health sync payloads from watch (HR, SpO2, BP, steps)
- Writes to `health_readings` table (time-series, partitioned by month)
- Serves historical data to companion app (paginated, date-range filtered)
- Computes daily aggregates (avg HR, min SpO2, total steps) on ingestion for dashboard

---

### Notification Service

**Responsibility:** Deliver push + SMS alerts to family members.

- Receives alert events from other services (SOS, missed medication, geofence exit, low battery, etc.)
- Sends FCM push notification to all eligible family members
- Starts 30-second acknowledgment timer per notification
- If not opened in 30 sec: fires Twilio SMS fallback
- SMS body: safe summary only (name, event type, Google Maps link — no raw health data)
- Full health payload: push notification only

---

### Location Service

**Responsibility:** Store GPS history, evaluate geofences, emit wandering alerts.

- Receives location pings from watch (every 30 sec when outside geofence, every 5 min inside)
- Writes to `locations` table
- On every write: evaluates active geofences for wearer
- Geofence exit: emits event to Notification Service immediately
- Applies safe zone hours rule: nighttime exit → high-priority alert
- 30-day rolling retention: cron job purges records older than 30 days nightly
- Serves last known location + timestamp to companion app

---

### VoIP Signaling Service

**Responsibility:** WebRTC signaling for check-in calls between companion app and watch.

- REST endpoint to initiate call session (companion app → watch or watch → companion app)
- WebSocket channel for ICE candidate exchange and SDP negotiation
- TURN server: AWS-hosted coturn instance (handles NAT traversal for LTE watch)
- Session cleanup on call end or 60-second timeout with no answer

---

### Medication Service

**Responsibility:** Manage medication schedules, track confirmations, detect missed doses.

- Stores medication schedules per wearer
- Pushes active schedule to Device Management Service on any schedule change
- Receives confirmation events from watch (medication taken, timestamp)
- Cron job runs every minute: checks for confirmations due > 15 min ago without confirmation
- Missed dose: emits event to Notification Service

---

### Device Management Service

**Responsibility:** Watch registration, remote configuration, config sync.

- Registers watch on first boot (device ID, model, OS version, carrier)
- Stores full watch config: geofence, medication schedule, escalation contacts, preset messages, emergency medical ID, watch face settings
- Config version hash: watch polls on reconnect, only downloads if hash changed
- Remote setup: companion app pushes config; watch pulls on first boot automatically
- Detects watch removal (heartbeat timeout > 5 min during daytime → alert)
- Battery level ingested with every health sync

---

### Wellness Check-in Service

**Responsibility:** Schedule morning prompts, process responses, alert on no-response.

- Per-wearer: stores prompt time (default 8am) and no-response alert time (default 9am)
- Cron job at each minute: push prompt commands to watches whose prompt time has arrived
- Receives emoji response from watch via REST
- Stores response in `wellness_checkins` table
- Cron job at 9am: for any wearer with no response today → emit alert to Notification Service

---

### Auth Service

**Responsibility:** Authentication and authorization for companion app and watch.

- Email/password login + Google/Apple OAuth
- Issues JWT access token (15 min TTL) + refresh token (30 day TTL, stored in Redis)
- Role-based: `admin` (full access) vs `member` (view + receive alerts only)
- Watch auth: 4-digit PIN issues a long-lived device token (30 days, rotating)
- Token revocation on logout, subscription cancellation, or device removal

---

### Audit Log Service

**Responsibility:** HIPAA-required immutable audit trail of all PHI access.

- Every service calls Audit Log Service before returning PHI data
- Log entry: user ID, action, resource type, resource ID, timestamp, IP address
- Written to append-only PostgreSQL table + streamed to S3 (compressed, 7-year retention)
- Not queryable by application users — accessible only for compliance review

---

## 3. Database Schema

```sql
-- ─────────────────────────────────────────
-- USERS & IDENTITY
-- ─────────────────────────────────────────

CREATE TABLE users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email         TEXT UNIQUE NOT NULL,
  password_hash TEXT,
  full_name     TEXT NOT NULL,
  phone         TEXT,
  avatar_url    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at    TIMESTAMPTZ
);

CREATE TABLE oauth_identities (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider    TEXT NOT NULL, -- 'google' | 'apple'
  provider_id TEXT NOT NULL,
  UNIQUE (provider, provider_id)
);

-- ─────────────────────────────────────────
-- WEARERS (ELDERLY PROFILES)
-- ─────────────────────────────────────────

CREATE TABLE wearers (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_user_id     UUID NOT NULL REFERENCES users(id),
  full_name         TEXT NOT NULL,
  date_of_birth     DATE,
  photo_url         TEXT,
  pin_hash          TEXT NOT NULL, -- 4-digit PIN for watch login
  blood_type        TEXT,
  medical_conditions TEXT[],
  allergies         TEXT[],
  notes             TEXT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at        TIMESTAMPTZ
);

-- Family members with access to a wearer
CREATE TABLE wearer_members (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'member', -- 'admin' | 'member'
  can_view_location BOOLEAN NOT NULL DEFAULT TRUE,
  invited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  accepted_at TIMESTAMPTZ,
  UNIQUE (wearer_id, user_id)
);

-- ─────────────────────────────────────────
-- WATCHES (DEVICE REGISTRATION)
-- ─────────────────────────────────────────

CREATE TABLE watches (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  device_id     TEXT UNIQUE NOT NULL, -- hardware identifier
  model         TEXT NOT NULL,        -- e.g. 'samsung_galaxy_watch6_lte'
  os_version    TEXT,
  carrier       TEXT,
  is_samsung    BOOLEAN NOT NULL DEFAULT FALSE, -- gates BP feature
  config_hash   TEXT,                -- hash of last synced config
  last_seen_at  TIMESTAMPTZ,
  battery_level INT,                 -- 0-100
  registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deactivated_at TIMESTAMPTZ
);

CREATE INDEX idx_watches_wearer_id ON watches(wearer_id);

-- ─────────────────────────────────────────
-- SOS ESCALATION
-- ─────────────────────────────────────────

CREATE TABLE emergency_contacts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id  UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  user_id    UUID REFERENCES users(id), -- NULL if external contact (not app user)
  full_name  TEXT NOT NULL,
  phone      TEXT NOT NULL,
  tier       INT NOT NULL,  -- 1 = primary, 2 = secondary, etc.
  timeout_sec INT NOT NULL DEFAULT 20,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_emergency_contacts_wearer_tier ON emergency_contacts(wearer_id, tier);

CREATE TABLE oncall_schedules (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  emergency_contact_id UUID NOT NULL REFERENCES emergency_contacts(id) ON DELETE CASCADE,
  day_of_week         INT[], -- 0=Sun, 1=Mon ... 6=Sat; NULL = all days
  start_time          TIME NOT NULL,
  end_time            TIME NOT NULL,
  timezone            TEXT NOT NULL DEFAULT 'UTC'
);

-- ─────────────────────────────────────────
-- HEALTH DATA
-- ─────────────────────────────────────────

-- Partitioned by month for scalability
CREATE TABLE health_readings (
  id          UUID NOT NULL DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  type        TEXT NOT NULL, -- 'heart_rate' | 'spo2' | 'blood_pressure' | 'steps'
  value       JSONB NOT NULL, -- { "bpm": 72 } | { "pct": 98 } | { "systolic": 120, "diastolic": 80 } | { "count": 3421 }
  recorded_at TIMESTAMPTZ NOT NULL,
  synced_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);

-- Create monthly partitions at deploy time, automate with pg_partman
CREATE TABLE health_readings_2026_01 PARTITION OF health_readings
  FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');

CREATE INDEX idx_health_readings_wearer_type_time
  ON health_readings(wearer_id, type, recorded_at DESC);

-- ─────────────────────────────────────────
-- LOCATION
-- ─────────────────────────────────────────

CREATE TABLE locations (
  id          UUID NOT NULL DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  lat         DOUBLE PRECISION NOT NULL,
  lng         DOUBLE PRECISION NOT NULL,
  accuracy_m  FLOAT,
  recorded_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE INDEX idx_locations_wearer_time ON locations(wearer_id, recorded_at DESC);

CREATE TABLE geofences (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id    UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  label        TEXT NOT NULL DEFAULT 'Home',
  center_lat   DOUBLE PRECISION NOT NULL,
  center_lng   DOUBLE PRECISION NOT NULL,
  radius_m     INT NOT NULL DEFAULT 200,
  safe_start   TIME, -- safe hours start (NULL = always safe)
  safe_end     TIME, -- safe hours end
  timezone     TEXT NOT NULL DEFAULT 'UTC',
  is_active    BOOLEAN NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────
-- SOS & FALL EVENTS
-- ─────────────────────────────────────────

CREATE TABLE sos_events (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id       UUID NOT NULL REFERENCES wearers(id),
  triggered_by    TEXT NOT NULL, -- 'button' | 'fall' | 'no_wellness_response'
  status          TEXT NOT NULL DEFAULT 'active', -- 'active' | 'resolved' | 'false_alarm'
  lat             DOUBLE PRECISION,
  lng             DOUBLE PRECISION,
  heart_rate      INT,
  spo2            INT,
  bp_systolic     INT,
  bp_diastolic    INT,
  steps_today     INT,
  battery_level   INT,
  fall_detected   BOOLEAN NOT NULL DEFAULT FALSE,
  triggered_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at     TIMESTAMPTZ
);

CREATE TABLE sos_escalation_log (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sos_event_id    UUID NOT NULL REFERENCES sos_events(id),
  contact_id      UUID REFERENCES emergency_contacts(id),
  phone           TEXT NOT NULL,
  tier            INT NOT NULL,
  twilio_call_sid TEXT,
  status          TEXT NOT NULL, -- 'calling' | 'answered' | 'no_answer' | 'failed'
  called_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  answered_at     TIMESTAMPTZ
);

CREATE INDEX idx_sos_events_wearer ON sos_events(wearer_id, triggered_at DESC);

CREATE TABLE fall_events (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  sos_event_id  UUID REFERENCES sos_events(id),
  severity      TEXT NOT NULL, -- 'hard' | 'soft'
  g_force       FLOAT,
  confirmed     BOOLEAN,       -- TRUE = user did not cancel, FALSE = user cancelled (false alarm)
  detected_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────
-- MEDICATIONS
-- ─────────────────────────────────────────

CREATE TABLE medications (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  dosage      TEXT NOT NULL, -- e.g. "10mg"
  notes       TEXT,
  is_active   BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE medication_schedules (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  medication_id UUID NOT NULL REFERENCES medications(id) ON DELETE CASCADE,
  time_of_day   TIME NOT NULL,
  days_of_week  INT[], -- NULL = every day
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE medication_confirmations (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  medication_id UUID NOT NULL REFERENCES medications(id),
  schedule_id   UUID NOT NULL REFERENCES medication_schedules(id),
  wearer_id     UUID NOT NULL REFERENCES wearers(id),
  due_at        TIMESTAMPTZ NOT NULL,
  confirmed_at  TIMESTAMPTZ, -- NULL = not yet confirmed / missed
  missed_alert_sent BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_med_confirmations_due ON medication_confirmations(wearer_id, due_at);

-- ─────────────────────────────────────────
-- WELLNESS CHECK-INS
-- ─────────────────────────────────────────

CREATE TABLE wellness_settings (
  wearer_id         UUID PRIMARY KEY REFERENCES wearers(id) ON DELETE CASCADE,
  prompt_time       TIME NOT NULL DEFAULT '08:00',
  alert_time        TIME NOT NULL DEFAULT '09:00',
  timezone          TEXT NOT NULL DEFAULT 'UTC'
);

CREATE TABLE wellness_checkins (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id    UUID NOT NULL REFERENCES wearers(id),
  date         DATE NOT NULL,
  response     TEXT, -- 'great' | 'okay' | 'not_well' | NULL (no response)
  responded_at TIMESTAMPTZ,
  alert_sent   BOOLEAN NOT NULL DEFAULT FALSE,
  UNIQUE (wearer_id, date)
);

-- ─────────────────────────────────────────
-- MESSAGING
-- ─────────────────────────────────────────

CREATE TABLE messages (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  wearer_id   UUID NOT NULL REFERENCES wearers(id),
  direction   TEXT NOT NULL, -- 'family_to_watch' | 'watch_to_family'
  sender_id   UUID REFERENCES users(id), -- NULL if from watch (preset)
  body        TEXT NOT NULL,             -- preset key or free text (max 80 chars)
  sent_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  read_at     TIMESTAMPTZ
);

CREATE INDEX idx_messages_wearer ON messages(wearer_id, sent_at DESC);

-- ─────────────────────────────────────────
-- SUBSCRIPTIONS
-- ─────────────────────────────────────────

CREATE TABLE subscriptions (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id               UUID NOT NULL REFERENCES users(id),
  wearer_id             UUID NOT NULL REFERENCES wearers(id),
  stripe_customer_id    TEXT NOT NULL,
  stripe_subscription_id TEXT UNIQUE,
  plan                  TEXT NOT NULL, -- 'monthly' | 'annual'
  status                TEXT NOT NULL, -- 'trialing' | 'active' | 'past_due' | 'canceled'
  trial_ends_at         TIMESTAMPTZ,
  current_period_end    TIMESTAMPTZ,
  canceled_at           TIMESTAMPTZ,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subscriptions_wearer ON subscriptions(wearer_id);

-- ─────────────────────────────────────────
-- AUDIT LOG (HIPAA)
-- ─────────────────────────────────────────

CREATE TABLE audit_logs (
  id            BIGSERIAL PRIMARY KEY,
  user_id       UUID REFERENCES users(id),
  action        TEXT NOT NULL, -- 'read_health_data' | 'read_location' | 'update_wearer' etc.
  resource_type TEXT NOT NULL,
  resource_id   UUID,
  ip_address    INET,
  user_agent    TEXT,
  occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partitioned by month, never updated or deleted (compliance)
-- Archived to S3 after 90 days, retained 7 years
CREATE INDEX idx_audit_logs_user ON audit_logs(user_id, occurred_at DESC);
CREATE INDEX idx_audit_logs_resource ON audit_logs(resource_type, resource_id);
```

---

## 4. API Contracts

Base URL: `https://api.mayuri.app/v1`

All requests require `Authorization: Bearer <jwt>` except `/auth/*` and `/watches/config` (uses device token).

---

### Auth

```
POST /auth/register
Body: { email, password, full_name, phone }
Response: { user, access_token, refresh_token }

POST /auth/login
Body: { email, password }
Response: { user, access_token, refresh_token }

POST /auth/refresh
Body: { refresh_token }
Response: { access_token, refresh_token }

POST /auth/oauth
Body: { provider: "google"|"apple", id_token }
Response: { user, access_token, refresh_token }
```

---

### Wearers

```
POST /wearers
Body: { full_name, date_of_birth, blood_type, medical_conditions, allergies, pin }
Response: { wearer }

GET /wearers/:id
Response: { wearer, watch, subscription_status }

PATCH /wearers/:id
Body: (any wearer fields)
Response: { wearer }

POST /wearers/:id/members
Body: { email, role }
Response: { invite_sent: true }

GET /wearers/:id/members
Response: { members: [{ user, role, can_view_location }] }
```

---

### Watch Device + Config Sync

```
POST /watches/register
Body: { wearer_id, device_id, model, os_version, carrier }
Response: { watch, device_token }

GET /watches/config
Header: Authorization: Bearer <device_token>
Response: {
  config_hash,
  geofences: [...],
  medications: [...],
  escalation_contacts: [...],
  preset_messages: [...],
  emergency_medical_id: {...},
  wellness_prompt_time,
  night_mode: { start, end }
}

POST /watches/health-sync
Header: Authorization: Bearer <device_token>
Body: {
  readings: [
    { type: "heart_rate", value: { bpm: 72 }, recorded_at },
    { type: "spo2", value: { pct: 98 }, recorded_at },
    { type: "steps", value: { count: 3421 }, recorded_at }
  ]
}
Response: { synced: 3 }

POST /watches/location
Header: Authorization: Bearer <device_token>
Body: { lat, lng, accuracy_m, recorded_at }
Response: { geofence_status: "inside"|"outside" }

POST /watches/battery
Header: Authorization: Bearer <device_token>
Body: { level: 18 }
Response: { ok: true }

POST /watches/removal
Header: Authorization: Bearer <device_token>
Body: { removed: true, removed_at }
Response: { ok: true }
```

---

### SOS

```
POST /sos
Header: Authorization: Bearer <device_token>
Body: {
  triggered_by: "button"|"fall",
  lat, lng,
  heart_rate, spo2, bp_systolic, bp_diastolic, steps_today,
  battery_level,
  fall_detected: true,
  fall_severity: "soft"|"hard"
}
Response: {
  sos_event_id,
  calling_contact: { name, phone, tier }
}

POST /sos/:id/cancel
Header: Authorization: Bearer <device_token>
Body: { cancelled_by: "user_button" }
Response: { ok: true }

GET /sos/:id
Response: { sos_event, escalation_log }
```

---

### Location

```
GET /wearers/:id/location/current
Response: { lat, lng, accuracy_m, recorded_at, is_stale: false }

GET /wearers/:id/location/history
Query: ?from=2026-03-01&to=2026-03-30
Response: { locations: [...] }

PUT /wearers/:id/geofence
Body: { center_lat, center_lng, radius_m, safe_start, safe_end, timezone }
Response: { geofence }
```

---

### Medications

```
POST /wearers/:id/medications
Body: { name, dosage, notes, schedules: [{ time_of_day, days_of_week }] }
Response: { medication }

GET /wearers/:id/medications
Response: { medications: [{ ...medication, schedules, compliance_today }] }

GET /wearers/:id/medications/compliance
Query: ?from=2026-03-01&to=2026-03-30
Response: { compliance: [{ date, medications: [{ name, confirmed_at, missed }] }] }

POST /medications/confirmations
Header: Authorization: Bearer <device_token>
Body: { medication_id, schedule_id, confirmed_at }
Response: { ok: true }
```

---

### Wellness

```
POST /wellness/response
Header: Authorization: Bearer <device_token>
Body: { response: "great"|"okay"|"not_well", responded_at }
Response: { ok: true }

GET /wearers/:id/wellness
Query: ?from=2026-03-01&to=2026-03-30
Response: { checkins: [{ date, response, responded_at }] }
```

---

### Messaging

```
POST /wearers/:id/messages
Body: { body }  -- max 80 chars, family → watch
Response: { message }

GET /wearers/:id/messages
Query: ?limit=50&before=<message_id>
Response: { messages: [...] }

POST /messages/preset
Header: Authorization: Bearer <device_token>
Body: { preset_key: "im_okay"|"call_me"|"coming_home" }
Response: { message }
```

---

### Escalation Configuration

```
GET /wearers/:id/escalation
Response: { contacts: [{ ...contact, schedules }] }

PUT /wearers/:id/escalation
Body: {
  contacts: [
    {
      full_name, phone, tier, timeout_sec,
      schedules: [{ day_of_week, start_time, end_time, timezone }]
    }
  ],
  auto_911: true
}
Response: { contacts }
```

---

### Subscriptions

```
POST /subscriptions
Body: { wearer_id, plan: "monthly"|"annual" }
Response: { stripe_checkout_url }

GET /subscriptions/:wearer_id
Response: { subscription }

DELETE /subscriptions/:wearer_id
Response: { canceled_at }
```

---

### WebSocket (Real-time)

```
WS /ws?token=<access_token>

Client subscribes to wearer channels on connect:
→ { type: "subscribe", wearer_id }

Server pushes events:
← { type: "health_update", wearer_id, readings: [...] }
← { type: "location_update", wearer_id, lat, lng, recorded_at }
← { type: "sos_triggered", wearer_id, sos_event }
← { type: "sos_escalation", wearer_id, sos_event_id, contact, tier }
← { type: "battery_alert", wearer_id, level }
← { type: "watch_removed", wearer_id }
← { type: "message_received", wearer_id, message }
← { type: "wellness_response", wearer_id, response }
```

---

## 5. Data Flow Diagrams

### Flow 1: Fall Detected → SOS → Escalation → SMS Fallback

```
Watch                    Backend                  Twilio       Family App
  │                         │                       │              │
  │ [fall threshold hit]     │                       │              │
  │ show confirmation UI     │                       │              │
  │ [10 sec, no cancel]      │                       │              │
  │──POST /sos──────────────►│                       │              │
  │                         │ store sos_event        │              │
  │                         │ load on-call schedule  │              │
  │                         │──initiate call────────►│              │
  │                         │                       │──call dial──►│
  │◄────{ calling_contact }─│                       │              │
  │ [show "Calling Mom..."]  │                       │              │
  │                         │ start 20sec Redis TTL  │              │
  │                         │──FCM push─────────────────────────►  │
  │                         │                       │              │ [SOS banner]
  │                         │ [timer expires]        │              │
  │                         │──escalate tier 2──────►│              │
  │                         │                       │──call dial──►│
  │                         │ [all tiers exhausted]  │              │
  │                         │──SMS via Twilio────────►│              │
  │                         │                       │──SMS sent───►│
```

---

### Flow 2: Normal Health Sync → Live Dashboard

```
Watch                    Backend               Redis          Family App
  │                         │                    │                │
  │ [every 5 min]            │                    │                │
  │──POST /watches/health-sync►                   │                │
  │                         │ write health_readings               │
  │                         │ compute daily aggregates            │
  │                         │──pub health_update─►│               │
  │◄────{ synced: N }────────│                    │               │
  │                         │                    │──WS push──────►│
  │                         │                    │               │ [dashboard updates]
```

---

### Flow 3: Remote Watch Setup → First Boot

```
Family App               Backend              Watch
  │                         │                   │
  │──POST /wearers──────────►│                   │
  │──PUT /wearers/escalation►│                   │
  │──POST /wearers/medications►                  │
  │──PUT /wearers/geofence──►│                   │
  │                         │ store config       │
  │                         │ compute config_hash│
  │◄──{ setup_complete }────│                   │
  │                         │                   │
  │   [watch shipped, powered on]               │
  │                         │◄──POST /watches/register
  │                         │   store watch      │
  │                         │◄──GET /watches/config
  │                         │──────config payload►│
  │                         │                   │ [watch fully configured]
  │◄──WS: watch_online──────│                   │
```

---

### Flow 4: Geofence Exit → Wandering Alert

```
Watch                    Backend               Notification Svc    Family App
  │                         │                       │                  │
  │──POST /watches/location─►│                       │                  │
  │                         │ evaluate geofences     │                  │
  │                         │ [outside safe zone]    │                  │
  │                         │ [current time > safe_end]                │
  │◄──{ geofence_status:    │                       │                  │
  │    "outside" }          │──emit geofence_exit───►│                  │
  │                         │                       │──FCM push────────►│
  │                         │                       │                  │ [ALERT: Dad left home]
  │                         │                       │──SMS (30s later)─►│
```

---

## 6. Infrastructure Architecture

### Compute: ECS Fargate

All backend services run as Docker containers on ECS Fargate. Reasons:
- No servers to manage
- Per-service scaling (SOS Routing Engine scales independently of Health Data Service)
- HIPAA-eligible
- Cost-effective at v1 scale

Each service is its own Fargate task definition. Services communicate via internal ALB (Application Load Balancer) within a private VPC subnet.

```
Internet → API Gateway (rate limiting, TLS termination)
                │
           Public ALB
                │
    ┌───────────┴─────────────┐
    │      Private VPC        │
    │                         │
    │  ECS Fargate Services   │
    │  (private subnet)       │
    │                         │
    │  RDS PostgreSQL         │
    │  (private subnet)       │
    │                         │
    │  ElastiCache Redis      │
    │  (private subnet)       │
    └─────────────────────────┘
```

### Database

- **RDS PostgreSQL 15** — Multi-AZ deployment (automatic failover)
- **ElastiCache Redis 7** — Multi-AZ, used for: on-call schedule cache, SOS escalation timers, WebSocket pub/sub, refresh token store

### Storage

- **S3** — Audit log archival, compressed JSON, 7-year retention
- **S3** — User avatar uploads (pre-signed URLs, private bucket)

### Networking

- VPC with public and private subnets across 2 AZs
- API Gateway handles TLS termination and rate limiting (100 req/min per IP, 1000 req/min per authenticated user)
- No direct internet access from private subnets — NAT Gateway for outbound (Twilio, FCM calls)

### HIPAA Controls

| Control | Implementation |
|---|---|
| Encryption at rest | RDS storage encrypted (AES-256), S3 SSE-S3, Redis encryption-at-rest |
| Encryption in transit | TLS 1.3 everywhere, enforce HTTPS |
| Access control | IAM roles per service (least privilege), no shared credentials |
| Audit logging | All PHI access logged to audit_logs table + S3 |
| Data retention | 30-day GPS, subscription lifetime + 90 days for health data |
| Data deletion | Full purge pipeline on user request, completed within 30 days |
| BAA agreements | AWS (account-level), Twilio (enterprise plan), Google (FCM/Firebase) |
| Monitoring | CloudWatch alarms for error rates, latency, unauthorized access attempts |

### Monitoring

- **CloudWatch** — service metrics, error rates, latency P50/P95/P99
- **CloudWatch Alarms** — page on: SOS routing failures, health sync lag > 10 min, DB connection exhaustion
- **Structured logging** — JSON logs, never log PHI fields

---

## 7. Watch App Architecture (Wear OS / Kotlin)

```
┌──────────────────────────────────────────────┐
│               Watch App                      │
│                                              │
│  ┌──────────────────────────────────────┐   │
│  │           UI Layer (Compose)         │   │
│  │  WatchFaceService │ ConfirmationUI   │   │
│  │  MessagingUI      │ MedReminderUI    │   │
│  │  WellnessUI       │ EmergencyID      │   │
│  └──────────────┬───────────────────────┘   │
│                 │                            │
│  ┌──────────────▼───────────────────────┐   │
│  │         Service Layer                │   │
│  │  FallDetectionService (foreground)   │   │
│  │  HealthMonitorService (foreground)   │   │
│  │  LocationService (foreground)        │   │
│  │  SOSService                          │   │
│  │  WearDetectionService                │   │
│  │  BatteryMonitorService               │   │
│  └──────────────┬───────────────────────┘   │
│                 │                            │
│  ┌──────────────▼───────────────────────┐   │
│  │         Data Layer                   │   │
│  │  LocalDatabase (Room/SQLite)         │   │
│  │  SyncManager                         │   │
│  │  ConfigRepository                    │   │
│  │  ApiClient (Retrofit + OkHttp)       │   │
│  └──────────────────────────────────────┘   │
└──────────────────────────────────────────────┘
```

### Background Execution Strategy

Wear OS aggressively kills background processes. To keep health monitoring always-on:

- **FallDetectionService** and **HealthMonitorService** run as Wear OS **Foreground Services** with persistent notification — system cannot kill them
- **LocationService** runs as Foreground Service when outside geofence; pauses to passive mode when inside
- **SyncManager** uses WorkManager for periodic sync (every 5 min) — survives process restarts
- **WearDetectionService** registers for Wear OS `ACTION_WEARING_STATE_CHANGED` broadcast

### Local Database (Room)

Tables: `health_readings_queue`, `location_queue`, `config_cache`, `medication_schedule_cache`, `message_queue`

All queues are FIFO. SyncManager drains queues on connectivity. Records deleted after confirmed sync.

---

## 8. Security Architecture

### Authentication Flow

```
Family App login → POST /auth/login
→ Server validates password_hash (bcrypt, cost 12)
→ Issues JWT (15 min, signed RS256) + refresh token (UUID, stored in Redis with 30-day TTL)
→ App stores access token in memory, refresh token in secure storage (Keychain/Keystore)
→ On 401: auto-refresh using refresh token
→ On refresh token expiry: force re-login
```

### Watch Authentication

- Watch registers with device_id → receives `device_token` (UUID, 30-day TTL, rotating)
- device_token stored in watch secure storage
- All watch API calls use device_token, not user JWT
- device_token rotation: new token issued on each /watches/config poll, old token invalidated after 5 min grace period

### Role-Based Access

| Action | Admin | Member |
|---|---|---|
| View health data | ✅ | ✅ |
| View live location | ✅ | Only if `can_view_location=true` |
| Configure escalation | ✅ | ❌ |
| Add medications | ✅ | ❌ |
| Invite members | ✅ | ❌ |
| Send watch messages | ✅ | ✅ |
| Manage subscription | ✅ | ❌ |

### PHI Protection

- Health data fields never appear in logs
- SMS bodies contain no health data — location link only
- Audit log captures every PHI read with user identity

---

## 9. Scalability Considerations

### Health Data Write Volume

| Scale | Wearers | Readings/min | Writes/day |
|---|---|---|---|
| v1 launch | 100 | ~100 | ~144K |
| Growth | 10,000 | ~10,000 | ~14.4M |
| Scale | 100,000 | ~100,000 | ~144M |

At 10K wearers: single RDS instance handles this comfortably (PostgreSQL handles ~50K writes/sec).

At 100K wearers: partition `health_readings` by month (already in schema) + read replicas for dashboard queries + consider TimescaleDB extension for time-series compression.

### Location Write Volume

Location pings: every 30 sec outside geofence, every 5 min inside.
At 100K wearers, worst case (all outside geofence): ~3,300 writes/sec — handled by RDS with connection pooling (PgBouncer).

### Redis Usage Patterns

- On-call schedule cache: `oncall:{wearer_id}` → serialized schedule (invalidated on update)
- Active SOS timer: `sos_timer:{sos_event_id}:{tier}` → TTL = tier timeout seconds
- Active SOS session: `sos_active:{wearer_id}` → sos_event_id (prevents duplicate SOS)
- Refresh tokens: `refresh:{token_uuid}` → user_id, TTL = 30 days
- WebSocket pub/sub: `wearer:{wearer_id}` channel for real-time events
