# Plan: Issue #2 — Project Scaffold

## Goal
Set up the three-layer project structure for Mayuri:
1. Go backend (REST API server)
2. Flutter companion app (iOS/Android/Web)
3. Wear OS app (Kotlin)
4. Docker Compose (Postgres 15 + Redis 7)
5. PostgreSQL migrations (all tables from architecture doc)
6. GitHub Actions CI (build + lint for all three projects)

## Directory Structure
```
/
├── backend/              # Go backend
│   ├── cmd/server/       # main.go entry point
│   ├── internal/
│   │   ├── api/          # HTTP handlers
│   │   ├── config/       # config loading
│   │   └── db/           # database connection
│   ├── migrations/       # SQL migration files
│   ├── go.mod
│   └── Dockerfile
├── companion/            # Flutter app
│   ├── lib/
│   │   ├── main.dart
│   │   └── app.dart
│   └── pubspec.yaml
├── wearos/               # Wear OS Kotlin app
│   ├── app/
│   │   └── src/main/
│   │       ├── AndroidManifest.xml
│   │       └── java/com/mayuri/watch/
│   │           └── MainActivity.kt
│   ├── build.gradle.kts
│   └── settings.gradle.kts
├── docker-compose.yml
├── .github/
│   └── workflows/
│       └── ci.yml
└── README.md
```

## Tasks

### Task 1: Initialize git repo + base structure
- git init in /Users/prince.shah/leo
- Create directory structure above
- Add .gitignore (Go, Flutter, Android, IDE files)

### Task 2: Go backend scaffold
- go mod init github.com/shahprincea/leo/backend
- Add dependencies: chi (router), pgx (postgres), redis, godotenv
- cmd/server/main.go: starts HTTP server on :8080
- internal/config/config.go: loads env vars
- internal/db/db.go: pgx connection pool
- internal/api/health.go: GET /health returns { "status": "ok" }
- Dockerfile: multi-stage build

### Task 3: Database migrations
- Use golang-migrate format (up/down SQL files)
- Create all tables from architecture doc:
  001_users.up.sql
  002_wearers.up.sql
  003_watches.up.sql
  004_emergency_contacts_oncall.up.sql
  005_health_readings.up.sql
  006_locations_geofences.up.sql
  007_sos_events.up.sql
  008_medications.up.sql
  009_wellness.up.sql
  010_messages.up.sql
  011_subscriptions.up.sql
  012_audit_logs.up.sql

### Task 4: Docker Compose
- postgres:15 with POSTGRES_DB=mayuri
- redis:7-alpine
- backend service (build from ./backend)
- .env.example with all required vars

### Task 5: Flutter companion app scaffold
- Create Flutter project in companion/
- Add dependencies: go_router, riverpod, dio, flutter_secure_storage
- Basic MaterialApp with placeholder home screen
- Targeting iOS + Android + Web

### Task 6: Wear OS app scaffold
- Create Android/Wear OS project in wearos/
- Kotlin, minSdk 30 (Wear OS 3.0+)
- Single MainActivity with placeholder tile
- Add Wear OS dependencies

### Task 7: GitHub Actions CI
- .github/workflows/ci.yml
- Job 1: Go — go build ./... + go vet ./...
- Job 2: Flutter — flutter analyze + flutter test
- Job 3: Wear OS — ./gradlew build

### Task 8: Push to GitHub
- git add, commit, push to main

## Verification
- docker-compose up → postgres + redis healthy
- cd backend && go build ./... → no errors
- GET http://localhost:8080/health → { "status": "ok" }
- cd companion && flutter analyze → no errors
- cd wearos && ./gradlew build → BUILD SUCCESSFUL
