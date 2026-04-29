package server

import (
	"chaps-service/pkg/iso20022"
	"chaps-service/pkg/ledger"
	"chaps-service/pkg/validator"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
)

type Server struct {
	Validator *validator.ValidatorRegistry
	Ledger    *ledger.LedgerService
}

func (s *Server) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request from %s", r.RemoteAddr)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("IO Error reading request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Hardcoded for the now
	version := "pacs.008.001.14"

	// STEP 1: Strict Validation
	if err := s.Validator.ValidateByVersion(version, body); err != nil {
		log.Printf("ISO 2022 [%s] Validation failed for: %v", version, err)
		http.Error(w, "Validation Failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// STEP 2: Extraction
	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal(body, &msg); err != nil {
		log.Printf("XML Unmarshal Error: %v", err)
		http.Error(w, "Failed to parse message structure", http.StatusBadRequest)
		return
	}

	// STEP 3: Settle in Postgres 18
	err = s.Ledger.SettlePayment(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount)

	var txStatus string
	if err != nil {
		if errors.Is(err, ledger.ErrInsufficientFunds) {
			txStatus = "PDNG" // Pending/Queued
		} else {
			log.Printf("Settlement error: %v", err)
			http.Error(w, "Internal Settlement Error", 500)
			return
		}
	} else {
		txStatus = "ACTC" // Accepted Technical (Settled)
	}

	// STEP 4: Generate pacs.002 Response
	responseMsg := iso20022.NewPacs002(msg.MsgId, msg.EndToEndId, txStatus, msg.Sender, msg.DestBIC)

    w.Header().Set("Content-Type", "application/xml")
    if txStatus == "ACTC" {
        w.WriteHeader(http.StatusOK)
    } else {
        w.WriteHeader(http.StatusAccepted)
    }
    
    if err := xml.NewEncoder(w).Encode(responseMsg); err != nil {
        log.Printf("Failed to encode pacs.002: %v", err)
    }

    log.Printf("Payment %s settled. Status: %s", msg.MsgId, txStatus)
}
