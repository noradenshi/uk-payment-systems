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
	"strconv"
	"time"
)

type Server struct {
	Validator *validator.ValidatorRegistry
	Ledger    *ledger.LedgerService
}

type DashboardData struct {
	Metrics  []Metric      `json:"metrics"`
	Accounts []AccountRow  `json:"accounts"`
	Queue    []QueueItem   `json:"queue"`
	Services []ServiceItem `json:"services"`
}

type Metric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AccountRow struct {
	BIC     string `json:"bic"`
	Name    string `json:"name"`
	Balance string `json:"balance"`
}

type QueueItem struct {
	MsgID    string `json:"msgId"`
	Sender   string `json:"sender"`
	Receiver string `json:"receiver"`
	Amount   string `json:"amount"`
	Status   string `json:"status"`
	Time     string `json:"time"`
}

type ServiceItem struct {
	Label  string `json:"label"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

func (s *Server) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	data := DashboardData{
		Metrics: []Metric{
			{Label: "Rozliczone dzis", Value: "0"},
			{Label: "W kolejce", Value: "0"},
			{Label: "Odrzucone", Value: "0"},
			{Label: "Laczna wartosc", Value: "GBP 0.00"},
		},
		Accounts: []AccountRow{},
		Queue:    []QueueItem{},
		Services: []ServiceItem{
			{Label: "API CHAPS", State: "online", Detail: "Nasluch na :8080/pay"},
			{Label: "Ksiega Postgres", State: "online", Detail: "Konta i dziennik gotowe"},
			{Label: "Kolejka rozliczen", State: "online", Detail: "Dane pobrane z bazy"},
		},
	}

	if s.Ledger == nil || s.Ledger.Pool == nil {
		data.Services[1] = ServiceItem{Label: "Ksiega Postgres", State: "offline", Detail: "Brak polaczenia z baza"}
		writeJSON(w, data)
		return
	}

	ctx := r.Context()

	accountRows, err := s.Ledger.Pool.Query(ctx, `
		SELECT bic_code, name, currency, balance
		FROM accounts
		ORDER BY bic_code`)
	if err != nil {
		log.Printf("Dashboard accounts query failed: %v", err)
		data.Services[1] = ServiceItem{Label: "Ksiega Postgres", State: "offline", Detail: "Nie udalo sie pobrac kont"}
		writeJSON(w, data)
		return
	}
	defer accountRows.Close()

	for accountRows.Next() {
		var bic, name, currency string
		var balance float64
		if err := accountRows.Scan(&bic, &name, &currency, &balance); err != nil {
			log.Printf("Dashboard account scan failed: %v", err)
			continue
		}
		data.Accounts = append(data.Accounts, AccountRow{
			BIC:     bic,
			Name:    name,
			Balance: formatMoney(currency, balance),
		})
	}

	queueRows, err := s.Ledger.Pool.Query(ctx, `
		SELECT msg_id, sender_bic, receiver_bic, amount, status::text, created_at
		FROM transactions
		ORDER BY created_at DESC
		LIMIT 10`)
	if err != nil {
		log.Printf("Dashboard queue query failed: %v", err)
		data.Services[2] = ServiceItem{Label: "Kolejka rozliczen", State: "degraded", Detail: "Nie udalo sie pobrac transakcji"}
		writeJSON(w, data)
		return
	}
	defer queueRows.Close()

	for queueRows.Next() {
		var item QueueItem
		var amount float64
		var createdAt time.Time
		if err := queueRows.Scan(&item.MsgID, &item.Sender, &item.Receiver, &amount, &item.Status, &createdAt); err != nil {
			log.Printf("Dashboard transaction scan failed: %v", err)
			continue
		}
		item.Amount = formatMoney("GBP", amount)
		item.Time = createdAt.Local().Format("15:04")
		data.Queue = append(data.Queue, item)
	}

	var settledToday int64
	var queued int64
	var rejected int64
	var settledValue float64
	if err := s.Ledger.Pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'SETTLED' AND created_at::date = CURRENT_DATE),
			COUNT(*) FILTER (WHERE status = 'QUEUED'),
			COUNT(*) FILTER (WHERE status = 'REJECTED'),
			COALESCE(SUM(amount) FILTER (WHERE status = 'SETTLED' AND created_at::date = CURRENT_DATE), 0)
		FROM transactions`).Scan(&settledToday, &queued, &rejected, &settledValue); err != nil {
		log.Printf("Dashboard metrics query failed: %v", err)
	} else {
		data.Metrics = []Metric{
			{Label: "Rozliczone dzis", Value: intString(settledToday)},
			{Label: "W kolejce", Value: intString(queued)},
			{Label: "Odrzucone", Value: intString(rejected)},
			{Label: "Laczna wartosc", Value: formatMoney("GBP", settledValue)},
		}
	}

	writeJSON(w, data)
}

func (s *Server) ProcessPayment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("Received request from %s", r.RemoteAddr)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("IO Error reading request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Hardcoded for the now
	version := "pacs.008.001.14"

	// STEP 1: Strict Validation
	if err := s.Validator.ValidateByVersion(version, body); err != nil {
		log.Printf("ISO 2022 [%s] Validation failed for: %v", version, err)
		http.Error(w, "Validation Failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// STEP 2: Extraction
	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal(body, &msg); err != nil {
		log.Printf("XML Unmarshal Error: %v", err)
		http.Error(w, "Failed to parse message structure", http.StatusBadRequest)
		return
	}

	// STEP 3: Settle in Postgres 18
	err = s.Ledger.SettlePayment(r.Context(), msg.MsgId, msg.Sender, msg.DestBIC, msg.Amount)

	var txStatus string
	if err != nil {
		if errors.Is(err, ledger.ErrInsufficientFunds) {
			txStatus = "PDNG" // Pending/Queued
		} else {
			log.Printf("Settlement error: %v", err)
			http.Error(w, "Internal Settlement Error", 500)
			return
		}
	} else {
		txStatus = "ACTC" // Accepted Technical (Settled)
	}

	// STEP 4: Generate pacs.002 Response
	responseMsg := iso20022.NewPacs002(msg.MsgId, msg.EndToEndId, txStatus, msg.Sender, msg.DestBIC)

    w.Header().Set("Content-Type", "application/xml")
    if txStatus == "ACTC" {
        w.WriteHeader(http.StatusOK)
    } else {
        w.WriteHeader(http.StatusAccepted)
    }
    
    if err := xml.NewEncoder(w).Encode(responseMsg); err != nil {
        log.Printf("Failed to encode pacs.002: %v", err)
    }

    log.Printf("Payment %s settled. Status: %s", msg.MsgId, txStatus)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func formatMoney(currency string, amount float64) string {
	return currency + " " + strconv.FormatFloat(amount, 'f', 2, 64)
}

func intString(value int64) string {
	return strconv.FormatInt(value, 10)
}
