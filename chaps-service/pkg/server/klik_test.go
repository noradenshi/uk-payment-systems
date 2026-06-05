package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKlikRef(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc123", "CHAPS-ABC123"},
		{"a", "CHAPS-A"},
		{"12345678901234567890", "CHAPS-123456789012"},
	}
	for _, tt := range tests {
		got := klikRef(tt.input)
		if got != tt.expected {
			t.Errorf("klikRef(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestKlikSettleRequest_HappyPath(t *testing.T) {
	body := `{
		"session_id": "550e8400-e29b-41d4-a716-446655440000",
		"transfer_id": "txn-001",
		"system": "CHAPS",
		"from": "Alice Bank",
		"to": "HSBC",
		"amount": "250.00",
		"currency": "GBP"
	}`

	r := httptest.NewRequest("POST", "/v1/klik/chaps/settle", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req KlikSettleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode KLIK request: %v", err)
	}

	if req.SessionID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("SessionID = %q, want %q", req.SessionID, "550e8400-e29b-41d4-a716-446655440000")
	}
	if req.TransferID != "txn-001" {
		t.Errorf("TransferID = %q, want %q", req.TransferID, "txn-001")
	}
	if req.System != "CHAPS" {
		t.Errorf("System = %q, want %q", req.System, "CHAPS")
	}
	if req.From != "Alice Bank" {
		t.Errorf("From = %q, want %q", req.From, "Alice Bank")
	}
	if req.To != "HSBC" {
		t.Errorf("To = %q, want %q", req.To, "HSBC")
	}
	if req.Amount != "250.00" {
		t.Errorf("Amount = %q, want %q", req.Amount, "250.00")
	}
	if req.Currency != "GBP" {
		t.Errorf("Currency = %q, want %q", req.Currency, "GBP")
	}
}

func TestKlikSettleRequest_Minimal(t *testing.T) {
	body := `{
		"transfer_id": "txn-002",
		"from": "Alice Bank",
		"to": "HSBC",
		"amount": "100.00",
		"currency": "GBP"
	}`

	var req KlikSettleRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&req); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if req.SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", req.SessionID)
	}
	if req.TransferID != "txn-002" {
		t.Errorf("TransferID = %q, want %q", req.TransferID, "txn-002")
	}
}

func TestKlikSettleRequest_MissingFields(t *testing.T) {
	body := `{"amount": "100.00"}`

	var req KlikSettleRequest
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&req); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}

	if req.TransferID != "" {
		t.Errorf("expected empty TransferID, got %q", req.TransferID)
	}
}

func TestKlikSettleResponse_Serialization(t *testing.T) {
	resp := KlikSettleResponse{
		TransferID:    "txn-001",
		Status:        "SUCCESS",
		RTGSReference: "CHAPS-TXN001",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if result["transfer_id"] != "txn-001" {
		t.Errorf("transfer_id = %v, want %q", result["transfer_id"], "txn-001")
	}
	if result["status"] != "SUCCESS" {
		t.Errorf("status = %v, want %q", result["status"], "SUCCESS")
	}
	if result["rtgs_reference"] != "CHAPS-TXN001" {
		t.Errorf("rtgs_reference = %v, want %q", result["rtgs_reference"], "CHAPS-TXN001")
	}
	if _, exists := result["failure_reason"]; exists {
		t.Errorf("failure_reason should be omitted on success")
	}
}

func TestKlikSettleResponse_Failure(t *testing.T) {
	resp := KlikSettleResponse{
		TransferID:    "txn-002",
		Status:        "FAILED",
		FailureReason: "insufficient liquidity",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if result["status"] != "FAILED" {
		t.Errorf("status = %v, want %q", result["status"], "FAILED")
	}
	if result["failure_reason"] != "insufficient liquidity" {
		t.Errorf("failure_reason = %v, want %q", result["failure_reason"], "insufficient liquidity")
	}
}

func TestKlikHealthHandler(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/klik/chaps/healthz", nil)

	s.handleKlikHealth(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want %q", body["status"], "ok")
	}
	if body["system"] != "CHAPS" {
		t.Errorf("system = %q, want %q", body["system"], "CHAPS")
	}
}

func TestKlikRoutes_Registered(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{}
	s.RegisterRoutes(mux)

	// Verify the KLIK routes are registered by making GET /v1/klik/chaps/healthz
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/klik/chaps/healthz", nil)
	mux.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Errorf("KLIK health route returned %d, want 200", w.Result().StatusCode)
	}

	// POST /v1/klik/chaps/settle should not 404
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/v1/klik/chaps/settle", strings.NewReader(`{}`))
	r2.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(w2, r2)

	if w2.Result().StatusCode == http.StatusNotFound {
		t.Error("KLIK settle route was not found (404)")
	}
}

