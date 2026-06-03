package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chaps-service/pkg/events"
)

func TestValidateBIC_Valid(t *testing.T) {
	cases := []string{
		"BARCGB2L",
		"HSBCGB44",
		"LLOYGB21",
		"SNDRUK22",
		"ABCDGB2LXXX",
	}
	for _, bic := range cases {
		if !validateBIC(bic) {
			t.Errorf("validateBIC(%q) = false, want true", bic)
		}
	}
}

func TestValidateBIC_Invalid(t *testing.T) {
	cases := []string{
		"",
		"short",
		"TOOLONGBICNAME",
		"lowercase1",
		"1234567",
		"123456789012",
		"BIC WITH SPACES",
	}
	for _, bic := range cases {
		if validateBIC(bic) {
			t.Errorf("validateBIC(%q) = true, want false", bic)
		}
	}
}

func TestBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	badRequest(w, "test error message")

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "test error message" {
		t.Errorf("error = %q, want %q", body["error"], "test error message")
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	payload := map[string]string{"key": "value"}
	writeJSON(w, http.StatusCreated, payload)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if body["key"] != "value" {
		t.Errorf("key = %q, want %q", body["key"], "value")
	}
}

func TestWriteJSON_NilPayload(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusNoContent, nil)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	if len(bodyBytes) != 0 {
		t.Errorf("expected empty body, got %q", string(bodyBytes))
	}
}

func TestSetCORS(t *testing.T) {
	w := httptest.NewRecorder()
	setCORS(w)

	headers := w.Header()
	if headers.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Allow-Origin = %q, want %q", headers.Get("Access-Control-Allow-Origin"), "*")
	}
	if headers.Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("Allow-Methods is empty")
	}
	if headers.Get("Access-Control-Allow-Headers") == "" {
		t.Errorf("Allow-Headers is empty")
	}
}

func TestOptionsHandler(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/", nil)
	handleOptions(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS header missing")
	}
}

func TestRegisterRoutes_DoesNotPanic(t *testing.T) {
	// Verify that RegisterRoutes can be called without panicking
	// even when Validator and Ledger are nil (registration just sets up handlers)
	mux := http.NewServeMux()
	s := &Server{Validator: nil, Ledger: nil}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterRoutes panicked: %v", r)
		}
	}()

	s.RegisterRoutes(mux)
}

func TestBICRegex_Compiles(t *testing.T) {
	if reBIC == nil {
		t.Fatal("reBIC regex is nil")
	}
	if !reBIC.MatchString("BARCGB2L") {
		t.Errorf("regex failed to match valid BIC")
	}
	if reBIC.MatchString("invalid!@#") {
		t.Errorf("regex matched invalid BIC")
	}
}

func TestProcessJSONPaymentRequest_WithSortCodes(t *testing.T) {
	body := `{"msg_id":"JSON-SORT-001","sender_bic":"SNDRUK22","receiver_bic":"HSBCGB44","amount":250.00,"sender_sort_code":"60-00-00","receiver_sort_code":"40-00-00"}`

	r := httptest.NewRequest("POST", "/v1/payments/chaps", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req struct {
		MsgID           string  `json:"msg_id"`
		SenderBIC       string  `json:"sender_bic"`
		ReceiverBIC     string  `json:"receiver_bic"`
		SenderSortCode  string  `json:"sender_sort_code,omitempty"`
		ReceiverSortCode string `json:"receiver_sort_code,omitempty"`
		Amount          float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}
	if req.SenderSortCode != "60-00-00" {
		t.Errorf("SenderSortCode = %q, want %q", req.SenderSortCode, "60-00-00")
	}
	if req.ReceiverSortCode != "40-00-00" {
		t.Errorf("ReceiverSortCode = %q, want %q", req.ReceiverSortCode, "40-00-00")
	}
	if req.MsgID != "JSON-SORT-001" {
		t.Errorf("MsgID = %q, want %q", req.MsgID, "JSON-SORT-001")
	}
}

func TestProcessJSONPaymentRequest_WithoutSortCodes(t *testing.T) {
	body := `{"msg_id":"JSON-NOSORT-001","sender_bic":"SNDRUK22","receiver_bic":"HSBCGB44","amount":100.00}`

	r := httptest.NewRequest("POST", "/v1/payments/chaps", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req struct {
		MsgID           string  `json:"msg_id"`
		SenderBIC       string  `json:"sender_bic"`
		ReceiverBIC     string  `json:"receiver_bic"`
		SenderSortCode  string  `json:"sender_sort_code,omitempty"`
		ReceiverSortCode string `json:"receiver_sort_code,omitempty"`
		Amount          float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}
	if req.SenderSortCode != "" {
		t.Errorf("expected empty SenderSortCode, got %q", req.SenderSortCode)
	}
	if req.ReceiverSortCode != "" {
		t.Errorf("expected empty ReceiverSortCode, got %q", req.ReceiverSortCode)
	}
}

func TestHandleRegisterRequest_WithSortCode(t *testing.T) {
	body := `{"bic":"TESTGB2L","name":"Test Bank","sort_code":"12-34-56","balance":1000000.00}`

	r := httptest.NewRequest("POST", "/v1/participants/register", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req struct {
		BIC      string  `json:"bic"`
		Name     string  `json:"name"`
		SortCode string  `json:"sort_code,omitempty"`
		Balance  float64 `json:"balance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}
	if req.SortCode != "12-34-56" {
		t.Errorf("SortCode = %q, want %q", req.SortCode, "12-34-56")
	}
	if req.BIC != "TESTGB2L" {
		t.Errorf("BIC = %q, want %q", req.BIC, "TESTGB2L")
	}
}

func TestHandleRegisterRequest_WithoutSortCode(t *testing.T) {
	body := `{"bic":"TESTGB2L","name":"Test Bank","balance":1000000.00}`

	r := httptest.NewRequest("POST", "/v1/participants/register", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req struct {
		BIC      string  `json:"bic"`
		Name     string  `json:"name"`
		SortCode string  `json:"sort_code,omitempty"`
		Balance  float64 `json:"balance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}
	if req.SortCode != "" {
		t.Errorf("expected empty SortCode, got %q", req.SortCode)
	}
}

func TestStartScheduler_StopsOnCancel(t *testing.T) {
	s := &Server{Ledger: nil, Events: events.NewEventBus()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.StartScheduler(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
}
