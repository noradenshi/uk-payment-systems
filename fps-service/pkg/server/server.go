package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fps-service/pkg/auth"
	"fps-service/pkg/events"
	"fps-service/pkg/iso20022"
	"fps-service/pkg/iso8583"
	"fps-service/pkg/ledger"
	"fps-service/pkg/validator"
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

	mux.HandleFunc("POST /v1/payments/fps", s.authMiddleware(s.ProcessPayment))
	mux.HandleFunc("GET /v1/payments/fps", s.handleListPayments)
	mux.HandleFunc("POST /v1/payments/fps/validate", s.handleValidatePayment)
	mux.HandleFunc("GET /v1/payments/fps/limits", s.handleGetLimits)
	mux.HandleFunc("PATCH /v1/payments/fps/limits", s.authMiddleware(s.handleUpdateLimit))
	mux.HandleFunc("PATCH /v1/payments/fps/limits/{bic}", s.handleUpdateLimit)
	mux.HandleFunc("POST /v1/payments/fps/gridlock/resolve", s.handleResolveGridlock)
	mux.HandleFunc("GET /v1/payments/fps/{id}", s.GetPayment)
	mux.HandleFunc("DELETE /v1/payments/fps/{id}", s.handleCancelPayment)

	mux.HandleFunc("POST /v1/payments/fps/forward-dated", s.authMiddleware(s.handleCreateForwardDated))
	mux.HandleFunc("GET /v1/payments/fps/forward-dated", s.handleListForwardDated)
	mux.HandleFunc("DELETE /v1/payments/fps/forward-dated/{id}", s.handleCancelForwardDated)

	mux.HandleFunc("POST /v1/payments/fps/standing-orders", s.authMiddleware(s.handleCreateStandingOrder))
	mux.HandleFunc("GET /v1/payments/fps/standing-orders", s.handleListStandingOrders)
	mux.HandleFunc("GET /v1/payments/fps/standing-orders/{id}", s.handleGetStandingOrder)
	mux.HandleFunc("PATCH /v1/payments/fps/standing-orders/{id}", s.handleUpdateStandingOrder)
	mux.HandleFunc("DELETE /v1/payments/fps/standing-orders/{id}", s.handleCancelStandingOrder)

	mux.HandleFunc("POST /v1/payments/fps/bulk", s.authMiddleware(s.handleCreateBulkSubmission))
	mux.HandleFunc("GET /v1/payments/fps/bulk/{id}", s.handleGetBulkSubmission)
	mux.HandleFunc("GET /v1/payments/fps/bulk", s.handleListBulkSubmissions)

	mux.HandleFunc("GET /v1/settlement/dns/cycle", s.handleGetCurrentDNS)
	mux.HandleFunc("POST /v1/settlement/dns/close", s.handleCloseDNSCycle)
	mux.HandleFunc("GET /v1/settlement/dns/history", s.handleGetDNSHistory)

	mux.HandleFunc("POST /v1/liquidity/top-up", s.authMiddleware(s.handleTopUp))
	mux.HandleFunc("GET /v1/liquidity/prefunded", s.authMiddleware(s.handleGetPrefunded))

	mux.HandleFunc("GET /v1/payments/fps/incoming", s.authMiddleware(s.handleEvents))

	mux.HandleFunc("GET /v1/system/schedule", s.handleSystemSchedule)

	mux.HandleFunc("POST /v1/payments/fps/iso8583", s.authMiddleware(s.handleISO8583Payment))
	mux.HandleFunc("GET /v1/payments/fps/iso8583/decode", s.handleISO8583Decode)

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

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func badRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": message})
}

func notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "FPS"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	bic := auth.BICFromContext(r.Context())
	if bic == "" || !validateBIC(bic) {
		badRequest(w, "Authentication required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, "streaming not supported", http.StatusInternalServerError)
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

func checkOperatingHours(cfg map[string]interface{}, now time.Time) error {
	openStr, _ := cfg["opening_time"].(string)
	closeStr, _ := cfg["closing_time"].(string)
	if openStr == "" {
		openStr = "00:00"
	}
	if closeStr == "" {
		closeStr = "23:59"
	}
	openTime, err1 := time.Parse("15:04", openStr)
	closeTime, err2 := time.Parse("15:04", closeStr)
	if err1 != nil || err2 != nil {
		return nil
	}
	nowMinutes := now.Hour()*60 + now.Minute()
	openMinutes := openTime.Hour()*60 + openTime.Minute()
	closeMinutes := closeTime.Hour()*60 + closeTime.Minute()

	if openMinutes <= closeMinutes {
		if nowMinutes < openMinutes || nowMinutes >= closeMinutes {
			return fmt.Errorf("service closed: %s-%s", openStr, closeStr)
		}
	} else {
		if nowMinutes >= closeMinutes && nowMinutes < openMinutes {
			return fmt.Errorf("service closed: %s-%s", openStr, closeStr)
		}
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
		log.Printf("Query error for %s: %v", msgID, err)
		jsonError(w, "Payment not found", http.StatusNotFound)
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
		jsonError(w, "Failed to list payments", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

func (s *Server) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	participants, err := s.Ledger.ListParticipants(r.Context())
	if err != nil {
		log.Printf("Failed to list participants: %v", err)
		jsonError(w, "Failed to list participants", http.StatusInternalServerError)
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
		jsonError(w, "Failed to update participant", http.StatusInternalServerError)
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
		Name            string  `json:"name"`
		SortCode        string  `json:"sort_code"`
		Balance         float64 `json:"balance"`
		ParticipantType string  `json:"participant_type"`
		SponsorBic      string  `json:"sponsor_bic"`
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
	if req.ParticipantType == "" {
		req.ParticipantType = "DIRECT"
	}
	if req.ParticipantType != "DIRECT" && req.ParticipantType != "INDIRECT" {
		badRequest(w, "participant_type must be DIRECT or INDIRECT")
		return
	}
	if err := 	s.Ledger.UpdateParticipant(r.Context(), bic, req.Name, req.SortCode, req.Balance, req.ParticipantType, req.SponsorBic); err != nil {
		if errors.Is(err, ledger.ErrAccountNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "participant not found"})
			return
		}
		log.Printf("Failed to update participant %s: %v", bic, err)
		jsonError(w, "Failed to update participant", http.StatusInternalServerError)
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
		jsonError(w, "Failed to delete participant", http.StatusInternalServerError)
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
			jsonError(w, "Participant not found", http.StatusNotFound)
			return
		}
		log.Printf("Block details error for %s: %v", bic, err)
		jsonError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC             string  `json:"bic"`
		Name            string  `json:"name"`
		SortCode        string  `json:"sort_code"`
		Balance         float64 `json:"balance"`
		ParticipantType string  `json:"participant_type"`
		SponsorBic      string  `json:"sponsor_bic"`
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
	if req.ParticipantType == "" {
		req.ParticipantType = "DIRECT"
	}
	if req.ParticipantType != "DIRECT" && req.ParticipantType != "INDIRECT" {
		badRequest(w, "participant_type must be DIRECT or INDIRECT")
		return
	}
	apiKey, err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.SortCode, req.Balance, req.ParticipantType, req.SponsorBic)
	if err != nil {
		log.Printf("Failed to register participant %s: %v", req.BIC, err)
		jsonError(w, "Failed to create participant", http.StatusInternalServerError)
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
		jsonError(w, "Participant not found", http.StatusNotFound)
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
		jsonError(w, "Failed to update liquidity", http.StatusInternalServerError)
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
		jsonError(w, "Internal Server Error", http.StatusInternalServerError)
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
		jsonError(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "ACTIVE"})
}

func (s *Server) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	cfg := loadGlobalSchedule("fps")
	if err := checkOperatingHours(cfg, time.Now()); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusServiceUnavailable)
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

	if strings.Contains(contentType, "application/octet-stream") {
		s.processISO8583Payment(w, r)
		return
	}

		jsonError(w, "Unsupported Media Type: use application/json, application/xml, or application/octet-stream", http.StatusUnsupportedMediaType)
}

