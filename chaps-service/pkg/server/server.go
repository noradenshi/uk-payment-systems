package server

import (
	"chaps-service/pkg/auth"
	"chaps-service/pkg/events"
	"chaps-service/pkg/iso20022"
	"chaps-service/pkg/ledger"
	"chaps-service/pkg/validator"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	Validator *validator.ValidatorRegistry
	Ledger    *ledger.LedgerService
	Events    *events.EventBus
}

var reBIC = regexp.MustCompile(`^[A-Z0-9]{8,11}$`)

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("OPTIONS /", handleOptions)
	mux.HandleFunc("POST /v1/participants/register", s.handleRegister)
	mux.HandleFunc("GET /v1/participants", s.handleListParticipants)
	mux.HandleFunc("PATCH /v1/participants/{bic}", s.handleUpdateParticipant)
	mux.HandleFunc("DELETE /v1/participants/{bic}", s.handleDeleteParticipant)
	mux.HandleFunc("PATCH /v1/participants/{bic}/status", s.handleUpdateParticipantStatus)
	mux.HandleFunc("POST /v1/participants/{bic}/block", s.handleBlockParticipant)
	mux.HandleFunc("GET /v1/participants/{bic}/block", s.handleGetBlock)
	mux.HandleFunc("DELETE /v1/participants/{bic}/block", s.handleUnblockParticipant)
	mux.HandleFunc("PATCH /v1/participants/status", s.authMiddleware(s.handleUpdateParticipantStatus))
	mux.HandleFunc("POST /v1/participants/block", s.authMiddleware(s.handleBlockParticipant))
	mux.HandleFunc("GET /v1/participants/block", s.authMiddleware(s.handleGetBlock))
	mux.HandleFunc("DELETE /v1/participants/block", s.authMiddleware(s.handleUnblockParticipant))
	mux.HandleFunc("GET /v1/participants/positions", s.authMiddleware(s.handleGetPosition))

	mux.HandleFunc("POST /v1/liquidity/top-up", s.authMiddleware(s.handleTopUp))

	mux.HandleFunc("POST /v1/payments/chaps", s.authMiddleware(s.ProcessPayment))
	mux.HandleFunc("GET /v1/payments/chaps", s.handleListPayments)
	mux.HandleFunc("POST /v1/payments/chaps/validate", s.handleValidatePayment)
	mux.HandleFunc("GET /v1/payments/chaps/limits", s.handleGetLimits)
	mux.HandleFunc("PATCH /v1/payments/chaps/limits", s.authMiddleware(s.handleUpdateLimit))
	mux.HandleFunc("POST /v1/payments/chaps/gridlock/resolve", s.handleResolveGridlock)
	mux.HandleFunc("POST /v1/payments/chaps/{id}/authorize", s.handleAuthorizePayment)
	mux.HandleFunc("GET /v1/payments/chaps/{id}", s.GetPayment)
	mux.HandleFunc("DELETE /v1/payments/chaps/{id}", s.handleCancelPayment)
	mux.HandleFunc("POST /v1/payments/chaps/{id}/amend", s.handleAmendPayment)

	mux.HandleFunc("GET /v1/payments/chaps/incoming", s.authMiddleware(s.handleEvents))

	mux.HandleFunc("POST /v1/klik/chaps/settle", s.handleKlikSettle)
	mux.HandleFunc("GET /v1/klik/chaps/healthz", s.handleKlikHealth)

	mux.HandleFunc("GET /v1/system/schedule", s.handleSystemSchedule)
	mux.HandleFunc("GET /v1/healthz", s.handleHealthz)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	setCORS(w)
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

