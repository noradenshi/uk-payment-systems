package ledger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerService struct {
	Pool *pgxpool.Pool
}

func NewLedgerService(pool *pgxpool.Pool) *LedgerService {
	return &LedgerService{Pool: pool}
}

var ErrInsufficientFunds = errors.New("insufficient funds")
var ErrAccountNotFound = errors.New("account not found")
var ErrAccountClosed = errors.New("account closed")
var ErrSanctionsBlock = errors.New("sanctions block")

const singlePaymentLimit = 20000000.00
const dailyParticipantLimit = 100000000.00

type SettlementResult struct {
	Status     string
	ReasonCode string
}

type PaymentSummary struct {
	MsgID       string    `json:"msg_id"`
	SenderBIC   string    `json:"sender_bic"`
	ReceiverBIC string    `json:"receiver_bic"`
	Amount      float64   `json:"amount"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type ParticipantSummary struct {
	BIC         string  `json:"bic"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	Balance     float64 `json:"balance"`
	Currency    string  `json:"currency"`
	IsClosed    bool    `json:"is_closed"`
	BlockReason *string `json:"block_reason,omitempty"`
}

type PaymentValidation struct {
	Valid     bool     `json:"valid"`
	Checks    []string `json:"checks"`
	Errors    []string `json:"errors"`
	Available float64  `json:"available"`
}

type ClearingLimits struct {
	Currency                   string  `json:"currency"`
	SinglePaymentLimit         float64 `json:"single_payment_limit"`
	DailyParticipantLimit      float64 `json:"daily_participant_limit"`
	TotalAvailableLiquidity    float64 `json:"total_available_liquidity"`
	RemainingIntradayLiquidity float64 `json:"remaining_intraday_liquidity"`
}

type Position struct {
	BIC       string  `json:"bic"`
	Balance   float64 `json:"balance"`
	Earmarked float64 `json:"earmarked"`
	Available float64 `json:"available"`
}

func (s *LedgerService) BlockParticipant(ctx context.Context, bic string, reason string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
            UPDATE participant_statuses
            SET status = 'SUSPENDED', block_reason = $1, blocked_at = NOW()
            WHERE bic_code = $2`, reason, bic)
		return err
	})
}

func (s *LedgerService) UpdateParticipantStatus(ctx context.Context, bic string, status string, reason string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE participant_statuses
			SET status = $1::participant_status, block_reason = NULLIF($2, ''), blocked_at = CASE WHEN $1 = 'SUSPENDED' THEN NOW() ELSE NULL END, updated_at = NOW()
			WHERE bic_code = $3`, status, reason, bic)
		return err
	})
}

