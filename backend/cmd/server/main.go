package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shahprincea/leo/backend/internal/api"
	"github.com/shahprincea/leo/backend/internal/cache"
	"github.com/shahprincea/leo/backend/internal/config"
	"github.com/shahprincea/leo/backend/internal/db"
)

func main() {
	cfg := config.Load()

	// Initialize database pool. A missing or unreachable DB is non-fatal at
	// startup so the service can still serve /health while the DB recovers.
	ctx := context.Background()
	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("WARNING: database unavailable: %v", err)
	} else {
		defer pool.Close()
		log.Println("Database connection pool established")
	}

	// Initialize Redis cache. Unavailability is non-fatal; the service
	// continues without caching until Redis becomes reachable.
	redisClient, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Printf("WARNING: Redis cache unavailable: %v", err)
	} else {
		defer redisClient.Close()
		log.Println("Redis cache connection established")
	}

	router := api.NewRouter()

	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine so we can listen for shutdown signals.
	go func() {
		log.Printf("Starting Mayuri backend on port %s (env: %s)", cfg.ServerPort, cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Block until SIGINT or SIGTERM is received.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}

	log.Println("Server stopped cleanly")
}
