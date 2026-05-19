package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type topUpRequest struct {
	System string  `json:"system"`
	BIC    string  `json:"bic"`
	Amount float64 `json:"amount"`
	Source string  `json:"source"`
}

func serviceURL(system string) string {
	switch strings.ToLower(system) {
	case "chaps":
		if v := os.Getenv("CHAPS_URL"); v != "" {
			return v
		}
		return "http://chaps-app:8080"
	case "fps":
		if v := os.Getenv("FPS_URL"); v != "" {
			return v
		}
		return "http://fps-app:8081"
	case "bacs":
		if v := os.Getenv("BACS_URL"); v != "" {
			return v
		}
		return "http://bacs-app:8082"
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func handleOptions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func handleTopUp(w http.ResponseWriter, r *http.Request) {
	var req topUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.System = strings.ToLower(req.System)
	req.BIC = strings.ToUpper(req.BIC)
	if serviceURL(req.System) == "" || req.BIC == "" || req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "system, bic and positive amount are required"})
		return
	}
	if req.Source == "" {
		req.Source = "BANK_OF_ENGLAND"
	}

	payload, _ := json.Marshal(map[string]interface{}{"bic": req.BIC, "amount": req.Amount, "source": req.Source})
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(serviceURL(req.System)+"/v1/liquidity/top-up", "application/json", bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"error": "target system rejected top-up", "target_status": resp.StatusCode})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "COMPLETED",
		"system": req.System,
		"bic":    req.BIC,
		"amount": req.Amount,
		"source": req.Source,
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("OPTIONS /", handleOptions)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("POST /v1/central-bank/top-up", handleTopUp)

	addr := ":8090"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	fmt.Printf("Central bank service starting on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
