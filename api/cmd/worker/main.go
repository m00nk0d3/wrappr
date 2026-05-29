package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log.Println("worker started")

	// Block until an OS termination signal is received so the process is easy
	// to manage inside containers and process supervisors. A zero exit code
	// signals a clean shutdown to the orchestrator.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	log.Println("worker shutting down")
}
