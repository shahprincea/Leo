# Mayuri

Elderly care watch platform — named after the founder's mother, built for his father.

A Wear OS smartwatch app that provides passive, always-on safety monitoring for elderly users. Family members configure everything remotely. The elderly person just wears the watch.

**Features:** SOS emergency routing, fall detection, GPS tracking, medication reminders, health monitoring, two-way calling, daily wellness check-ins.

---

## Quick Start

**Prerequisites:** [Docker Desktop](https://www.docker.com/products/docker-desktop/)

```bash
# 1. Clone and enter the repo
git clone https://github.com/shahprincea/Leo.git
cd Leo

# 2. One-time setup
make setup

# 3. Start everything
make up
```

The backend is running at `http://localhost:8080`.

**Verify it works:**
```bash
make health
# → { "status": "ok", "version": "0.1.0" }
```

---

## Run Database Migrations

After `make up`, run migrations to create all tables:

```bash
make migrate
```

> Requires `golang-migrate` — the command installs it via Homebrew automatically if missing.

---

## Test the API Manually

### Register a user
```bash
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123","full_name":"Prince Shah"}' \
  | python3 -m json.tool
```

### Login
```bash
curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"password123"}' \
  | python3 -m json.tool
```

### Create a wearer (use the access_token from login)
```bash
curl -s -X POST http://localhost:8080/wearers \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <access_token>" \
  -d '{"full_name":"Dad","pin":"1234","blood_type":"O+","allergies":["penicillin"]}' \
  | python3 -m json.tool
```

### Authenticate a watch device (returns device_token)
```bash
curl -s -X POST http://localhost:8080/auth/device \
  -H "Content-Type: application/json" \
  -d '{"wearer_id":"<wearer_id>","pin":"1234"}' \
  | python3 -m json.tool
```

### Register a watch (use device_token)
```bash
curl -s -X POST http://localhost:8080/watches/register \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <device_token>" \
  -d '{"device_id":"hw-abc123","model":"samsung_galaxy_watch6_lte","os_version":"4.0","carrier":"T-Mobile"}' \
  | python3 -m json.tool
```

### Fetch watch config
```bash
curl -s http://localhost:8080/watches/config \
  -H "Authorization: Bearer <device_token>" \
  | python3 -m json.tool
```

### Trigger SOS (from watch)
```bash
curl -s -X POST http://localhost:8080/sos \
  -H "Authorization: Bearer <device_token>" \
  -H "Content-Type: application/json" \
  -d '{"triggered_by":"manual"}' \
  | python3 -m json.tool
```

### Cancel SOS (wearer tapped I'm OK)
```bash
curl -s -X POST http://localhost:8080/sos/<sos_id>/cancel \
  -H "Authorization: Bearer <device_token>" \
  | python3 -m json.tool
```

### Report a detected fall
```bash
curl -s -X POST http://localhost:8080/falls \
  -H "Authorization: Bearer <device_token>" \
  -H "Content-Type: application/json" \
  -d '{"fall_type":"hard"}' \
  | python3 -m json.tool
```

### Cancel a fall (user tapped I'm OK during countdown)
```bash
curl -s -X POST http://localhost:8080/falls/<fall_id>/cancel \
  -H "Authorization: Bearer <device_token>" \
  | python3 -m json.tool
```

---

## Run Tests

```bash
make test
```

---

## Commands Reference

| Command | What it does |
|---|---|
| `make setup` | Copy `.env.example` → `.env`, build Docker images |
| `make up` | Start Postgres + Redis + backend |
| `make up-bg` | Start in background |
| `make down` | Stop all services |
| `make migrate` | Run all DB migrations |
| `make migrate-down` | Roll back all DB migrations |
| `make test` | Run backend tests (verbose) |
| `make test-short` | Run backend tests (quiet) |
| `make health` | Check backend health endpoint |
| `make logs` | Tail backend logs |

---

## Project Structure

```
├── backend/          # Go REST API (chi, pgx, Redis)
│   ├── cmd/server/   # Entry point
│   ├── internal/
│   │   ├── api/      # HTTP handlers
│   │   ├── auth/     # JWT, bcrypt, middleware
│   │   ├── config/   # Config loading
│   │   ├── db/       # Postgres connection
│   │   └── cache/    # Redis client
│   └── migrations/   # SQL migrations (up/down)
├── companion/        # Flutter app (iOS/Android/Web)
├── wearos/           # Wear OS app (Kotlin)
├── docker-compose.yml
└── Makefile
```

---

## Environment Variables

Copy `.env.example` to `.env` and update as needed:

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | Backend port |
| `DATABASE_URL` | postgres://mayuri:... | Postgres connection string |
| `REDIS_URL` | redis://localhost:6379 | Redis connection |
| `JWT_SECRET` | change-me | **Change this in production** |
| `TWILIO_ACCOUNT_SID` | — | For SOS calls + SMS (Issues #6+, swap `NoopCaller` in router.go) |
| `FIREBASE_PROJECT_ID` | — | For push notifications |
| `STRIPE_SECRET_KEY` | — | For billing |

---

## Development Status

| Issue | Feature | Status |
|---|---|---|
| #2 | Project scaffold (Go, Flutter, Wear OS, migrations) | ✅ Done |
| #3 | Auth (register, login, JWT, refresh, watch PIN) | ✅ Done |
| #4 | Wearer + family member management | ✅ Done |
| #5 | Watch registration + remote config sync | ✅ Done |
| #6 | SOS routing + escalation tiers | ✅ Done |
| #7 | Fall detection | 🔄 In Progress |
| #8 | GPS + geofencing + wandering alerts | ⏳ Pending |
| #9 | Health monitoring (HR, SpO2, steps) | ⏳ Pending |
| #10 | Blood pressure (Samsung only) | ⏳ Pending |
| #11 | Two-way calling (VoIP + cellular) | ⏳ Pending |
| #12 | Medication tracking | ⏳ Pending |
| #13 | Daily wellness check-in | ⏳ Pending |
| #14 | Watch UX + battery + wear detection | ⏳ Pending |
| #15 | Messaging (presets + family push) | ⏳ Pending |
| #16 | Emergency Medical ID | ⏳ Pending |
| #17 | Subscription + billing | ⏳ Pending |
| #18 | HIPAA audit logging | ⏳ Pending |

---

## Docs

- [`mayuri-prd.md`](mayuri-prd.md) — Full product requirements
- [`mayuri-architecture.md`](mayuri-architecture.md) — Technical architecture + DB schema + API contracts
