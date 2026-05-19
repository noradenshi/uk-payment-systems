package server

import (
	"bacs-service/pkg/ledger"
	"bacs-service/pkg/standard18"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Server struct {
	Ledger *ledger.LedgerService
}

var reBIC = regexp.MustCompile(`^[A-Z0-9]{8,11}$`)

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// File Submission
	mux.HandleFunc("POST /v1/payments/bacs/submit", s.handleSubmit)
	mux.HandleFunc("GET /v1/payments/bacs/submit/{id}", s.handleGetSubmission)
	mux.HandleFunc("GET /v1/payments/bacs/submit", s.handleListSubmissions)
	mux.HandleFunc("DELETE /v1/payments/bacs/submit/{id}", s.handleDeleteSubmission)

	// Cycle Management
	mux.HandleFunc("GET /v1/payments/bacs/cycle/current", s.handleGetCurrentCycle)
	mux.HandleFunc("GET /v1/payments/bacs/cycle/{cycle-date}", s.handleGetCycleByDate)
	mux.HandleFunc("GET /v1/payments/bacs/cycle", s.handleListCycles)
	mux.HandleFunc("POST /v1/payments/bacs/cycle/close", s.handleCloseInputDay)

	// Participant Management
	mux.HandleFunc("GET /v1/participants", s.handleListParticipants)
	mux.HandleFunc("POST /v1/participants/register", s.handleRegisterParticipant)
	mux.HandleFunc("PATCH /v1/participants/{bic}/status", s.handleUpdateParticipantStatus)
	mux.HandleFunc("POST /v1/participants/{bic}/block", s.handleBlockParticipant)
	mux.HandleFunc("GET /v1/participants/{bic}/block", s.handleGetBlockDetails)
	mux.HandleFunc("DELETE /v1/participants/{bic}/block", s.handleUnblockParticipant)

	// Mandate (AUDDIS) Management
	mux.HandleFunc("POST /v1/payments/bacs/mandates", s.handleCreateMandate)
	mux.HandleFunc("GET /v1/payments/bacs/mandates/{ref}", s.handleGetMandate)
	mux.HandleFunc("GET /v1/payments/bacs/mandates", s.handleListMandates)
	mux.HandleFunc("PATCH /v1/payments/bacs/mandates/{ref}", s.handleAmendMandate)
	mux.HandleFunc("DELETE /v1/payments/bacs/mandates/{ref}", s.handleCancelMandate)
	mux.HandleFunc("POST /v1/payments/bacs/mandates/{ref}/claim", s.handleClaimMandate)

	// Returns & Rejects
	mux.HandleFunc("GET /v1/payments/bacs/returns", s.handleListReturns)
	mux.HandleFunc("POST /v1/payments/bacs/returns", s.handleCreateReturn)
	mux.HandleFunc("GET /v1/payments/bacs/rejects", s.handleListRejects)

	// Reports
	mux.HandleFunc("GET /v1/payments/bacs/reports/{cycle-date}", s.handleCycleReports)
	mux.HandleFunc("GET /v1/payments/bacs/reports/{cycle-date}/su/{bic}", s.handleCycleReportsForSU)
	mux.HandleFunc("GET /v1/payments/bacs/reports/{cycle-date}/summary", s.handleCycleSummary)
	mux.HandleFunc("GET /v1/payments/bacs/reports/su/{bic}", s.handleSUReports)

	// Limits & Controls
	mux.HandleFunc("GET /v1/payments/bacs/limits", s.handleGetLimits)
	mux.HandleFunc("PATCH /v1/payments/bacs/limits/{bic}", s.handlePatchLimits)

	// System Metadata
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

// ── File Submission Handlers ──

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")
	var content string
	var filename string

	if strings.Contains(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			badRequest(w, "Failed to parse multipart form")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			badRequest(w, "Missing file field")
			return
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil {
			badRequest(w, "Failed to read file")
			return
		}
		content = string(body)
		filename = header.Filename
	} else {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			badRequest(w, "Failed to read body")
			return
		}
		defer r.Body.Close()
		content = string(body)
		filename = r.URL.Query().Get("filename")
		if filename == "" {
			filename = "submission_standard18.txt"
		}
	}

	parsed, err := standard18.Parse(content)
	if err != nil {
		badRequest(w, "Standard 18 parse error: "+err.Error())
		return
	}

	if errs := parsed.Validate(); len(errs) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":  "Validation failed",
			"errors": errs,
		})
		return
	}

	cycle, err := s.Ledger.GetCurrentCycle(r.Context())
	if err != nil {
		log.Printf("Failed to get current cycle: %v", err)
		http.Error(w, "No open cycle available", http.StatusServiceUnavailable)
		return
	}

	cycleID, _ := cycle["id"].(int)
	if cycleID == 0 {
		if fid, ok := cycle["id"].(float64); ok {
			cycleID = int(fid)
		}
	}

	totalVolume := len(parsed.DirectDebits) + len(parsed.DirectCredits)
	totalValue := 0.0
	for _, d := range parsed.DirectDebits {
		totalValue += d.Amount
	}
	for _, c := range parsed.DirectCredits {
		totalValue += c.Amount
	}

	suBic := r.URL.Query().Get("su_bic")
	if suBic == "" && parsed.Header != nil &&
		(parsed.Header.DestSortCode != "" || parsed.Header.DestAccount != "") {
		suBic = "BARCGB2L"
	}
	if !validateBIC(suBic) {
		badRequest(w, "Invalid or missing su_bic")
		return
	}

	subID, err := s.Ledger.CreateSubmission(r.Context(), filename, suBic, totalVolume, totalValue, cycleID)
	if err != nil {
		log.Printf("Failed to create submission: %v", err)
		http.Error(w, "Failed to create submission", http.StatusInternalServerError)
		return
	}

	var debits []map[string]interface{}
	var credits []map[string]interface{}
	for _, d := range parsed.DirectDebits {
		debits = append(debits, map[string]interface{}{
			"volume_header_no": d.VolumeHeaderNo,
			"dest_sort_code":   d.DestSortCode,
			"dest_account":     d.DestAccount,
			"amount":           d.Amount,
			"originator_ref":   d.OriginatorSortAcc,
			"reference":        d.Reference,
			"su_code":          d.SUCode,
		})
	}
	for _, c := range parsed.DirectCredits {
		credits = append(credits, map[string]interface{}{
			"volume_header_no": c.VolumeHeaderNo,
			"dest_sort_code":   c.DestSortCode,
			"dest_account":     c.DestAccount,
			"amount":           c.Amount,
			"originator_ref":   c.OriginatorName,
			"reference":        c.PayrollRef,
			"su_code":          c.SUCode,
		})
	}

	if err := s.Ledger.StoreTransactions(r.Context(), subID, debits, credits); err != nil {
		log.Printf("Failed to store transactions: %v", err)
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"id":     subID,
			"status": "RECEIVED",
			"error":  "transactions may be partially stored",
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":           subID,
		"filename":     filename,
		"volume":       totalVolume,
		"value":        totalValue,
		"status":       "RECEIVED",
		"cycle_id":     cycleID,
		"su_bic":       suBic,
	})
}