func (s *Server) processXMLPayment(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("IO Error reading request body: %v", err)
		jsonError(w, "Internal Server Error", http.StatusInternalServerError)
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

	res, err := s.Ledger.SettleSIP(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount, msg.EndToEndId, msg.SenderSortCode, msg.DestSortCode, msg.GetDebtorAccount(), msg.GetCreditorAccount())
	if err != nil {
		log.Printf("[CRITICAL] Ledger system failure for MsgId %s: %v", msg.MsgId, err)
		jsonError(w, "Service Unavailable", http.StatusServiceUnavailable)
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
				"receiver_account": msg.GetCreditorAccount(),
				"amount":           msg.Amount,
				"status":           "SETTLED",
				"scheme":           "FPS",
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

	msgID := req.MsgID
	if idKey := r.Header.Get("Idempotency-Key"); idKey != "" {
		if msgID != "" && msgID != idKey {
			jsonError(w, "msg_id in body differs from Idempotency-Key header", http.StatusConflict)
			return
		}
		msgID = idKey
	}
	if msgID == "" || req.ReceiverBIC == "" || req.Amount <= 0 {
		badRequest(w, "msg_id (or Idempotency-Key), receiver_bic, and positive amount are required")
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
	if len(msgID) > 35 {
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
		jsonError(w, "Sender not found", http.StatusInternalServerError)
		return
	}

	res, err := s.Ledger.SettleSIP(r.Context(), msgID, senderBic, req.ReceiverBIC, req.Amount, req.EndToEndID, senderSortCode, req.ReceiverSortCode, "", req.ReceiverAccount)
	if err != nil {
		log.Printf("[CRITICAL] Ledger system failure for MsgId %s: %v", msgID, err)
		jsonError(w, "Service Unavailable", http.StatusServiceUnavailable)
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
				"msg_id":           msgID,
				"sender":           senderBic,
				"receiver":         req.ReceiverBIC,
				"receiver_sort_code": req.ReceiverSortCode,
				"receiver_account": req.ReceiverAccount,
				"amount":           req.Amount,
				"status":           "SETTLED",
				"scheme":           "FPS",
			},
		})
	}

	w.Header().Set("X-Transaction-Status", res.Status)
	writeJSON(w, httpStatus, map[string]string{
		"msg_id":      msgID,
		"status":      status,
		"iso_status":  res.Status,
		"reason_code": res.ReasonCode,
	})
}

