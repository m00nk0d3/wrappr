// Package main is a one-off migration runner. It is intended to be executed
// as a pre-deploy job (e.g. a Fly.io task), never inside the API process.
//
// Usage:
//
//	DATABASE_URL=postgres://... ./migrate [up|down|version|force N]
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "file://db/migrations"
	}

	m, err := migrate.New(migrationsPath, databaseURL)
	if err != nil {
		log.Fatalf("failed to create migrator: %v", err)
	}
	defer m.Close()

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up failed: %v", err)
		}
		log.Println("migrations applied successfully")

	case "down":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate down failed: %v", err)
		}
		log.Println("all migrations rolled back")

	case "version":
		version, dirty, err := m.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			log.Fatalf("failed to get version: %v", err)
		}
		fmt.Printf("version=%d dirty=%v\n", version, dirty)

	case "force":
		if len(os.Args) < 3 {
			log.Fatal("force requires a version number: migrate force <N>")
		}
		version, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("invalid version %q: %v", os.Args[2], err)
		}
		if err := m.Force(version); err != nil {
			log.Fatalf("migrate force failed: %v", err)
		}
		log.Printf("forced version to %d", version)

	default:
		log.Fatalf("unknown command %q — use: up | down | version | force <N>", command)
	}
}
