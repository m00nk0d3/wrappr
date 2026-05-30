package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/m00nk0d3/wrappr/api/internal/auth"
	"github.com/m00nk0d3/wrappr/api/internal/config"
	"github.com/m00nk0d3/wrappr/api/internal/mailer"
)

const shutdownTimeout = 5 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	m := mailer.NewResend(cfg.ResendAPIKey)

	router := buildRouter()
	registerAuthRoutes(router, pool, m, cfg.AppURL)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: router,
	}

	// Start server in a background goroutine so main can block on the signal.
	go func() {
		log.Printf("server listening on %s", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server exited with error: %v", err)
		}
	}()

	// Block until SIGINT or SIGTERM is received.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	// Give in-flight requests up to shutdownTimeout to complete.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	log.Println("server stopped")
}

// buildRouter constructs and returns the Gin engine with all routes registered.
// Keeping it separate (and zero-arg) makes the router independently testable.
func buildRouter() *gin.Engine {
	router := gin.New()

	// Structured request logging and panic recovery.
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.GET("/health", healthHandler)

	return router
}

// registerAuthRoutes adds authenticated/infrastructure routes that require
// external dependencies (DB pool, mailer). Called from main after deps are
// initialised so buildRouter stays independently testable.
func registerAuthRoutes(router *gin.Engine, pool *pgxpool.Pool, m mailer.Mailer, appURL string) {
	v1 := router.Group("/v1")
	v1.POST("/auth/register", auth.RegisterHandler(pool, m, appURL))
}

// healthHandler responds with a simple liveness payload.
//
//	GET /health → 200 {"status":"ok"}
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