func (s *LedgerService) ListParticipants(ctx context.Context) ([]ParticipantSummary, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.bic_code, p.name, COALESCE(st.status::text, 'ACTIVE'), COALESCE(l.balance, 0), p.currency, COALESCE(st.is_closed, false), st.block_reason
		FROM participant_profiles p
		LEFT JOIN participant_statuses st ON st.bic_code = p.bic_code
		LEFT JOIN participant_liquidity l ON l.bic_code = p.bic_code
		ORDER BY p.bic_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	participants := []ParticipantSummary{}
	for rows.Next() {
		var p ParticipantSummary
		if err := rows.Scan(&p.BIC, &p.Name, &p.Status, &p.Balance, &p.Currency, &p.IsClosed, &p.BlockReason); err != nil {
			return nil, err
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}

func (s *LedgerService) ListPayments(ctx context.Context, status string, limit int) ([]PaymentSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT msg_id, sender_bic, receiver_bic, amount, status::text, created_at
		FROM transactions`
	args := []any{}
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT $2"
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []PaymentSummary{}
	for rows.Next() {
		var p PaymentSummary
		if err := rows.Scan(&p.MsgID, &p.SenderBIC, &p.ReceiverBIC, &p.Amount, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	return payments, rows.Err()
}

func (s *LedgerService) ValidatePayment(ctx context.Context, sender, receiver string, amount float64) (PaymentValidation, error) {
	result := PaymentValidation{
		Valid:  true,
		Checks: []string{"BIC_FORMAT", "PARTICIPANT_STATUS", "LIQUIDITY"},
		Errors: []string{},
	}
	if len(sender) < 8 || len(sender) > 11 || len(receiver) < 8 || len(receiver) > 11 {
		result.Valid = false
		result.Errors = append(result.Errors, "BIC must be 8 to 11 characters")
	}
	if amount <= 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Amount must be positive")
	}
	if amount > singlePaymentLimit {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Amount exceeds single payment limit of £%.0f", singlePaymentLimit))
	}

	for _, bic := range []string{sender, receiver} {
		var status string
		var isClosed bool
		err := s.Pool.QueryRow(ctx, "SELECT status::text, is_closed FROM participant_statuses WHERE bic_code = $1", bic).Scan(&status, &isClosed)
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("%s participant not found", bic))
			continue
		}
		if status != "ACTIVE" || isClosed {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("%s is not active", bic))
		}
	}

	var available float64
	err := s.Pool.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code = $1", sender).Scan(&available)
	if err != nil {
		result.Errors = append(result.Errors, "Sender liquidity unavailable")
	} else {
		result.Available = available
		if available < amount {
			result.Valid = false
			result.Errors = append(result.Errors, "Insufficient liquidity")
		}
	}

	return result, nil
}

func (s *LedgerService) CancelPayment(ctx context.Context, msgID string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, "UPDATE transactions SET status = 'REJECTED' WHERE msg_id = $1 AND (status = 'PENDING' OR status = 'QUEUED')", msgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *LedgerService) AmendPayment(ctx context.Context, msgID string, endToEndID string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, "UPDATE transactions SET end_to_end_id = COALESCE(NULLIF($1, ''), end_to_end_id) WHERE msg_id = $2 AND status = 'PENDING'", endToEndID, msgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *LedgerService) GetClearingLimits(ctx context.Context, bic string) (ClearingLimits, error) {
	limits := ClearingLimits{
		Currency:              "GBP",
		SinglePaymentLimit:    singlePaymentLimit,
		DailyParticipantLimit: dailyParticipantLimit,
	}
	row := s.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(balance), 0) FROM participant_liquidity")
	if err := row.Scan(&limits.TotalAvailableLiquidity); err != nil {
		return limits, err
	}
	if bic != "" {
		if err := s.Pool.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code = $1", bic).Scan(&limits.RemainingIntradayLiquidity); err != nil {
			return limits, err
		}
	} else {
		limits.RemainingIntradayLiquidity = limits.TotalAvailableLiquidity
	}
	return limits, nil
}

func (s *LedgerService) GetBlockDetails(ctx context.Context, bic string) (map[string]interface{}, error) {
	var status string
	var blockedAt *time.Time
	var reason *string
	err := s.Pool.QueryRow(ctx, `
		SELECT status::text, blocked_at, block_reason
		FROM participant_statuses
		WHERE bic_code = $1`, bic).Scan(&status, &blockedAt, &reason)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"bic":        bic,
		"status":     status,
		"blocked_at": blockedAt,
		"reason":     reason,
	}, nil
}

func (s *LedgerService) GetPaymentDetails(ctx context.Context, msgID string) (map[string]interface{}, error) {
	var details = make(map[string]interface{})

	var status string
	var amount float64
	var internalID pgtype.UUID
	var senderBic string
	var receiverBic string
	var endToEndID *string
	var createdAt time.Time

	err := s.Pool.QueryRow(ctx,
		"SELECT id, status, amount, sender_bic, receiver_bic, end_to_end_id, created_at FROM transactions WHERE msg_id = $1",
		msgID).Scan(&internalID, &status, &amount, &senderBic, &receiverBic, &endToEndID, &createdAt)
	if err != nil {
		return nil, err
	}

	rows, err := s.Pool.Query(ctx,
		"SELECT account_bic, amount FROM journal_entries WHERE transaction_id = $1",
		internalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type entry struct {
		BIC    string  `json:"bic"`
		Amount float64 `json:"amount"`
	}
	journal := []entry{}

	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.BIC, &e.Amount); err != nil {
			log.Printf("Scan error in journal for %s: %v", msgID, err)
			continue
		}
		journal = append(journal, e)
	}

	details["msg_id"] = msgID
	details["status"] = status
	details["amount"] = amount
	details["sender_bic"] = senderBic
	details["receiver_bic"] = receiverBic
	details["created_at"] = createdAt
	if endToEndID != nil {
		details["end_to_end_id"] = *endToEndID
	}
	details["audit_trail"] = journal

	return details, nil
}

func (s *LedgerService) RegisterParticipant(ctx context.Context, bic, name string, initialBalance float64) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO participant_profiles (bic_code, name) VALUES ($1, $2)", bic, name); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO participant_statuses (bic_code) VALUES ($1)", bic); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO participant_liquidity (bic_code, balance) VALUES ($1, $2)", bic, initialBalance); err != nil {
			return err
		}
		return nil
	})
}

func (s *LedgerService) UnblockParticipant(ctx context.Context, bic string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE participant_statuses
		SET status = 'ACTIVE', block_reason = NULL, blocked_at = NULL, updated_at = NOW()
		WHERE bic_code = $1`, bic)
	return err
}

func (s *LedgerService) GetPosition(ctx context.Context, bic string) (Position, error) {
	var p Position
	err := s.Pool.QueryRow(ctx, `
		SELECT bic_code, balance
		FROM participant_liquidity
		WHERE bic_code = $1`, bic).Scan(&p.BIC, &p.Balance)

	p.Available = p.Balance
	return p, err
}

func (s *LedgerService) TopUpLiquidity(ctx context.Context, bic string, amount float64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE participant_liquidity
		SET balance = balance + $1, updated_at = NOW()
		WHERE bic_code = $2`, amount, bic)
	return err
}

func (s *LedgerService) checkDailyLimit(ctx context.Context, tx pgx.Tx, sender string, amount float64) error {
	var dayTotal float64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM transactions
		WHERE sender_bic = $1 AND status = 'SETTLED'
		AND created_at >= CURRENT_DATE`, sender).Scan(&dayTotal)
	if err != nil {
		return err
	}
	if dayTotal+amount > dailyParticipantLimit {
		return fmt.Errorf("daily participant limit of £%.0f exceeded", dailyParticipantLimit)
	}
	return nil
}

