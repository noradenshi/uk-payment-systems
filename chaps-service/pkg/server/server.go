package server

import (
	"chaps-service/pkg/iso20022"
	"chaps-service/pkg/ledger"
	"chaps-service/pkg/validator"
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"net/http"
)

type Server struct {
	Validator *validator.ValidatorRegistry
	Ledger    *ledger.LedgerService
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Participant Admin
	mux.HandleFunc("POST /v1/participants/register", s.handleRegister)
	mux.HandleFunc("POST /v1/participants/{bic}/block", s.BlockParticipant)
	mux.HandleFunc("DELETE /v1/participants/{bic}/block", s.UnblockParticipant)
	mux.HandleFunc("GET /v1/participants/{bic}/positions", s.handleGetPosition)

	// Liquidity Simulation
	mux.HandleFunc("POST /v1/liquidity/top-up", s.handleTopUp)

	// Core Payments (pacs.008)
	mux.HandleFunc("POST /v1/payments/chaps", s.ProcessPayment)
	mux.HandleFunc("GET /v1/payments/chaps/{id}", s.GetPayment)
}

func (s *Server) GetPayment(w http.ResponseWriter, r *http.Request) {
	// Correct way to get {id} in Go 1.22+
	msgID := r.PathValue("id")

	if msgID == "" {
		http.Error(w, "Missing transaction ID", http.StatusBadRequest)
		return
	}

	details, err := s.Ledger.GetPaymentDetails(r.Context(), msgID)
	if err != nil {
		log.Printf("Query error for %s: %v", msgID, err)
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Note: ensure you import "encoding/json" in server.go
	json.NewEncoder(w).Encode(details)
}

// handleRegister handles the onboarding of a new bank.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC     string  `json:"bic"`
		Name    string  `json:"name"`
		Balance float64 `json:"balance"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if req.BIC == "" || req.Name == "" {
		http.Error(w, "BIC and Name are required", http.StatusBadRequest)
		return
	}

	err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.Balance)
	if err != nil {
		log.Printf("Failed to register participant %s: %v", req.BIC, err)
		http.Error(w, "Failed to create participant", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleGetPosition returns the current liquidity standing of a participant.
func (s *Server) handleGetPosition(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")

	if bic == "" {
		http.Error(w, "BIC required", http.StatusBadRequest)
		return
	}

	pos, err := s.Ledger.GetPosition(r.Context(), bic)
	if err != nil {
		log.Printf("Error fetching position for %s: %v", bic, err)
		http.Error(w, "Participant not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pos)
}

// handleTopUp simulates a central bank liquidity injection.
func (s *Server) handleTopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC    string  `json:"bic"`
		Amount float64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Amount <= 0 {
		http.Error(w, "Amount must be positive", http.StatusBadRequest)
		return
	}

	err := s.Ledger.TopUpLiquidity(r.Context(), req.BIC, req.Amount)
	if err != nil {
		log.Printf("Liquidity top-up failed for %s: %v", req.BIC, err)
		http.Error(w, "Failed to update liquidity", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) BlockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")

	// Always validate that we have a BIC before hitting the DB
	if bic == "" {
		http.Error(w, "BIC required", http.StatusBadRequest)
		return
	}

	err := s.Ledger.UpdateParticipantStatus(r.Context(), bic, "SUSPENDED", "FRAUD_SUSPECTED")
	if err != nil {
		log.Printf("Failed to block %s: %v", bic, err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) UnblockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")

	err := s.Ledger.UpdateParticipantStatus(r.Context(), bic, "ACTIVE", "")
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("IO Error reading request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	docBytes, version, err := s.Validator.ValidateWrapped(body)
	if err != nil {
		log.Printf("Schema Validation failed [%s]: %v", version, err)
		s.sendManualReject(w, "XMLI", "SCHEMA-ERR")
		return
	}

	// Extraction
	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal(docBytes, &msg); err != nil {
		log.Printf("XML Unmarshal Error: %v", err)
		// If we reach here, schema passed but unmarshal failed (rare)
		s.sendManualReject(w, "XMLI", "PARSE-ERR")
		return
	}

	// Settle in Postgres 18
	res, err := s.Ledger.SettlePayment(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount)
	if err != nil {
		// 500 Internal Server Error (System/Database failure)
		log.Printf("[CRITICAL] Ledger system failure for MsgId %s: %v", msg.MsgId, err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	// Generate pacs.002 Response
	pacs002 := iso20022.NewPacs002(msg.MsgId, msg.EndToEndId, res.Status, msg.Sender, msg.DestBIC, res.ReasonCode)

	responseMsg := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH(msg.DestBIC, msg.Sender, msg.MsgId, "pacs.002.001.16"),
		Document: pacs002,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("X-Transaction-Status", res.Status)

	// 200 for success, 202 for pending/rejected but processed
	if res.Status == "ACTC" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusAccepted)
	}

	if err := xml.NewEncoder(w).Encode(responseMsg); err != nil {
		log.Printf("Final Response Encoding Error: %v", err)
	}

	log.Printf("Processed MsgId: %s | Result: %s | Reason: %s", msg.MsgId, res.Status, res.ReasonCode)
}

// sendManualReject handles cases where the incoming message is invalid
// or cannot be parsed. It sends a skeletal pacs.002 response.
func (s *Server) sendManualReject(w http.ResponseWriter, reason string, detail string) {
	// We use "UNKNOWN" or "NOTPROVIDED" for fields we couldn't extract
	pacs002 := iso20022.NewPacs002(
		"NONREF",  // Original MsgId unknown
		"NONREF",  // Original E2E unknown
		"RJCT",    // Status is always Rejected here
		"SYSTEM",  // Generic Sender
		"UNKNOWN", // Generic Receiver
		reason,    // The ISO Reason Code (e.g., XMLI)
	)

	responseMsg := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH("UNKNOWN", "SYSTEM", "REJ-GENERIC", "pacs.002.001.16"),
		Document: pacs002,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusAccepted) // 202: Message received but rejected by business logic

	if err := xml.NewEncoder(w).Encode(responseMsg); err != nil {
		log.Printf("Failed to encode manual reject: %v", err)
	}
}
