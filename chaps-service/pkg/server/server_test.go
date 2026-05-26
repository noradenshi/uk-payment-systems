package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
