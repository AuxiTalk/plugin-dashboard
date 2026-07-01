package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/auxitalk/plugin-dashboard/internal/server"
)

func main() {
	port := "8080"
	if p := os.Getenv("DASHBOARD_PORT"); p != "" {
		port = p
	}

	srv := server.NewServer(port)

	go func() {
		log.Printf("[dashboard] starting on http://0.0.0.0:%s", port)
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[dashboard] server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("[dashboard] shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[dashboard] shutdown error: %v", err)
	}

	fmt.Println("[dashboard] stopped")
}
