package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type KlikSettleRequest struct {
	SessionID  string `json:"session_id,omitempty"`
	TransferID string `json:"transfer_id"`
	System     string `json:"system,omitempty"`
	From       string `json:"from"`
	To         string `json:"to"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
}

type KlikSettleResponse struct {
	TransferID     string `json:"transfer_id"`
	Status         string `json:"status"`
	RTGSReference  string `json:"rtgs_reference"`
	FailureReason  string `json:"failure_reason,omitempty"`
}

func klikRef(transferID string) string {
	if len(transferID) > 12 {
		return "CHAPS-" + strings.ToUpper(transferID[:12])
	}
	return "CHAPS-" + strings.ToUpper(transferID)
}

func (s *Server) handleKlikSettle(w http.ResponseWriter, r *http.Request) {
	var req KlikSettleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: "invalid request body",
		})
		return
	}

	if req.TransferID == "" || req.From == "" || req.To == "" {
		writeJSON(w, http.StatusBadRequest, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: "transfer_id, from, and to are required",
		})
		return
	}

	if req.Currency != "" && req.Currency != "GBP" {
		writeJSON(w, http.StatusUnprocessableEntity, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: fmt.Sprintf("CHAPS supports only GBP, got %s", req.Currency),
		})
		return
	}

	amount, err := strconv.ParseFloat(req.Amount, 64)
	if err != nil || amount <= 0 {
		writeJSON(w, http.StatusBadRequest, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: "invalid amount",
		})
		return
	}

	senderBIC, err := s.Ledger.LookupBankByName(r.Context(), req.From)
	if err != nil {
		writeJSON(w, http.StatusOK, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: fmt.Sprintf("unknown sender bank: %s", req.From),
		})
		return
	}
	receiverBIC, err := s.Ledger.LookupBankByName(r.Context(), req.To)
	if err != nil {
		writeJSON(w, http.StatusOK, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: fmt.Sprintf("unknown receiver bank: %s", req.To),
		})
		return
	}

	cfg := loadGlobalSchedule("chaps")
	if err := checkCHAPSHours(cfg, time.Now()); err != nil {
		writeJSON(w, http.StatusOK, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: err.Error(),
		})
		return
	}

	result, err := s.Ledger.SettlePayment(r.Context(), req.TransferID, senderBIC, receiverBIC, amount, "", "", "", req.SessionID)
	if err != nil {
		log.Printf("[KLIK] Ledger failure for transfer %s: %v", req.TransferID, err)
		writeJSON(w, http.StatusOK, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "FAILED",
			FailureReason: "internal error",
		})
		return
	}

	if result.Status == "ACTC" {
		log.Printf("[KLIK] Settled %s: %s -> %s, GBP %.2f", req.TransferID, req.From, req.To, amount)
		writeJSON(w, http.StatusOK, KlikSettleResponse{
			TransferID:    req.TransferID,
			Status:        "SUCCESS",
			RTGSReference: klikRef(req.TransferID),
		})
		return
	}

	reason := result.ReasonCode
	if reason == "" {
		reason = "payment rejected"
	}
	writeJSON(w, http.StatusOK, KlikSettleResponse{
		TransferID:    req.TransferID,
		Status:        "FAILED",
		FailureReason: reason,
	})
}

func (s *Server) handleKlikHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"system": "CHAPS",
	})
}