func participantBICFromRequest(r *http.Request) string {
	if bic := auth.BICFromContext(r.Context()); bic != "" {
		return bic
	}
	return strings.ToUpper(r.PathValue("bic"))
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,Idempotency-Key,X-Digital-Signature")
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
			return
		}
		apiKey := strings.TrimPrefix(authHeader, "Bearer ")
		if apiKey == "" {
			http.Error(w, `{"error":"missing API key"}`, http.StatusUnauthorized)
			return
		}
		bic, err := s.Ledger.ValidateAPIKey(r.Context(), apiKey)
		if err != nil {
			http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), auth.BICKey, bic)
		next(w, r.WithContext(ctx))
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "CHAPS"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	bic := auth.BICFromContext(r.Context())
	if bic == "" || !validateBIC(bic) {
		badRequest(w, "Authentication required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := s.Events.Subscribe(bic, 100)
	defer unsubscribe()

	for {
		select {
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func handleOptions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func loadGlobalSchedule(system string) map[string]interface{} {
	paths := []string{os.Getenv("UKPS_CONFIG_PATH"), "config/sessions.json", "../config/sessions.json", "../../config/sessions.json"}
	for _, path := range paths {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg map[string]map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err == nil {
			if entry, ok := cfg[system]; ok {
				return entry
			}
		}
	}
	return map[string]interface{}{}
}

func parseTimeOfDay(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

func checkCHAPSHours(cfg map[string]interface{}, now time.Time) error {
	openStr, _ := cfg["opening_time"].(string)
	cutoffStr, _ := cfg["interbank_cutoff"].(string)

	if openStr == "" {
		openStr = "06:00"
	}
	if cutoffStr == "" {
		cutoffStr = "18:00"
	}

	nowMin := now.Hour()*60 + now.Minute()
	openMin, err := parseTimeOfDay(openStr)
	if err != nil {
		return nil
	}
	cutoffMin, err := parseTimeOfDay(cutoffStr)
	if err != nil {
		return nil
	}

	if nowMin < openMin {
		return fmt.Errorf("CHAPS opens at %s", openStr)
	}
	if nowMin >= cutoffMin {
		return fmt.Errorf("CHAPS interbank cutoff at %s passed", cutoffStr)
	}
	return nil
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
	bic := participantBICFromRequest(r)
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

func (s *Server) handleUpdateParticipant(w http.ResponseWriter, r *http.Request) {
	bic := strings.ToUpper(r.PathValue("bic"))
	if !validateBIC(bic) {
		badRequest(w, "BIC must be 8-11 alphanumeric characters")
		return
	}

	var req struct {
		Name     string  `json:"name"`
		SortCode string  `json:"sort_code"`
		Balance  float64 `json:"balance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Name == "" {
		badRequest(w, "name is required")
		return
	}
	if req.SortCode == "" {
		badRequest(w, "sort_code is required")
		return
	}
	if req.Balance < 0 {
		badRequest(w, "balance cannot be negative")
		return
	}
	if err := s.Ledger.UpdateParticipant(r.Context(), bic, req.Name, req.SortCode, req.Balance); err != nil {
		if errors.Is(err, ledger.ErrAccountNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "participant not found"})
			return
		}
		log.Printf("Failed to update participant %s: %v", bic, err)
		http.Error(w, "Failed to update participant", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "updated"})
}

func (s *Server) handleDeleteParticipant(w http.ResponseWriter, r *http.Request) {
	bic := strings.ToUpper(r.PathValue("bic"))
	if !validateBIC(bic) {
		badRequest(w, "BIC must be 8-11 alphanumeric characters")
		return
	}
	if err := s.Ledger.DeleteParticipant(r.Context(), bic); err != nil {
		if errors.Is(err, ledger.ErrAccountNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "participant not found"})
			return
		}
		if errors.Is(err, ledger.ErrParticipantInUse) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "participant has related payments or journal entries"})
			return
		}
		log.Printf("Failed to delete participant %s: %v", bic, err)
		http.Error(w, "Failed to delete participant", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "deleted"})
}

func (s *Server) handleGetBlock(w http.ResponseWriter, r *http.Request) {
	bic := participantBICFromRequest(r)
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}

	details, err := s.Ledger.GetBlockDetails(r.Context(), bic)
	if err != nil {
		if errors.Is(err, ledger.ErrParticipantNotFound) {
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
		BIC      string  `json:"bic"`
		Name     string  `json:"name"`
		SortCode string  `json:"sort_code"`
		Balance  float64 `json:"balance"`
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
	if req.SortCode == "" {
		badRequest(w, "sort_code is required")
		return
	}
	if req.Balance < 0 {
		badRequest(w, "Initial balance cannot be negative")
		return
	}

	apiKey, err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.SortCode, req.Balance)
	if err != nil {
		log.Printf("Failed to register participant %s: %v", req.BIC, err)
		http.Error(w, "Failed to create participant", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"bic":     req.BIC,
		"api_key": apiKey,
		"status":  "ACTIVE",
	})
}

func (s *Server) handleGetPosition(w http.ResponseWriter, r *http.Request) {
	bic := auth.BICFromContext(r.Context())
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
		Amount float64 `json:"amount"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}

	if req.Amount <= 0 {
		badRequest(w, "Amount must be positive")
		return
	}

	bic := auth.BICFromContext(r.Context())
	err := s.Ledger.TopUpLiquidity(r.Context(), bic, req.Amount)
	if err != nil {
		log.Printf("Liquidity top-up failed for %s: %v", bic, err)
		http.Error(w, "Failed to update liquidity", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "UPDATED"})
}

func (s *Server) handleBlockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := participantBICFromRequest(r)
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
	bic := participantBICFromRequest(r)
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
	cfg := loadGlobalSchedule("chaps")
	if err := checkCHAPSHours(cfg, time.Now()); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusServiceUnavailable)
		return
	}

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

	if msg.SenderSortCode == "" || msg.DestSortCode == "" {
		s.sendXMLReject(w, "XMLI", "SORT-CODE-MISSING")
		return
	}

	if len(msg.MsgId) > 35 {
		s.sendXMLReject(w, "XMLI", "MSGID-TOO-LONG")
		return
	}

	authBic := auth.BICFromContext(r.Context())
	if authBic != "" && msg.Sender != authBic {
		s.sendXMLReject(w, "XMLI", "SENDER-MISMATCH")
		return
	}

	res, err := s.Ledger.SettlePayment(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount, msg.EndToEndId, msg.SenderSortCode, msg.DestSortCode, "")
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
		s.Events.Publish(msg.DestBIC, events.Event{
			Type: "payment.received",
			Data: map[string]interface{}{
				"msg_id":           msg.MsgId,
				"sender":           msg.Sender,
				"receiver":         msg.DestBIC,
				"receiver_sort_code": msg.DestSortCode,
				"receiver_account": msg.DestAccount,
				"amount":           msg.Amount,
				"status":           "SETTLED",
				"scheme":           "CHAPS",
			},
		})
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
		MsgID            string  `json:"msg_id"`
		EndToEndID       string  `json:"end_to_end_id"`
		ReceiverBIC      string  `json:"receiver_bic"`
		ReceiverSortCode string  `json:"receiver_sort_code"`
		ReceiverAccount  string  `json:"receiver_account"`
		Amount           float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.MsgID == "" || req.ReceiverBIC == "" || req.Amount <= 0 {
		badRequest(w, "msg_id, receiver_bic, and positive amount are required")
		return
	}
	if !validateBIC(req.ReceiverBIC) {
		badRequest(w, "Invalid BIC format")
		return
	}
	if req.ReceiverSortCode == "" {
		badRequest(w, "receiver_sort_code is required")
		return
	}
	if req.ReceiverAccount == "" {
		badRequest(w, "receiver_account is required")
		return
	}
	if len(req.MsgID) > 35 {
		badRequest(w, "msg_id exceeds 35 character limit")
		return
	}

	senderBic := auth.BICFromContext(r.Context())
	if !validateBIC(senderBic) {
		badRequest(w, "Invalid authentication")
		return
	}

	senderSortCode, err := s.Ledger.GetSortCode(r.Context(), senderBic)
	if err != nil {
		log.Printf("Failed to lookup sender sort code for %s: %v", senderBic, err)
		http.Error(w, "Sender not found", http.StatusInternalServerError)
		return
	}

	res, err := s.Ledger.SettlePayment(r.Context(), req.MsgID, senderBic, req.ReceiverBIC, req.Amount, req.EndToEndID, senderSortCode, req.ReceiverSortCode, "")
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

	if res.Status == "ACTC" {
		s.Events.Publish(req.ReceiverBIC, events.Event{
			Type: "payment.received",
			Data: map[string]interface{}{
				"msg_id":           req.MsgID,
				"sender":           senderBic,
				"receiver":         req.ReceiverBIC,
				"receiver_sort_code": req.ReceiverSortCode,
				"receiver_account": req.ReceiverAccount,
				"amount":           req.Amount,
				"status":           "SETTLED",
				"scheme":           "CHAPS",
			},
		})
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

func (s *Server) handleUpdateLimit(w http.ResponseWriter, r *http.Request) {
	bic := auth.BICFromContext(r.Context())
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	var req struct {
		OverdraftLimit *float64 `json:"overdraft_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.OverdraftLimit == nil || *req.OverdraftLimit < 0 {
		badRequest(w, "overdraft_limit must be a non-negative number")
		return
	}
	if err := s.Ledger.UpdateOverdraftLimit(r.Context(), bic, *req.OverdraftLimit); err != nil {
		log.Printf("Failed to update overdraft limit for %s: %v", bic, err)
		http.Error(w, "Failed to update limit", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"bic": bic, "overdraft_limit": *req.OverdraftLimit})
}

func (s *Server) handleResolveGridlock(w http.ResponseWriter, r *http.Request) {
	settled, err := s.Ledger.ResolveGridlock(r.Context())
	if err != nil {
		log.Printf("Gridlock resolution failed: %v", err)
		http.Error(w, "Gridlock resolution failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "COMPLETED", "settled": settled})
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

	status, _ := details["status"].(string)
	if status != "PENDING" && status != "QUEUED" {
		http.Error(w, "Only pending or queued payments can be authorized", http.StatusConflict)
		return
	}

	sender, _ := details["sender_bic"].(string)
	receiver, _ := details["receiver_bic"].(string)
	amount, _ := details["amount"].(float64)
	endToEndID, _ := details["end_to_end_id"].(string)
	senderSortCode, _ := details["sender_sort_code"].(string)
	receiverSortCode, _ := details["receiver_sort_code"].(string)

	result, err := s.Ledger.SettlePayment(r.Context(), msgID, sender, receiver, amount, endToEndID, senderSortCode, receiverSortCode, "")
	if err != nil {
		log.Printf("AuthorizePayment settle error: %v", err)
		http.Error(w, "Authorization failed", http.StatusInternalServerError)
		return
	}

	respStatus := "AUTHORIZED"
	if result.Status == "ACTC" {
		respStatus = "SETTLED"
	} else if result.Status == "RJCT" {
		respStatus = "REJECTED"
	} else if result.Status == "PDNG" {
		respStatus = "QUEUED"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"msg_id":  msgID,
		"status":  respStatus,
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
	cfg := loadGlobalSchedule("chaps")
	opening, _ := cfg["opening_time"].(string)
	interbankCutoff, _ := cfg["interbank_cutoff"].(string)
	if opening == "" {
		opening = "06:00"
	}
	if interbankCutoff == "" {
		interbankCutoff = "18:00"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"date":                 time.Now().Format("2006-01-02"),
		"opening_time":         opening,
		"interbank_cutoff":     interbankCutoff,
		"timezone":             "Europe/London",
		"demo_session_minutes": fmt.Sprint(cfg["demo_session_minutes"]),
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
