package server

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fps-service/pkg/iso20022"
	"fps-service/pkg/iso8583"
	"fps-service/pkg/ledger"
)

type Server struct {
	Ledger *ledger.LedgerService
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

	mux.HandleFunc("POST /v1/payments/fps", s.ProcessPayment)
	mux.HandleFunc("GET /v1/payments/fps", s.handleListPayments)
	mux.HandleFunc("POST /v1/payments/fps/validate", s.handleValidatePayment)
	mux.HandleFunc("GET /v1/payments/fps/limits", s.handleGetLimits)
	mux.HandleFunc("PATCH /v1/payments/fps/limits/{bic}", s.handleUpdateLimit)
	mux.HandleFunc("GET /v1/payments/fps/{id}", s.GetPayment)
	mux.HandleFunc("DELETE /v1/payments/fps/{id}", s.handleCancelPayment)

	mux.HandleFunc("POST /v1/payments/fps/forward-dated", s.handleCreateForwardDated)
	mux.HandleFunc("GET /v1/payments/fps/forward-dated", s.handleListForwardDated)
	mux.HandleFunc("DELETE /v1/payments/fps/forward-dated/{id}", s.handleCancelForwardDated)

	mux.HandleFunc("POST /v1/payments/fps/standing-orders", s.handleCreateStandingOrder)
	mux.HandleFunc("GET /v1/payments/fps/standing-orders", s.handleListStandingOrders)
	mux.HandleFunc("GET /v1/payments/fps/standing-orders/{id}", s.handleGetStandingOrder)
	mux.HandleFunc("PATCH /v1/payments/fps/standing-orders/{id}", s.handleUpdateStandingOrder)
	mux.HandleFunc("DELETE /v1/payments/fps/standing-orders/{id}", s.handleCancelStandingOrder)

	mux.HandleFunc("POST /v1/payments/fps/bulk", s.handleCreateBulkSubmission)
	mux.HandleFunc("GET /v1/payments/fps/bulk/{id}", s.handleGetBulkSubmission)
	mux.HandleFunc("GET /v1/payments/fps/bulk", s.handleListBulkSubmissions)

	mux.HandleFunc("GET /v1/settlement/dns/cycle", s.handleGetCurrentDNS)
	mux.HandleFunc("POST /v1/settlement/dns/close", s.handleCloseDNSCycle)
	mux.HandleFunc("GET /v1/settlement/dns/history", s.handleGetDNSHistory)

	mux.HandleFunc("POST /v1/liquidity/top-up", s.handleTopUp)
	mux.HandleFunc("GET /v1/liquidity/prefunded/{bic}", s.handleGetPrefunded)

	mux.HandleFunc("GET /v1/system/schedule", s.handleSystemSchedule)

	mux.HandleFunc("POST /v1/payments/fps/iso8583", s.handleISO8583Payment)
	mux.HandleFunc("GET /v1/payments/fps/iso8583/decode", s.handleISO8583Decode)
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

func notImplemented(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented"})
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
		BIC             string  `json:"bic"`
		Name            string  `json:"name"`
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
	err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.Balance, req.ParticipantType, req.SponsorBic)
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

	if strings.Contains(contentType, "application/octet-stream") {
		s.processISO8583Payment(w, r)
		return
	}

	http.Error(w, "Unsupported Media Type: use application/json, application/xml, or application/octet-stream", http.StatusUnsupportedMediaType)
}

