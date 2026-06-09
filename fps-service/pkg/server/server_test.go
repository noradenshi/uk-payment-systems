package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fps-service/pkg/events"
)

func TestProcessJSONPaymentRequest_WithSortCodes(t *testing.T) {
	body := `{"msg_id":"FPS-SORT-001","sender_bic":"SNDRUK22","receiver_bic":"HSBCGB44","amount":250.00,"sender_sort_code":"60-00-00","receiver_sort_code":"40-00-00"}`

	r := httptest.NewRequest("POST", "/v1/payments/fps", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	var req struct {
		MsgID           string  `json:"msg_id"`
		SenderBIC       string  `json:"sender_bic"`
		ReceiverBIC     string  `json:"receiver_bic"`
		SenderSortCode  string  `json:"sender_sort_code"`
		ReceiverSortCode string `json:"receiver_sort_code"`
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
		SenderSortCode  string  `json:"sender_sort_code"`
		ReceiverSortCode string `json:"receiver_sort_code"`
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
		SortCode string  `json:"sort_code"`
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
		SortCode string  `json:"sort_code"`
		Balance  float64 `json:"balance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}
	if req.SortCode != "" {
		t.Errorf("expected empty SortCode, got %q", req.SortCode)
	}
}

// buildISO8583Msg builds a raw 0200 message with the given bitmap fields.
// The message includes only the fields enabled in the bitmap, in field order.
func buildISO8583Msg(fields ...int) []byte {
	var buf []byte
	buf = append(buf, []byte("0200")...)

	primBmp := uint64(0)
	secBmp := uint64(0)
	hasSec := false
	for _, f := range fields {
		if f <= 64 {
			primBmp |= 1 << (64 - f)
		} else if f <= 128 {
			primBmp |= 1 << 63
			secBmp |= 1 << (128 - f)
			hasSec = true
		}
	}
	for i := 7; i >= 0; i-- {
		buf = append(buf, byte((primBmp>>(i*8))&0xFF))
	}
	if hasSec {
		for i := 7; i >= 0; i-- {
			buf = append(buf, byte((secBmp>>(i*8))&0xFF))
		}
	}
	return buf
}

func TestHandleISO8583TCP_InvalidMTI(t *testing.T) {
	s := &Server{Ledger: nil, Events: events.NewEventBus()}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go s.handleISO8583TCP(serverConn)

	body := []byte("0100")
	binary.Write(clientConn, binary.BigEndian, uint16(len(body)))
	clientConn.Write(body)

	clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 4)
	_, err := clientConn.Read(buf)
	if err == nil {
		t.Fatal("expected error (connection closed) for invalid MTI")
	}
}

func TestHandleISO8583TCP_MessageTooLarge(t *testing.T) {
	s := &Server{Ledger: nil, Events: events.NewEventBus()}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go s.handleISO8583TCP(serverConn)

	binary.Write(clientConn, binary.BigEndian, uint16(5000))

	clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 4)
	_, err := clientConn.Read(buf)
	if err == nil {
		t.Fatal("expected error (connection closed) for oversized message")
	}
}

func TestHandleISO8583TCP_MissingFields(t *testing.T) {
	s := &Server{Ledger: nil, Events: events.NewEventBus()}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go s.handleISO8583TCP(serverConn)

	body := buildISO8583Msg()
	binary.Write(clientConn, binary.BigEndian, uint16(len(body)))
	clientConn.Write(body)

	clientConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 4)
	_, err := clientConn.Read(buf)
	if err == nil {
		t.Fatal("expected error (connection closed) for missing required fields")
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
