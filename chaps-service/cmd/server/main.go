// cmd/server/main.go
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

func main() {
	ctx := context.Background()

	// 1. Initialize DB
	pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))

	// 2. Initialize Validator Registry
	reg := validator.NewValidatorRegistry()

	if err := reg.Register("pacs.008.001.14", "xsd/pacs.008.001.14.xsd"); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
	if err := reg.Register("pacs.002.001.16", "xsd/pacs.002.001.16.xsd"); err != nil {
		log.Fatalf("Fatal: %v", err)
	}

	// 3. Initialize Server
	srv := &server.Server{
		Validator: reg,
		Ledger:    ledger.NewLedgerService(pool),
	}

	// 4. Set up routes and start
	http.HandleFunc("/api/dashboard", srv.Dashboard)
	http.HandleFunc("/pay", srv.ProcessPayment)
	log.Println("Engine starting...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