func (s *LedgerService) SettlePayment(ctx context.Context, msgID string, sender string, receiver string, amount float64, endToEndID string) (SettlementResult, error) {
	var result SettlementResult

	if amount > singlePaymentLimit {
		return SettlementResult{Status: "RJCT", ReasonCode: "AM05"}, nil
	}

	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var senderExists bool
		var receiverExists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM participant_profiles WHERE bic_code = $1)", sender).Scan(&senderExists); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM participant_profiles WHERE bic_code = $1)", receiver).Scan(&receiverExists); err != nil {
			return err
		}
		if !senderExists || !receiverExists {
			result = SettlementResult{Status: "RJCT", ReasonCode: "AC01"}
			return nil
		}

		var internalUUID pgtype.UUID
		var currentStatus string
		var existingSender, existingReceiver string
		var existingAmount float64

		err := tx.QueryRow(ctx, `
			INSERT INTO transactions (msg_id, sender_bic, receiver_bic, amount, status, end_to_end_id)
			VALUES ($1, $2, $3, $4, 'PENDING', $5)
			ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
			RETURNING id, status, sender_bic, receiver_bic, amount`,
			msgID, sender, receiver, amount, endToEndID).Scan(&internalUUID, &currentStatus, &existingSender, &existingReceiver, &existingAmount)

		if err != nil {
			return fmt.Errorf("failed to initialize transaction: %w", err)
		}

		if currentStatus == "SETTLED" {
			if existingSender != sender || existingReceiver != receiver || existingAmount != amount {
				result = SettlementResult{Status: "RJCT", ReasonCode: "AM05"}
				return nil
			}
			log.Printf("Idempotent hit for MsgId: %s. Returning cached result.", msgID)
			result = SettlementResult{Status: "ACTC", ReasonCode: ""}
			return nil
		}

		for _, bic := range []string{sender, receiver} {
			var participantStatus string
			var isClosed bool
			err = tx.QueryRow(ctx,
				"SELECT status, is_closed FROM participant_statuses WHERE bic_code = $1 FOR UPDATE",
				bic).Scan(&participantStatus, &isClosed)
			if err == pgx.ErrNoRows {
				result = SettlementResult{Status: "RJCT", ReasonCode: "AC01"}
				tx.Exec(ctx, "UPDATE transactions SET status = 'REJECTED' WHERE id = $1", internalUUID)
				return nil
			}
			if isClosed || participantStatus != "ACTIVE" {
				result = SettlementResult{Status: "RJCT", ReasonCode: "AC04"}
				tx.Exec(ctx, "UPDATE transactions SET status = 'REJECTED' WHERE id = $1", internalUUID)
				return nil
			}
		}

		if err := s.checkDailyLimit(ctx, tx, sender, amount); err != nil {
			result = SettlementResult{Status: "RJCT", ReasonCode: "AM05"}
			tx.Exec(ctx, "UPDATE transactions SET status = 'REJECTED' WHERE id = $1", internalUUID)
			return nil
		}

		var balance float64
		err = tx.QueryRow(ctx,
			"SELECT balance FROM participant_liquidity WHERE bic_code = $1 FOR UPDATE",
			sender).Scan(&balance)

		if balance < amount {
			result = SettlementResult{Status: "PDNG", ReasonCode: "INSU"}
			_, _ = tx.Exec(ctx, "UPDATE transactions SET status = 'QUEUED' WHERE id = $1", internalUUID)
			return nil
		}

		var receiverBalance float64
		err = tx.QueryRow(ctx,
			"SELECT balance FROM participant_liquidity WHERE bic_code = $1 FOR UPDATE",
			receiver).Scan(&receiverBalance)
		if err != nil {
			return fmt.Errorf("receiver lock failed: %w", err)
		}

		_, err = tx.Exec(ctx, "UPDATE participant_liquidity SET balance = balance - $1 WHERE bic_code = $2", amount, sender)
		if err != nil {
			return fmt.Errorf("debit failed: %w", err)
		}

		_, err = tx.Exec(ctx, "UPDATE participant_liquidity SET balance = balance + $1 WHERE bic_code = $2", amount, receiver)
		if err != nil {
			return fmt.Errorf("credit failed: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO journal_entries (transaction_id, account_bic, amount)
			VALUES ($1, $2, $3), ($1, $4, $5)`,
			internalUUID, sender, -amount, receiver, amount)
		if err != nil {
			return fmt.Errorf("journal entry failed: %w", err)
		}

		result = SettlementResult{Status: "ACTC", ReasonCode: ""}
		_, err = tx.Exec(ctx, "UPDATE transactions SET status = 'SETTLED' WHERE id = $1", internalUUID)
		return nil
	})

	return result, err
}
