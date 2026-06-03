package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessJSONPaymentRequest_WithSortCodes(t *testing.T) {
	body := `{"msg_id":"FPS-SORT-001","sender_bic":"SNDRUK22","receiver_bic":"HSBCGB44","amount":250.00,"sender_sort_code":"60-00-00","receiver_sort_code":"40-00-00"}`

	r := httptest.NewRequest("POST", "/v1/payments/fps", strings.NewReader(body))
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
	if req.MsgID != "FPS-SORT-001" {
		t.Errorf("MsgID = %q, want %q", req.MsgID, "FPS-SORT-001")
	}
}

func TestProcessJSONPaymentRequest_WithoutSortCodes(t *testing.T) {
	body := `{"msg_id":"FPS-NOSORT-001","sender_bic":"SNDRUK22","receiver_bic":"HSBCGB44","amount":100.00}`

	r := httptest.NewRequest("POST", "/v1/payments/fps", strings.NewReader(body))
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
	body := `{"bic":"TESTGB2L","name":"Test Bank","sort_code":"12-34-56","balance":1000000.00,"participant_type":"DIRECT"}`

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
	body := `{"bic":"TESTGB2L","name":"Test Bank","balance":1000000.00,"participant_type":"DIRECT"}`

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