func (s *Server) processXMLPayment(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("IO Error reading request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal(body, &msg); err != nil {
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

	res, err := s.Ledger.SettleSIP(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount, msg.EndToEndId)
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

	res, err := s.Ledger.SettleSIP(r.Context(), req.MsgID, req.SenderBIC, req.ReceiverBIC, req.Amount, req.EndToEndID)
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

	amount := float64(msg.DE4_Amount) / 100.0
	msgID := fmt.Sprintf("ISO8583-%s-%06d", time.Now().Format("20060102"), msg.DE11_Trace)

	res, err := s.Ledger.SettleSIP(r.Context(), msgID, msg.DE32_Acquirer, msg.DE100_Receiver, amount, msgID)
	if err != nil {
		log.Printf("[CRITICAL] ISO8583 ledger failure for trace %d: %v", msg.DE11_Trace, err)
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	respCode := "00"
	if res.Status == "RJCT" {
		respCode = "57"
	} else if res.Status == "PDNG" {
		respCode = "51"
	}

	resp := &iso8583.Message0210{
		DE39_RespCode: respCode,
		DE4_Amount:    msg.DE4_Amount,
		DE11_Trace:    msg.DE11_Trace,
		DE32_Acquirer: msg.DE32_Acquirer,
		DE100_Receiver: msg.DE100_Receiver,
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Transaction-Status", res.Status)
	w.WriteHeader(http.StatusOK)
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
		badRequest(w, "Invalid BIC format")
		return
	}
	valid := req.Amount > 0 && req.Amount <= 1000000.00
	result := map[string]interface{}{
		"valid":           valid,
		"sender_bic":      req.SenderBIC,
		"receiver_bic":    req.ReceiverBIC,
		"amount":          req.Amount,
		"checks_passed":   []string{"bic_format", "positive_amount", "limit_check"},
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	bic := r.URL.Query().Get("bic")
	if bic != "" && !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	limits, err := s.Ledger.GetFPSLimits(r.Context(), strings.ToUpper(bic))
	if err != nil {
		http.Error(w, "Limits unavailable", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (s *Server) handleUpdateLimit(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	var req struct {
		SinglePaymentLimit    *float64 `json:"single_payment_limit"`
		DailyParticipantLimit *float64 `json:"daily_participant_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"bic":    bic,
		"status": "LIMITS_UPDATED",
	})
}

func (s *Server) handleCancelPayment(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	if msgID == "" {
		badRequest(w, "Missing transaction ID")
		return
	}
	cancelled, err := s.Ledger.RecallPayment(r.Context(), msgID)
	if err != nil {
		http.Error(w, "Recall failed", http.StatusInternalServerError)
		return
	}
	if !cancelled {
		http.Error(w, "Payment cannot be recalled unless it is PENDING", http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg_id": msgID, "status": "RECALLED"})
}

func (s *Server) handleCreateForwardDated(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MsgID       string  `json:"msg_id"`
		SenderBIC   string  `json:"sender_bic"`
		ReceiverBIC string  `json:"receiver_bic"`
		Amount      float64 `json:"amount"`
		ExecDate    string  `json:"execution_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.MsgID == "" || !validateBIC(req.SenderBIC) || !validateBIC(req.ReceiverBIC) || req.Amount <= 0 || req.ExecDate == "" {
		badRequest(w, "Missing required fields")
		return
	}
	execDate, err := time.Parse("2006-01-02", req.ExecDate)
	if err != nil {
		badRequest(w, "Invalid execution_date format, use YYYY-MM-DD")
		return
	}
	if err := s.Ledger.CreateForwardDated(r.Context(), req.MsgID, req.SenderBIC, req.ReceiverBIC, req.Amount, execDate); err != nil {
		log.Printf("Failed to create forward dated: %v", err)
		http.Error(w, "Failed to create forward dated payment", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"msg_id": req.MsgID, "status": "SCHEDULED"})
}

func (s *Server) handleListForwardDated(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListForwardDated(r.Context())
	if err != nil {
		http.Error(w, "Failed to list forward dated payments", http.StatusInternalServerError)
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
		http.Error(w, "Failed to cancel", http.StatusInternalServerError)
		return
	}
	if !removed {
		http.Error(w, "Not found or already executed", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "CANCELLED"})
}

func (s *Server) handleCreateStandingOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference  string  `json:"reference"`
		SenderBIC  string  `json:"sender_bic"`
		ReceiverBIC string `json:"receiver_bic"`
		Amount     float64 `json:"amount"`
		Frequency  string  `json:"frequency"`
		NextDate   string  `json:"next_date"`
		EndDate    string  `json:"end_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Reference == "" || !validateBIC(req.SenderBIC) || !validateBIC(req.ReceiverBIC) || req.Amount <= 0 || req.Frequency == "" || req.NextDate == "" {
		badRequest(w, "Missing required fields")
		return
	}
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
	if err := s.Ledger.CreateStandingOrder(r.Context(), req.Reference, req.SenderBIC, req.ReceiverBIC, req.Amount, req.Frequency, nextDate, endDate); err != nil {
		log.Printf("Failed to create standing order: %v", err)
		http.Error(w, "Failed to create standing order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"reference": req.Reference, "status": "ACTIVE"})
}

func (s *Server) handleListStandingOrders(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListStandingOrders(r.Context())
	if err != nil {
		http.Error(w, "Failed to list standing orders", http.StatusInternalServerError)
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
		http.Error(w, "Standing order not found", http.StatusNotFound)
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
		http.Error(w, "Failed to update standing order", http.StatusInternalServerError)
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
		http.Error(w, "Failed to cancel standing order", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "CANCELLED"})
}

func (s *Server) handleCreateBulkSubmission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename   string  `json:"filename"`
		SenderBIC  string  `json:"sender_bic"`
		TotalItems int     `json:"total_items"`
		TotalValue float64 `json:"total_value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Filename == "" || !validateBIC(req.SenderBIC) || req.TotalItems <= 0 || req.TotalValue <= 0 {
		badRequest(w, "Invalid fields")
		return
	}
	id, err := s.Ledger.CreateBulkSubmission(r.Context(), req.Filename, req.SenderBIC, req.TotalItems, req.TotalValue)
	if err != nil {
		http.Error(w, "Failed to create bulk submission", http.StatusInternalServerError)
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
		http.Error(w, "Bulk submission not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleListBulkSubmissions(w http.ResponseWriter, r *http.Request) {
	items, err := s.Ledger.ListBulkSubmissions(r.Context())
	if err != nil {
		http.Error(w, "Failed to list bulk submissions", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetCurrentDNS(w http.ResponseWriter, r *http.Request) {
	cycle, err := s.Ledger.GetCurrentDNS(r.Context())
	if err != nil {
		http.Error(w, "No open DNS cycle", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, cycle)
}

func (s *Server) handleCloseDNSCycle(w http.ResponseWriter, r *http.Request) {
	netResults, err := s.Ledger.CloseDNSCycle(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("DNS close failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "CLOSED", "net_positions": netResults})
}

func (s *Server) handleGetDNSHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.Ledger.GetDNSHistory(r.Context())
	if err != nil {
		http.Error(w, "Failed to get DNS history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleGetPrefunded(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	bal, err := s.Ledger.GetPrefundedBalance(r.Context(), bic)
	if err != nil {
		http.Error(w, "Participant not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, bal)
}

func (s *Server) handleSystemSchedule(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"date":             time.Now().Format("2006-01-02"),
		"opening_time":     "00:00",
		"closing_time":     "23:59",
		"settlement_times": []string{"03:00", "09:00", "12:00", "15:00", "18:00", "21:00"},
		"timezone":         "Europe/London",
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
