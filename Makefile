.PHONY: up down logs migrate-up migrate-down migrate-version migrate-force

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

# Run all pending migrations (requires DATABASE_URL in environment or .env)
migrate-up:
	cd api && go run ./cmd/migrate up

# Roll back all migrations
migrate-down:
	cd api && go run ./cmd/migrate down

# Print current schema version
migrate-version:
	cd api && go run ./cmd/migrate version

# Force schema version without running migrations (use after resolving a dirty state)
# Usage: make migrate-force VERSION=3
migrate-force:
	cd api && go run ./cmd/migrate force $(VERSION)
