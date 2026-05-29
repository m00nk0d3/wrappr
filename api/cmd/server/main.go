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
	"github.com/m00nk0d3/wrappr/api/internal/config"
)

const shutdownTimeout = 5 * time.Second

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: buildRouter(),
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
// Keeping it separate makes the router independently testable.
func buildRouter() *gin.Engine {
	router := gin.New()

	// Structured request logging and panic recovery.
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.GET("/health", healthHandler)

	return router
}

// healthHandler responds with a simple liveness payload.
//
//	GET /health → 200 {"status":"ok"}
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
