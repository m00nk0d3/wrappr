.PHONY: up down logs migrate-up migrate-down migrate-version migrate-force sqlc web-dev

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

# Run all pending migrations (requires DATABASE_URL in environment or .env)
migrate-up:
	cd api && go run ./cmd/migrate -path file://../db/migrations up

# Roll back all migrations
migrate-down:
	cd api && go run ./cmd/migrate -path file://../db/migrations down

# Print current schema version
migrate-version:
	cd api && go run ./cmd/migrate -path file://../db/migrations version

# Force schema version without running migrations (use after resolving a dirty state)
# Usage: make migrate-force VERSION=3
migrate-force:
	cd api && go run ./cmd/migrate -path file://../db/migrations force $(VERSION)

# Regenerate type-safe DB code from SQL queries
sqlc:
	cd api && sqlc generate

web-dev: ## Start Next.js dashboard dev server on :3001
	cd web && npm run dev
