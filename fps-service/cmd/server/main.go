package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fps-service/pkg/events"
	"fps-service/pkg/ledger"
	"fps-service/pkg/server"
	"fps-service/pkg/validator"

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
		connStr = "postgres://fps_admin:password123@127.0.0.1:5433/fps_ledger?sslmode=disable"
		log.Println("DATABASE_URL not set, falling back to localhost")
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to create pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("DB connection failed: %v", err)
	}
	log.Println("Database connected")

	reg := validator.NewValidatorRegistry()
	registerXSD(reg, "pacs.008.001.14")
	registerXSD(reg, "pacs.002.001.16")
	registerXSD(reg, "head.001.001.02")
	registerXSD(reg, "head.001.001.04")
	registerXSD(reg, "chaps_wrapper")

	srv := &server.Server{
		Validator: reg,
		Ledger:    ledger.NewLedgerService(pool),
		Events:    events.NewEventBus(),
	}

	schedCtx, schedCancel := context.WithCancel(context.Background())
	go srv.StartScheduler(schedCtx)

	isoPort := os.Getenv("ISO8583_PORT")
	if isoPort == "" {
		isoPort = ":7421"
	}

	isoCtx, isoCancel := context.WithCancel(context.Background())
	go func() {
		if err := srv.StartISO8583Socket(isoCtx, isoPort); err != nil {
			log.Printf("ISO8583 socket error: %v", err)
		}
	}()

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    ":8081",
		Handler: mux,
	}

	go func() {
		log.Printf("FPS service starting HTTP on :8081")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down FPS service...")

	schedCancel()
	isoCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("FPS service stopped")
}