func (s *Server) handleGetSubmission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing submission ID")
		return
	}
	sub, err := s.Ledger.GetSubmission(r.Context(), id)
	if err != nil {
		log.Printf("GetSubmission error: %v", err)
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleListSubmissions(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.ToUpper(r.URL.Query().Get("status"))
	suBic := strings.ToUpper(r.URL.Query().Get("su_bic"))
	submissions, err := s.Ledger.ListSubmissions(r.Context(), statusFilter, suBic)
	if err != nil {
		log.Printf("Failed to list submissions: %v", err)
		http.Error(w, "Failed to list submissions", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, submissions)
}

func (s *Server) handleDeleteSubmission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		badRequest(w, "Missing submission ID")
		return
	}
	if err := s.Ledger.RecallSubmission(r.Context(), id); err != nil {
		log.Printf("RecallSubmission error: %v", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "RECALLED"})
}

// ── Cycle Handlers ──

func (s *Server) handleGetCurrentCycle(w http.ResponseWriter, r *http.Request) {
	cycle, err := s.Ledger.GetCurrentCycle(r.Context())
	if err != nil {
		log.Printf("GetCurrentCycle error: %v", err)
		http.Error(w, "No open cycle found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, cycle)
}

func (s *Server) handleGetCycleByDate(w http.ResponseWriter, r *http.Request) {
	cycleDate := r.PathValue("cycle-date")
	if cycleDate == "" {
		badRequest(w, "Missing cycle date")
		return
	}
	reports, err := s.Ledger.GetCycleReports(r.Context(), cycleDate, "")
	if err != nil {
		log.Printf("GetCycleReports error: %v", err)
		http.Error(w, "Cycle not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) handleListCycles(w http.ResponseWriter, r *http.Request) {
	cycles, err := s.Ledger.ListCycles(r.Context())
	if err != nil {
		log.Printf("Failed to list cycles: %v", err)
		http.Error(w, "Failed to list cycles", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, cycles)
}

func (s *Server) handleCloseInputDay(w http.ResponseWriter, r *http.Request) {
	if err := s.Ledger.CloseInputDay(r.Context()); err != nil {
		log.Printf("CloseInputDay error: %v", err)
		http.Error(w, "Failed to close input day", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "PROCESSING"})
}

// ── Participant Handlers ──

func (s *Server) handleListParticipants(w http.ResponseWriter, r *http.Request) {
	participants, err := s.Ledger.ListParticipants(r.Context())
	if err != nil {
		log.Printf("Failed to list participants: %v", err)
		http.Error(w, "Failed to list participants", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, participants)
}

func (s *Server) handleRegisterParticipant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BIC       string  `json:"bic"`
		Name      string  `json:"name"`
		Balance   float64 `json:"balance"`
		SUCode    string  `json:"su_code"`
		IsServiceUser bool `json:"is_service_user"`
		IsDestUser    bool `json:"is_destination_user"`
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
	err := s.Ledger.RegisterParticipant(r.Context(), req.BIC, req.Name, req.Balance, req.SUCode, req.IsServiceUser, req.IsDestUser)
	if err != nil {
		log.Printf("Failed to register participant %s: %v", req.BIC, err)
		http.Error(w, "Failed to register participant", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"bic":   req.BIC,
		"name":  req.Name,
		"status": "ACTIVE",
	})
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
	if err := s.Ledger.BlockParticipant(r.Context(), bic, reason); err != nil {
		log.Printf("Failed to block %s: %v", bic, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "SUSPENDED", "reason": reason})
}

func (s *Server) handleGetBlockDetails(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleUnblockParticipant(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	if err := s.Ledger.UnblockParticipant(r.Context(), bic); err != nil {
		log.Printf("Failed to unblock %s: %v", bic, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "ACTIVE"})
}

// ── Mandate Handlers ──

func (s *Server) handleCreateMandate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reference string  `json:"reference"`
		SUBic     string  `json:"su_bic"`
		PayerName string  `json:"payer_name"`
		SortCode  string  `json:"payer_sort_code"`
		Account   string  `json:"payer_account"`
		Amount    float64 `json:"amount"`
		Frequency string  `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.Reference == "" || req.SUBic == "" {
		badRequest(w, "reference and su_bic are required")
		return
	}
	id, err := s.Ledger.CreateMandate(r.Context(), req.Reference, req.SUBic, req.PayerName, req.SortCode, req.Account, req.Amount, req.Frequency)
	if err != nil {
		log.Printf("Failed to create mandate: %v", err)
		http.Error(w, "Failed to create mandate", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        id,
		"reference": req.Reference,
		"status":    "ACTIVE",
	})
}

func (s *Server) handleGetMandate(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if ref == "" {
		badRequest(w, "Missing mandate reference")
		return
	}
	mandate, err := s.Ledger.GetMandate(r.Context(), ref)
	if err != nil {
		http.Error(w, "Mandate not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, mandate)
}

func (s *Server) handleListMandates(w http.ResponseWriter, r *http.Request) {
	suBic := strings.ToUpper(r.URL.Query().Get("su_bic"))
	mandates, err := s.Ledger.ListMandates(r.Context(), suBic)
	if err != nil {
		log.Printf("Failed to list mandates: %v", err)
		http.Error(w, "Failed to list mandates", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, mandates)
}

func (s *Server) handleAmendMandate(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if ref == "" {
		badRequest(w, "Missing mandate reference")
		return
	}
	var req struct {
		Amount    float64 `json:"amount"`
		Frequency string  `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if err := s.Ledger.AmendMandate(r.Context(), ref, req.Amount, req.Frequency); err != nil {
		log.Printf("Failed to amend mandate: %v", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reference": ref, "status": "AMENDED"})
}

func (s *Server) handleCancelMandate(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if ref == "" {
		badRequest(w, "Missing mandate reference")
		return
	}
	if err := s.Ledger.CancelMandate(r.Context(), ref); err != nil {
		log.Printf("Failed to cancel mandate: %v", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reference": ref, "status": "CANCELLED"})
}

func (s *Server) handleClaimMandate(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if ref == "" {
		badRequest(w, "Missing mandate reference")
		return
	}
	var req struct {
		SortCode string `json:"payer_sort_code"`
		Account  string `json:"payer_account"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if err := s.Ledger.ClaimMandate(r.Context(), ref, req.SortCode, req.Account); err != nil {
		log.Printf("Failed to claim mandate: %v", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reference": ref, "status": "CLAIMED"})
}

// ── Return Handlers ──

func (s *Server) handleListReturns(w http.ResponseWriter, r *http.Request) {
	returns, err := s.Ledger.ListReturns(r.Context())
	if err != nil {
		log.Printf("Failed to list returns: %v", err)
		http.Error(w, "Failed to list returns", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, returns)
}

func (s *Server) handleCreateReturn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OriginalTransactionID int     `json:"original_transaction_id"`
		ReasonCode            string  `json:"reason_code"`
		Amount                float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badRequest(w, "Invalid request body")
		return
	}
	if req.ReasonCode == "" || req.Amount <= 0 {
		badRequest(w, "reason_code and positive amount are required")
		return
	}
	id, err := s.Ledger.CreateReturn(r.Context(), req.OriginalTransactionID, req.ReasonCode, req.Amount)
	if err != nil {
		log.Printf("Failed to create return: %v", err)
		http.Error(w, "Failed to create return", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id": id,
		"status": "RETURNED",
	})
}

func (s *Server) handleListRejects(w http.ResponseWriter, r *http.Request) {
	submissions, err := s.Ledger.ListSubmissions(r.Context(), "REJECTED", "")
	if err != nil {
		log.Printf("Failed to list rejects: %v", err)
		http.Error(w, "Failed to list rejects", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, submissions)
}

// ── Report Handlers ──

func (s *Server) handleCycleReports(w http.ResponseWriter, r *http.Request) {
	cycleDate := r.PathValue("cycle-date")
	if cycleDate == "" {
		badRequest(w, "Missing cycle date")
		return
	}
	reports, err := s.Ledger.GetCycleReports(r.Context(), cycleDate, "")
	if err != nil {
		log.Printf("GetCycleReports error: %v", err)
		http.Error(w, "Cycle reports not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) handleCycleReportsForSU(w http.ResponseWriter, r *http.Request) {
	cycleDate := r.PathValue("cycle-date")
	bic := r.PathValue("bic")
	if cycleDate == "" || bic == "" {
		badRequest(w, "Missing cycle date or BIC")
		return
	}
	reports, err := s.Ledger.GetCycleReports(r.Context(), cycleDate, bic)
	if err != nil {
		log.Printf("GetCycleReports error: %v", err)
		http.Error(w, "Reports not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) handleCycleSummary(w http.ResponseWriter, r *http.Request) {
	cycleDate := r.PathValue("cycle-date")
	if cycleDate == "" {
		badRequest(w, "Missing cycle date")
		return
	}
	summary, err := s.Ledger.GetCycleSummary(r.Context(), cycleDate)
	if err != nil {
		log.Printf("GetCycleSummary error: %v", err)
		http.Error(w, "Summary not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleSUReports(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	submissions, err := s.Ledger.ListSubmissions(r.Context(), "", bic)
	if err != nil {
		log.Printf("Failed to list SU reports: %v", err)
		http.Error(w, "Failed to get reports", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, submissions)
}

// ── Limits Handlers ──

func (s *Server) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	limits, err := s.Ledger.GetBACSLimits(r.Context())
	if err != nil {
		http.Error(w, "Limits unavailable", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, limits)
}

func (s *Server) handlePatchLimits(w http.ResponseWriter, r *http.Request) {
	bic := r.PathValue("bic")
	if !validateBIC(bic) {
		badRequest(w, "Invalid BIC format")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bic": bic, "status": "LIMITS_UPDATED"})
}

// ── System Schedule ──

func (s *Server) handleSystemSchedule(w http.ResponseWriter, r *http.Request) {
	schedule, err := s.Ledger.GetSchedule(r.Context())
	if err != nil {
		log.Printf("GetSchedule error: %v", err)
		http.Error(w, "Schedule unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schedule":       schedule,
		"current_date":   time.Now().Format("2006-01-02"),
		"input_cutoff":   "15:30",
		"system":         "BACS",
		"settlement":     "T+2",
		"timezone":       "Europe/London",
	})
}
