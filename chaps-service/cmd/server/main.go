package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chaps-service/pkg/ledger"
	"chaps-service/pkg/server"
	"chaps-service/pkg/validator"

	"github.com/jackc/pgx/v5/pgxpool"
)

func registerXSD(reg *validator.ValidatorRegistry, file string) {
	if err := reg.Register(file, "xsd/"+file+".xsd"); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}

func main() {
	ctx := context.Background()

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://chaps_admin:password123@127.0.0.1:5432/chaps_ledger?sslmode=disable"
		log.Println("DATABASE_URL not set, falling back to localhost")
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Database connected successfully")

	reg := validator.NewValidatorRegistry()
	registerXSD(reg, "pacs.008.001.14")
	registerXSD(reg, "pacs.002.001.16")
	registerXSD(reg, "head.001.001.02")
	registerXSD(reg, "head.001.001.04")
	registerXSD(reg, "chaps_wrapper")

	srv := &server.Server{
		Validator: reg,
		Ledger:    ledger.NewLedgerService(pool),
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		log.Printf("CHAPS service starting on :8080")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down CHAPS service...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("CHAPS service stopped")
}
