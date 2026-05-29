package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/m00nk0d3/wrappr/api/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	router := buildRouter()

	log.Printf("server listening on %s", cfg.Addr())
	if err := router.Run(cfg.Addr()); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited with error: %v", err)
	}
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
