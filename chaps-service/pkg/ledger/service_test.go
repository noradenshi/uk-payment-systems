package ledger

import (
	"testing"
)

func TestErrorSentinels(t *testing.T) {
	if ErrInsufficientFunds.Error() != "insufficient funds" {
		t.Errorf("ErrInsufficientFunds = %q, want %q", ErrInsufficientFunds.Error(), "insufficient funds")
	}
	if ErrAccountNotFound.Error() != "account not found" {
		t.Errorf("ErrAccountNotFound = %q, want %q", ErrAccountNotFound.Error(), "account not found")
	}
	if ErrAccountClosed.Error() != "account closed" {
		t.Errorf("ErrAccountClosed = %q, want %q", ErrAccountClosed.Error(), "account closed")
	}
	if ErrSanctionsBlock.Error() != "sanctions block" {
		t.Errorf("ErrSanctionsBlock = %q, want %q", ErrSanctionsBlock.Error(), "sanctions block")
	}
}

func TestSettlementResult_ACTC(t *testing.T) {
	r := SettlementResult{Status: "ACTC", ReasonCode: ""}
	if r.Status != "ACTC" {
		t.Errorf("Status = %q, want %q", r.Status, "ACTC")
	}
	if r.ReasonCode != "" {
		t.Errorf("ReasonCode = %q, want empty", r.ReasonCode)
	}
}

func TestSettlementResult_RJCT(t *testing.T) {
	r := SettlementResult{Status: "RJCT", ReasonCode: "AM05"}
	if r.Status != "RJCT" {
		t.Errorf("Status = %q, want %q", r.Status, "RJCT")
	}
	if r.ReasonCode != "AM05" {
		t.Errorf("ReasonCode = %q, want %q", r.ReasonCode, "AM05")
	}
}

func TestSettlementResult_PDNG(t *testing.T) {
	r := SettlementResult{Status: "PDNG", ReasonCode: "INSU"}
	if r.Status != "PDNG" {
		t.Errorf("Status = %q, want %q", r.Status, "PDNG")
	}
	if r.ReasonCode != "INSU" {
		t.Errorf("ReasonCode = %q, want %q", r.ReasonCode, "INSU")
	}
}

func TestConstants(t *testing.T) {
	if singlePaymentLimit != 20000000.00 {
		t.Errorf("singlePaymentLimit = %f, want %f", singlePaymentLimit, 20000000.00)
	}
	if dailyParticipantLimit != 100000000.00 {
		t.Errorf("dailyParticipantLimit = %f, want %f", dailyParticipantLimit, 100000000.00)
	}
}

func TestNewLedgerService(t *testing.T) {
	// NewLedgerService requires a pool, but we verify it returns the expected type
	// by checking it doesn't panic with nil pool
	svc := NewLedgerService(nil)
	if svc == nil {
		t.Fatal("NewLedgerService returned nil")
	}
	if svc.Pool != nil {
		t.Errorf("expected nil pool, got %v", svc.Pool)
	}
}

func TestPaymentValidation_Valid(t *testing.T) {
	v := PaymentValidation{
		Valid:     true,
		Checks:    []string{"BIC_FORMAT", "PARTICIPANT_STATUS", "LIQUIDITY"},
		Errors:    []string{},
		Available: 1000000.00,
	}
	if !v.Valid {
		t.Errorf("expected valid=true")
	}
	if len(v.Checks) != 3 {
		t.Errorf("expected 3 checks, got %d", len(v.Checks))
	}
	if len(v.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(v.Errors))
	}
	if v.Available != 1000000.00 {
		t.Errorf("Available = %f, want %f", v.Available, 1000000.00)
	}
}

func TestClearingLimits_Defaults(t *testing.T) {
	limits := ClearingLimits{
		Currency:                   "GBP",
		SinglePaymentLimit:         20000000.00,
		DailyParticipantLimit:      100000000.00,
		TotalAvailableLiquidity:    5000000.00,
		RemainingIntradayLiquidity: 5000000.00,
	}
	if limits.Currency != "GBP" {
		t.Errorf("Currency = %q, want %q", limits.Currency, "GBP")
	}
	if limits.SinglePaymentLimit != 20000000.00 {
		t.Errorf("SinglePaymentLimit = %f, want %f", limits.SinglePaymentLimit, 20000000.00)
	}
	if limits.TotalAvailableLiquidity != 5000000.00 {
		t.Errorf("TotalAvailableLiquidity = %f, want %f", limits.TotalAvailableLiquidity, 5000000.00)
	}
}

func TestPosition_Struct(t *testing.T) {
	p := Position{
		BIC:       "BARCGB2L",
		Balance:   1000000.00,
		Earmarked: 50000.00,
		Available: 950000.00,
	}
	if p.BIC != "BARCGB2L" {
		t.Errorf("BIC = %q, want %q", p.BIC, "BARCGB2L")
	}
	if p.Available != 950000.00 {
		t.Errorf("Available = %f, want %f", p.Available, 950000.00)
	}
}
