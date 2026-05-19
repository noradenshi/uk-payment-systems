package server

import (
	"chaps-service/pkg/iso20022"
	"chaps-service/pkg/ledger"
	"chaps-service/pkg/validator"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Validator *validator.ValidatorRegistry
	Ledger    *ledger.LedgerService
}

var reBIC = regexp.MustCompile(`^[A-Z0-9]{8,11}$`)

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/participants/register", s.handleRegister)
	mux.HandleFunc("GET /v1/participants", s.handleListParticipants)
	mux.HandleFunc("PATCH /v1/participants/{bic}/status", s.handleUpdateParticipantStatus)
	mux.HandleFunc("POST /v1/participants/{bic}/block", s.handleBlockParticipant)
	mux.HandleFunc("GET /v1/participants/{bic}/block", s.handleGetBlock)
	mux.HandleFunc("DELETE /v1/participants/{bic}/block", s.handleUnblockParticipant)
	mux.HandleFunc("GET /v1/participants/{bic}/positions", s.handleGetPosition)

	mux.HandleFunc("POST /v1/liquidity/top-up", s.handleTopUp)

	mux.HandleFunc("POST /v1/payments/chaps", s.ProcessPayment)
	mux.HandleFunc("GET /v1/payments/chaps", s.handleListPayments)
	mux.HandleFunc("POST /v1/payments/chaps/validate", s.handleValidatePayment)
	mux.HandleFunc("GET /v1/payments/chaps/limits", s.handleGetLimits)
	mux.HandleFunc("POST /v1/payments/chaps/{id}/authorize", s.handleAuthorizePayment)
	mux.HandleFunc("GET /v1/payments/chaps/{id}", s.GetPayment)
	mux.HandleFunc("DELETE /v1/payments/chaps/{id}", s.handleCancelPayment)
	mux.HandleFunc("POST /v1/payments/chaps/{id}/amend", s.handleAmendPayment)

	mux.HandleFunc("GET /v1/system/schedule", s.handleSystemSchedule)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}

func badRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

func validateBIC(bic string) bool {
	return reBIC.MatchString(bic)
}

func (s *Server) GetPayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}

	details, err := s.Ledger.GetPaymentDetails(r.Context(), msgID)
	if err != nil {
		if errors.Is(err, ledger.ErrAccountNotFound) {
			http.Error(w, "Payment not found", http.StatusNotFound)
			return
		}
		log.Printf("Query error for %s: %v", msgID, err)
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, details)
}

