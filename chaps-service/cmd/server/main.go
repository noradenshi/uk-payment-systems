package main

import (
	"context"
	"log"
	"net/http"
	"os"

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

	// 1. Initialize DB
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		// Force TCP to avoid the /tmp/.s.PGSQL.5432 socket error
		connStr = "postgres://chaps_admin:password123@127.0.0.1:5432/chaps_ledger?sslmode=disable"
		log.Println("DATABASE_URL not set, falling back to localhost")
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Unable to create connection pool: %v", err)
	}
	defer pool.Close()

	// Ensure the DB is actually reachable
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	log.Println("Database connected successfully")

	// 2. Initialize Validator Registry
	reg := validator.NewValidatorRegistry()
	registerXSD(reg, "pacs.008.001.14")
	registerXSD(reg, "pacs.002.001.16")
	registerXSD(reg, "head.001.001.02")
	registerXSD(reg, "head.001.001.04")
	registerXSD(reg, "chaps_wrapper")

	// 3. Initialize Server
	srv := &server.Server{
		Validator: reg,
		Ledger:    ledger.NewLedgerService(pool),
	}

	// 4. Set up routes and start
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	log.Fatal(http.ListenAndServe(":8080", mux))
}
