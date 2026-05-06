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
	if err := reg.Register(file, "xsd/" + file + ".xsd"); err != nil {
		log.Fatalf("Fatal: %v", err)
	}
}

func main() {
	ctx := context.Background()

	// 1. Initialize DB
	pool, _ := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))

	// 2. Initialize Validator Registry
	reg := validator.NewValidatorRegistry()
	registerXSD(reg, "pacs.008.001.14")
	registerXSD(reg, "pacs.002.001.16")
	registerXSD(reg, "head.001.001.02")
	registerXSD(reg, "head.001.001.04")

	// 3. Initialize Server
	srv := &server.Server{
		Validator: reg,
		Ledger:    ledger.NewLedgerService(pool),
	}

	// 4. Set up routes and start
	http.HandleFunc("/v1/payments/chaps", srv.ProcessPayment)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