func (s *Server) handleListPayments(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			badRequest(w, "Invalid limit parameter")
			return
		}
	}

	payments, err := s.Ledger.ListPayments(r.Context(), strings.ToUpper(r.URL.Query().Get("status")), limit)
	if err != nil {
		log.Printf("Failed to list payments: %v", err)
		http.Error(w, "Failed to list payments", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s *Server) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	participants, err := s.Ledger.ListParticipants(r.Context())
	if err != nil {
		log.Printf("Failed to list participants: %v", err)
		http.Error(w, "Failed to list participants", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, participants)
}

func (s *Server) handleUpdateParticipantStatus(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	req.Status = strings.ToUpper(req.Status)
	if req.Status != "ACTIVE" && req.Status != "SUSPENDED" && req.Status != "DISABLED" {
		badRequest(w, "Status must be ACTIVE, SUSPENDED, or DISABLED")
		return
	}
	if err := s.Ledger.UpdateParticipantStatus(r.Context(), bic, req.Status, req.Reason); err != nil {
		log.Printf("Failed to update participant %s: %v", bic, err)
		http.Error(w, "Failed to update participant", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": req.Status})
}

func (s *Server) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	details, err := s.Ledger.GetBlockDetails(r.Context(), bic)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "Participant not found", http.StatusNotFound)
			return
		}
		log.Printf("Block details error for %s: %v", bic, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC     string  `json:"bic"`
		Name    string  `json:"name"`
		Balance float64 `json:"balance"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}

	if req.BIC == "" || req.Name == "" {
		badRequest(w, "BIC and Name are required")
		return
	}
	if !validateBIC(req.BIC) {
		badRequest(w, "BIC must be 8-11 alphanumeric characters")
		return
	}
	if req.Balance < 0 {
		badRequest(w, "Initial balance cannot be negative")
		return
	}

	err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.Balance)
	if err != nil {
		log.Printf("Failed to register participant %s: %v", req.BIC, err)
		http.Error(w, "Failed to create participant", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"bic": req.BIC, "status": "ACTIVE"})
}

func (s *Server) handleGetPosition(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	pos, err := s.Ledger.GetPosition(r.Context(), bic)
	if err != nil {
		log.Printf("Error fetching position for %s: %v", bic, err)
		http.Error(w, "Participant not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, pos)
}

func (s *Server) handleTopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC    string  `json:"bic"`
		Amount float64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}

	if !validateBIC(req.BIC) {
		badRequest(w, "Invalid BIC format")
		return
	}
	if req.Amount <= 0 {
		badRequest(w, "Amount must be positive")
		return
	}

	err := s.Ledger.TopUpLiquidity(r.Context(), req.BIC, req.Amount)
	if err != nil {
		log.Printf("Liquidity top-up failed for %s: %v", req.BIC, err)
		http.Error(w, "Failed to update liquidity", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"bic": req.BIC, "status": "UPDATED"})
}

func (s *Server) handleBlockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	reason := "FRAUD_SUSPECTED"
	var req struct {
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Reason != "" {
			reason = req.Reason
		}
	}

	err := s.Ledger.BlockParticipant(r.Context(), bic, reason)
	if err != nil {
		log.Printf("Failed to block %s: %v", bic, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "SUSPENDED", "reason": reason})
}

func (s *Server) handleUnblockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	err := s.Ledger.UnblockParticipant(r.Context(), bic)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "ACTIVE"})
}

func (s *Server) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/json") {
		s.processJSONPayment(w, r)
		return
	}

	if strings.Contains(contentType, "application/xml") || strings.Contains(contentType, "text/xml") {
		s.processXMLPayment(w, r)
		return
	}

	http.Error(w, "Unsupported Media Type: use application/json or application/xml", http.StatusUnsupportedMediaType)
}

func (s *Server) processXMLPayment(w http.ResponseWriter, r *http.Request) {
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
		s.sendXMLReject(w, "XMLI", "SCHEMA-ERR")
		return
	}

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal(docBytes, &msg); err != nil {
		log.Printf("XML Unmarshal Error: %v", err)
		s.sendXMLReject(w, "XMLI", "PARSE-ERR")
		return
	}

	if msg.MsgId == "" || !validateBIC(msg.Sender) || !validateBIC(msg.DestBIC) || msg.Amount <= 0 {
		s.sendXMLReject(w, "XMLI", "INVALID-FIELDS")
		return
	}

	if len(msg.MsgId) > 35 {
		s.sendXMLReject(w, "XMLI", "MSGID-TOO-LONG")
		return
	}

	res, err := s.Ledger.SettlePayment(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount, msg.EndToEndId)
	if err != nil {
		log.Printf("[CRITICAL] Ledger system failure for MsgId %s: %v", msg.MsgId, err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	pacs002 := iso20022.NewPacs002(msg.MsgId, msg.EndToEndId, res.Status, msg.Sender, msg.DestBIC, res.ReasonCode)

	responseMsg := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH(msg.DestBIC, msg.Sender, msg.MsgId, "pacs.002.001.16"),
		Document: pacs002,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("X-Transaction-Status", res.Status)

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

func (s *Server) processJSONPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MsgID       string  `json:"msg_id"`
		EndToEndID  string  `json:"end_to_end_id"`
		SenderBIC   string  `json:"sender_bic"`
		ReceiverBIC string  `json:"receiver_bic"`
		Amount      float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.MsgID == "" || req.SenderBIC == "" || req.ReceiverBIC == "" || req.Amount <= 0 {
		badRequest(w, "msg_id, sender_bic, receiver_bic, and positive amount are required")
		return
	}
	if !validateBIC(req.SenderBIC) || !validateBIC(req.ReceiverBIC) {
		badRequest(w, "Invalid BIC format")
		return
	}
	if len(req.MsgID) > 35 {
		badRequest(w, "msg_id exceeds 35 character limit")
		return
	}

	res, err := s.Ledger.SettlePayment(r.Context(), req.MsgID, req.SenderBIC, req.ReceiverBIC, req.Amount, req.EndToEndID)
	if err != nil {
		log.Printf("[CRITICAL] Ledger system failure for MsgId %s: %v", req.MsgID, err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	status := "SETTLED"
	httpStatus := http.StatusOK
	if res.Status == "PDNG" {
		status = "QUEUED"
		httpStatus = http.StatusAccepted
	}
	if res.Status == "RJCT" {
		status = "REJECTED"
		httpStatus = http.StatusAccepted
	}

	w.Header().Set("X-Transaction-Status", res.Status)
	writeJSON(w, httpStatus, map[string]string{
		"msg_id":      req.MsgID,
		"status":      status,
		"iso_status":  res.Status,
		"reason_code": res.ReasonCode,
	})
}

func (s *Server) handleValidatePayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SenderBIC   string  `json:"sender_bic"`
		ReceiverBIC string  `json:"receiver_bic"`
		Amount      float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	result, err := s.Ledger.ValidatePayment(r.Context(), req.SenderBIC, req.ReceiverBIC, req.Amount)
	if err != nil {
		log.Printf("Validation failed: %v", err)
		http.Error(w, "Validation failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	bic := r.URL.Query().Get("bic")
	if bic != "" && !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	limits, err := s.Ledger.GetClearingLimits(r.Context(), strings.ToUpper(bic))
	if err != nil {
		http.Error(w, "Limits unavailable", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (s *Server) handleAuthorizePayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}

	details, err := s.Ledger.GetPaymentDetails(r.Context(), msgID)
	if err != nil {
		http.Error(w, "Payment not found", http.StatusNotFound)
		return
	}

	if details["status"] != "PENDING" && details["status"] != "QUEUED" {
		http.Error(w, "Only pending or queued payments can be authorized", http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"msg_id":        msgID,
		"status":        "AUTHORIZED",
		"authorized_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleCancelPayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}

	cancelled, err := s.Ledger.CancelPayment(r.Context(), msgID)
	if err != nil {
		http.Error(w, "Cancel failed", http.StatusInternalServerError)
		return
	}
	if !cancelled {
		http.Error(w, "Payment cannot be cancelled unless it is PENDING or QUEUED", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg_id": msgID, "status": "CANCELLED"})
}

func (s *Server) handleAmendPayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}

	var req struct {
		EndToEndID string `json:"end_to_end_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}

	amended, err := s.Ledger.AmendPayment(r.Context(), msgID, req.EndToEndID)
	if err != nil {
		http.Error(w, "Amend failed", http.StatusInternalServerError)
		return
	}
	if !amended {
		http.Error(w, "Payment cannot be amended unless it is PENDING", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg_id": msgID, "status": "AMENDED"})
}

func (s *Server) handleSystemSchedule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"date":              time.Now().Format("2006-01-02"),
		"opening_time":      "06:00",
		"customer_cutoff":   "17:40",
		"interbank_cutoff": "18:00",
		"timezone":          "Europe/London",
	})
}

func (s *Server) sendXMLReject(w http.ResponseWriter, reason string, detail string) {
	pacs002 := iso20022.NewPacs002(
		"NONREF",
		"NONREF",
		"RJCT",
		"SYSTEM",
		"UNKNOWN",
		reason,
	)

	responseMsg := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH("SYSTEM", "UNKNOWN", "REJ-GENERIC", "pacs.002.001.16"),
		Document: pacs002,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("X-Transaction-Status", "RJCT")
	w.WriteHeader(http.StatusAccepted)

	if err := xml.NewEncoder(w).Encode(responseMsg); err != nil {
		log.Printf("Failed to encode reject: %v", err)
	}
}
