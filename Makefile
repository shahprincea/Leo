.PHONY: setup up down migrate test health logs

# ── First-time setup ──────────────────────────────────────────────
setup:
	cp -n .env.example .env || true
	docker compose build

# ── Run ──────────────────────────────────────────────────────────
up:
	docker compose up

up-bg:
	docker compose up -d

down:
	docker compose down

# ── Database migrations ───────────────────────────────────────────
migrate:
	@which migrate > /dev/null || (echo "Installing golang-migrate..." && brew install golang-migrate)
	migrate -path backend/migrations \
	        -database "$$(grep DATABASE_URL .env | cut -d= -f2-)" \
	        up

migrate-down:
	migrate -path backend/migrations \
	        -database "$$(grep DATABASE_URL .env | cut -d= -f2-)" \
	        down

# ── Test ─────────────────────────────────────────────────────────
test:
	cd backend && go test ./... -v

test-short:
	cd backend && go test ./... -count=1

# ── Health check ──────────────────────────────────────────────────
health:
	curl -s http://localhost:8080/health | python3 -m json.tool

# ── Logs ─────────────────────────────────────────────────────────
logs:
	docker compose logs -f backend
