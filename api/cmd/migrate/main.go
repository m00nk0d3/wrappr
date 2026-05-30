// Package main is a one-off migration runner. It is intended to be executed
// as a pre-deploy job (e.g. a Fly.io task), never inside the API process.
//
// Usage:
//
//	DATABASE_URL=postgres://... ./migrate [-path file://../db/migrations] [up|down|version|force N]
//
// The -path flag defaults to file://../db/migrations, which resolves correctly
// when running from the api/ directory (either via go run or a compiled binary).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	migrationsPath := flag.String("path", "", "Path to migrations directory (e.g. file://../db/migrations). Falls back to MIGRATIONS_PATH env var, then 'file://../db/migrations'.")
	flag.Parse()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	if *migrationsPath == "" {
		*migrationsPath = os.Getenv("MIGRATIONS_PATH")
	}
	if *migrationsPath == "" {
		*migrationsPath = "file://../db/migrations"
	}

	m, err := migrate.New(*migrationsPath, databaseURL)
	if err != nil {
		log.Fatalf("failed to create migrator: %v", err)
	}
	defer m.Close()

	args := flag.Args()
	command := "up"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatalf("migrate up failed: %v", err)
		}
		log.Println("migrations applied successfully")

	case "down":
		// Down rolls back ALL migrations to version 0 — every table is dropped.
		// Use only in local dev or to fully reset a staging environment.
		// In production, prefer deploying a new forward migration instead.
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
		if len(args) < 2 {
			log.Fatal("force requires a version number: migrate force <N>")
		}
		version, err := strconv.Atoi(args[1])
		if err != nil {
			log.Fatalf("invalid version %q: %v", args[1], err)
		}
		if err := m.Force(version); err != nil {
			log.Fatalf("migrate force failed: %v", err)
		}
		log.Printf("forced version to %d", version)

	default:
		log.Fatalf("unknown command %q — use: up | down | version | force <N>", command)
	}
}