func (s *Server) processISO8583Payment(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		badRequest(w, "Failed to read body")
		return
	}
	defer r.Body.Close()

	msg, err := iso8583.ParseISO8583(body)
	if err != nil {
		badRequest(w, fmt.Sprintf("ISO 8583 parse error: %v", err))
		return
	}

	if msg.DE32_Acquirer == "" || msg.DE100_Receiver == "" || msg.DE4_Amount <= 0 {
		badRequest(w, "Missing DE32 (acquirer), DE100 (receiver), or DE4 (amount)")
		return
	}

	authBic := auth.BICFromContext(r.Context())
	if authBic != "" && msg.DE32_Acquirer != authBic {
		badRequest(w, "Sender BIC mismatch with authentication")
		return
	}

	amount := float64(msg.DE4_Amount) / 100.0
	msgID := r.Header.Get("Idempotency-Key")
	if msgID == "" {
		msgID = fmt.Sprintf("ISO8583-%s-%06d", time.Now().Format("20060102"), msg.DE11_Trace)
	}

	var senderSort, receiverSort string
	s.Ledger.Pool.QueryRow(r.Context(), "SELECT sort_code FROM participant_profiles WHERE bic_code=$1", msg.DE32_Acquirer).Scan(&senderSort)
	s.Ledger.Pool.QueryRow(r.Context(), "SELECT sort_code FROM participant_profiles WHERE bic_code=$1", msg.DE100_Receiver).Scan(&receiverSort)

	res, err := s.Ledger.SettleSIP(r.Context(), msgID, msg.DE32_Acquirer, msg.DE100_Receiver, amount, msgID, senderSort, receiverSort, msg.DE102_SourceAccount, msg.DE103_DestAccount)
	if err != nil {
		log.Printf("[CRITICAL] ISO8583 ledger failure for trace %d: %v", msg.DE11_Trace, err)
		jsonError(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	if res.Status == "ACTC" {
		s.Events.Publish(msg.DE100_Receiver, events.Event{
			Type: "payment.received",
			Data: map[string]interface{}{
				"msg_id":             msgID,
				"sender":             msg.DE32_Acquirer,
				"receiver":           msg.DE100_Receiver,
				"receiver_sort_code": receiverSort,
				"receiver_account":   msg.DE103_DestAccount,
				"amount":             amount,
				"status":             "SETTLED",
				"scheme":             "FPS",
			},
		})
	}

	respCode := "00"
	httpStatus := http.StatusOK
	if res.Status == "RJCT" {
		respCode = "57"
		httpStatus = http.StatusAccepted
	} else if res.Status == "PDNG" {
		respCode = "51"
		httpStatus = http.StatusAccepted
	}

	resp := &iso8583.Message0210{
		DE39_RespCode:       respCode,
		DE4_Amount:          msg.DE4_Amount,
		DE11_Trace:          msg.DE11_Trace,
		DE32_Acquirer:       msg.DE32_Acquirer,
		DE100_Receiver:      msg.DE100_Receiver,
		DE102_SourceAccount: msg.DE102_SourceAccount,
		DE103_DestAccount:   msg.DE103_DestAccount,
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Transaction-Status", res.Status)
	w.WriteHeader(httpStatus)
	w.Write(resp.Encode())

	log.Printf("ISO8583 trace=%d amount=%.2f %s->%s status=%s code=%s", msg.DE11_Trace, amount, msg.DE32_Acquirer, msg.DE100_Receiver, res.Status, respCode)
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
	if !validateBIC(req.SenderBIC) || !validateBIC(req.ReceiverBIC) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": "Invalid BIC format",
		})
		return
	}
	if req.Amount <= 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": "Amount must be positive",
		})
		return
	}
	if req.Amount > 1000000.00 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": "Amount exceeds single payment limit",
		})
		return
	}

	checks := []string{"bic_format", "positive_amount", "limit_check"}
	ctx := r.Context()

	var senderExists, receiverExists bool
	s.Ledger.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM participant_profiles WHERE bic_code=$1)", req.SenderBIC).Scan(&senderExists)
	s.Ledger.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM participant_profiles WHERE bic_code=$1)", req.ReceiverBIC).Scan(&receiverExists)
	if !senderExists || !receiverExists {
		reason := "Receiver not found"
		if !senderExists && !receiverExists {
			reason = "Sender and receiver not found"
		} else if !senderExists {
			reason = "Sender not found"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": reason,
		})
		return
	}
	checks = append(checks, "participant_exists")

	var senderActive bool
	s.Ledger.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM participant_statuses WHERE bic_code=$1 AND status='ACTIVE' AND is_closed=false)", req.SenderBIC).Scan(&senderActive)
	if !senderActive {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": "Sender is not active or is closed",
		})
		return
	}
	checks = append(checks, "sender_active")

	var balance, overdraft float64
	s.Ledger.Pool.QueryRow(ctx, "SELECT COALESCE(l.balance,0), COALESCE(st.overdraft_limit,0) FROM participant_liquidity l JOIN participant_statuses st ON st.bic_code=l.bic_code WHERE l.bic_code=$1", req.SenderBIC).Scan(&balance, &overdraft)
	if balance+overdraft < req.Amount {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"valid": false, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
			"amount": req.Amount, "reason": "Insufficient liquidity",
		})
		return
	}
	checks = append(checks, "sufficient_liquidity")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid": true, "sender_bic": req.SenderBIC, "receiver_bic": req.ReceiverBIC,
		"amount": req.Amount, "checks_passed": checks,
	})
}

func (s *Server) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	bic := r.URL.Query().Get("bic")
	if bic != "" && !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	limits, err := s.Ledger.GetFPSLimits(r.Context(), strings.ToUpper(bic))
	if err != nil {
		jsonError(w, "Limits unavailable", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (s *Server) handleUpdateLimit(w http.ResponseWriter, r *http.Request) {
	bic := strings.ToUpper(r.PathValue("bic"))
	if bic == "" {
		bic = auth.BICFromContext(r.Context())
	}
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
		jsonError(w, "Failed to update limit", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"bic": bic, "status": "LIMITS_UPDATED", "overdraft_limit": *req.OverdraftLimit})
}

func (s *Server) handleResolveGridlock(w http.ResponseWriter, r *http.Request) {
	settled, err := s.Ledger.ResolveGridlock(r.Context())
	if err != nil {
		log.Printf("Gridlock resolution failed: %v", err)
		jsonError(w, "Gridlock resolution failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "COMPLETED", "settled": settled})
}

func (s *Server) handleCancelPayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}
	cancelled, err := s.Ledger.RecallPayment(r.Context(), msgID)
	if err != nil {
		jsonError(w, "Recall failed", http.StatusInternalServerError)
		return
	}
	if !cancelled {
		jsonError(w, "Payment cannot be cancelled unless it is PENDING or QUEUED", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg_id": msgID, "status": "CANCELLED"})
}

func (s *Server) handleCreateForwardDated(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MsgID       string  `json:"msg_id"`
		ReceiverBIC string  `json:"receiver_bic"`
		Amount      float64 `json:"amount"`
		ExecDate    string  `json:"execution_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.MsgID == "" || !validateBIC(req.ReceiverBIC) || req.Amount <= 0 || req.ExecDate == "" {
		badRequest(w, "Missing required fields")
		return
	}
	senderBic := auth.BICFromContext(r.Context())
	execDate, err := time.Parse("2006-01-02", req.ExecDate)
	if err != nil {
		badRequest(w, "Invalid execution_date format, use YYYY-MM-DD")
		return
	}
	if err := s.Ledger.CreateForwardDated(r.Context(), req.MsgID, senderBic, req.ReceiverBIC, req.Amount, execDate); err != nil {
		log.Printf("Failed to create forward dated: %v", err)
		jsonError(w, "Failed to create forward dated payment", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"msg_id": req.MsgID, "status": "SCHEDULED"})
}

func (s *Server) handleListForwardDated(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListForwardDated(r.Context())
	if err != nil {
		jsonError(w, "Failed to list forward dated payments", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCancelForwardDated(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing ID")
		return
	}
	removed, err := s.Ledger.CancelForwardDated(r.Context(), id)
	if err != nil {
		jsonError(w, "Failed to cancel", http.StatusInternalServerError)
		return
	}
	if !removed {
		jsonError(w, "Not found or already executed", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "CANCELLED"})
}

func (s *Server) handleCreateStandingOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference   string  `json:"reference"`
		ReceiverBIC string  `json:"receiver_bic"`
		Amount      float64 `json:"amount"`
		Frequency   string  `json:"frequency"`
		NextDate    string  `json:"next_date"`
		EndDate     string  `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Reference == "" || !validateBIC(req.ReceiverBIC) || req.Amount <= 0 || req.Frequency == "" || req.NextDate == "" {
		badRequest(w, "Missing required fields")
		return
	}
	senderBic := auth.BICFromContext(r.Context())
	nextDate, err := time.Parse("2006-01-02", req.NextDate)
	if err != nil {
		badRequest(w, "Invalid next_date format")
		return
	}
	var endDate time.Time
	if req.EndDate != "" {
		endDate, err = time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			badRequest(w, "Invalid end_date format")
			return
		}
	}
	if err := s.Ledger.CreateStandingOrder(r.Context(), req.Reference, senderBic, req.ReceiverBIC, req.Amount, req.Frequency, nextDate, endDate); err != nil {
		log.Printf("Failed to create standing order: %v", err)
		jsonError(w, "Failed to create standing order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"reference": req.Reference, "status": "ACTIVE"})
}

func (s *Server) handleListStandingOrders(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListStandingOrders(r.Context())
	if err != nil {
		jsonError(w, "Failed to list standing orders", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetStandingOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing ID")
		return
	}
	item, err := s.Ledger.GetStandingOrder(r.Context(), id)
	if err != nil {
		jsonError(w, "Standing order not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateStandingOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing ID")
		return
	}
	var req struct {
		Frequency string  `json:"frequency"`
		Amount    float64 `json:"amount"`
		NextDate  string  `json:"next_date"`
		EndDate   string  `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	var nextDate, endDate time.Time
	var err error
	if req.NextDate != "" {
		nextDate, err = time.Parse("2006-01-02", req.NextDate)
		if err != nil {
			badRequest(w, "Invalid next_date format")
			return
		}
	}
	if req.EndDate != "" {
		endDate, err = time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			badRequest(w, "Invalid end_date format")
			return
		}
	}
	if err := s.Ledger.UpdateStandingOrder(r.Context(), id, req.Frequency, req.Amount, nextDate, endDate); err != nil {
		jsonError(w, "Failed to update standing order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "UPDATED"})
}

func (s *Server) handleCancelStandingOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing ID")
		return
	}
	if err := s.Ledger.CancelStandingOrder(r.Context(), id); err != nil {
		jsonError(w, "Failed to cancel standing order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "CANCELLED"})
}

func (s *Server) handleCreateBulkSubmission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename   string  `json:"filename"`
		TotalItems int     `json:"total_items"`
		TotalValue float64 `json:"total_value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Filename == "" || req.TotalItems <= 0 || req.TotalValue <= 0 {
		badRequest(w, "Invalid fields")
		return
	}
	senderBic := auth.BICFromContext(r.Context())
	id, err := s.Ledger.CreateBulkSubmission(r.Context(), req.Filename, senderBic, req.TotalItems, req.TotalValue)
	if err != nil {
		jsonError(w, "Failed to create bulk submission", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "RECEIVED"})
}

func (s *Server) handleGetBulkSubmission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing ID")
		return
	}
	sub, err := s.Ledger.GetBulkSubmission(r.Context(), id)
	if err != nil {
		jsonError(w, "Bulk submission not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleListBulkSubmissions(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListBulkSubmissions(r.Context())
	if err != nil {
		jsonError(w, "Failed to list bulk submissions", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetCurrentDNS(w http.ResponseWriter, r *http.Request) {
	cycle, err := s.Ledger.GetCurrentDNS(r.Context())
	if err != nil {
		jsonError(w, "No open DNS cycle", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, cycle)
}

func (s *Server) handleCloseDNSCycle(w http.ResponseWriter, r *http.Request) {
	netResults, err := s.Ledger.CloseDNSCycle(r.Context())
	if err != nil {
		jsonError(w, fmt.Sprintf("DNS close failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "CLOSED", "net_positions": netResults})
}

func (s *Server) handleGetDNSHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.Ledger.GetDNSHistory(r.Context())
	if err != nil {
		jsonError(w, "Failed to get DNS history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleGetPrefunded(w http.ResponseWriter, r *http.Request) {
	bic := auth.BICFromContext(r.Context())
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	bal, err := s.Ledger.GetPrefundedBalance(r.Context(), bic)
	if err != nil {
		jsonError(w, "Participant not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bal)
}

func (s *Server) handleSystemSchedule(w http.ResponseWriter, r *http.Request) {
	cfg := loadGlobalSchedule("fps")
	opening, _ := cfg["opening_time"].(string)
	closing, _ := cfg["closing_time"].(string)
	settlementTimes, ok := cfg["settlement_times"].([]interface{})
	if opening == "" {
		opening = "00:00"
	}
	if closing == "" {
		closing = "23:59"
	}
	times := []string{"03:00", "09:00", "12:00", "15:00", "18:00", "21:00"}
	if ok && len(settlementTimes) > 0 {
		times = []string{}
		for _, item := range settlementTimes {
			if value, ok := item.(string); ok {
				times = append(times, value)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"date":                 time.Now().Format("2006-01-02"),
		"opening_time":         opening,
		"closing_time":         closing,
		"settlement_times":     times,
		"timezone":             "Europe/London",
		"demo_session_minutes": cfg["demo_session_minutes"],
	})
}

func (s *Server) handleISO8583Payment(w http.ResponseWriter, r *http.Request) {
	s.processISO8583Payment(w, r)
}

func (s *Server) handleISO8583Decode(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		badRequest(w, "Failed to read body")
		return
	}
	defer r.Body.Close()

	msg, err := iso8583.ParseISO8583(body)
	if err != nil {
		badRequest(w, fmt.Sprintf("ISO 8583 parse error: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleISO8583TCP(conn net.Conn) {
	defer conn.Close()

	var length uint16
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		log.Printf("ISO8583 TCP: failed to read length: %v", err)
		return
	}

	if length > 4096 {
		log.Printf("ISO8583 TCP: message too large: %d", length)
		return
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(conn, body); err != nil {
		log.Printf("ISO8583 TCP: failed to read body: %v", err)
		return
	}

	msg, err := iso8583.ParseISO8583(body)
	if err != nil {
		log.Printf("ISO8583 TCP: parse error: %v", err)
		return
	}

	if msg.DE32_Acquirer == "" || msg.DE100_Receiver == "" || msg.DE4_Amount <= 0 {
		log.Printf("ISO8583 TCP: missing required fields")
		return
	}

	cfg := loadGlobalSchedule("fps")
	if err := checkOperatingHours(cfg, time.Now()); err != nil {
		log.Printf("ISO8583 TCP: %v", err)
		resp := &iso8583.Message0210{
			DE39_RespCode:       "91",
			DE4_Amount:          msg.DE4_Amount,
			DE11_Trace:          msg.DE11_Trace,
			DE32_Acquirer:       msg.DE32_Acquirer,
			DE100_Receiver:      msg.DE100_Receiver,
			DE102_SourceAccount: msg.DE102_SourceAccount,
			DE103_DestAccount:   msg.DE103_DestAccount,
		}
		encoded := resp.Encode()
		binary.Write(conn, binary.BigEndian, uint16(len(encoded)))
		conn.Write(encoded)
		return
	}

	amount := float64(msg.DE4_Amount) / 100.0
	msgID := fmt.Sprintf("ISO8583-%s-%06d", time.Now().Format("20060102"), msg.DE11_Trace)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var senderSort, receiverSort string
	s.Ledger.Pool.QueryRow(ctx, "SELECT sort_code FROM participant_profiles WHERE bic_code=$1", msg.DE32_Acquirer).Scan(&senderSort)
	s.Ledger.Pool.QueryRow(ctx, "SELECT sort_code FROM participant_profiles WHERE bic_code=$1", msg.DE100_Receiver).Scan(&receiverSort)

	res, err := s.Ledger.SettleSIP(ctx, msgID, msg.DE32_Acquirer, msg.DE100_Receiver, amount, msgID, senderSort, receiverSort, msg.DE102_SourceAccount, msg.DE103_DestAccount)
	if err != nil {
		log.Printf("ISO8583 TCP: ledger failure: %v", err)
		return
	}

	if res.Status == "ACTC" {
		s.Events.Publish(msg.DE100_Receiver, events.Event{
			Type: "payment.received",
			Data: map[string]interface{}{
				"msg_id":             msgID,
				"sender":             msg.DE32_Acquirer,
				"receiver":           msg.DE100_Receiver,
				"receiver_sort_code": receiverSort,
				"receiver_account":   msg.DE103_DestAccount,
				"amount":             amount,
				"status":             "SETTLED",
				"scheme":             "FPS",
			},
		})
	}

	respCode := "00"
	if res.Status == "RJCT" {
		respCode = "57"
	} else if res.Status == "PDNG" {
		respCode = "51"
	}

	resp := &iso8583.Message0210{
		DE39_RespCode:       respCode,
		DE4_Amount:          msg.DE4_Amount,
		DE11_Trace:          msg.DE11_Trace,
		DE32_Acquirer:       msg.DE32_Acquirer,
		DE100_Receiver:      msg.DE100_Receiver,
		DE102_SourceAccount: msg.DE102_SourceAccount,
		DE103_DestAccount:   msg.DE103_DestAccount,
	}

	encoded := resp.Encode()
	binary.Write(conn, binary.BigEndian, uint16(len(encoded)))
	if _, err := conn.Write(encoded); err != nil {
		log.Printf("ISO8583 TCP: failed to write response: %v", err)
	}

	log.Printf("ISO8583 TCP trace=%d amount=%.2f %s->%s status=%s code=%s", msg.DE11_Trace, amount, msg.DE32_Acquirer, msg.DE100_Receiver, res.Status, respCode)
}

func (s *Server) StartISO8583Socket(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ISO8583 socket listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Printf("ISO8583 TCP socket listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("ISO8583 TCP accept error: %v", err)
			continue
		}
		go s.handleISO8583TCP(conn)
	}
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
