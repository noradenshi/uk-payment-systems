package ledger

import (
	"chaps-service/pkg/auth"
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
var ErrParticipantInUse = errors.New("participant has related records")

const singlePaymentLimit = 20000000.00
const dailyParticipantLimit = 100000000.00

type SettlementResult struct {
	Status     string
	ReasonCode string
}

type PaymentSummary struct {
	MsgID           string    `json:"msg_id"`
	SenderBIC       string    `json:"sender_bic"`
	ReceiverBIC     string    `json:"receiver_bic"`
	SenderSortCode  string    `json:"sender_sort_code,omitempty"`
	ReceiverSortCode string   `json:"receiver_sort_code,omitempty"`
	Amount          float64   `json:"amount"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
}

type ParticipantSummary struct {
	BIC           string  `json:"bic"`
	Name          string  `json:"name"`
	SortCode      string  `json:"sort_code,omitempty"`
	Status        string  `json:"status"`
	Balance       float64 `json:"balance"`
	Currency      string  `json:"currency"`
	IsClosed      bool    `json:"is_closed"`
	OverdraftLimit float64 `json:"overdraft_limit"`
	BlockReason   *string `json:"block_reason,omitempty"`
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

func (s *LedgerService) UpdateParticipant(ctx context.Context, bic, name, sortCode string, balance float64) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE participant_profiles
			SET name = $1, sort_code = $2
			WHERE bic_code = $3`, name, sortCode, bic)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrAccountNotFound
		}
		_, err = tx.Exec(ctx, `
			UPDATE participant_liquidity
			SET balance = $1, updated_at = NOW()
			WHERE bic_code = $2`, balance, bic)
		return err
	})
}

func (s *LedgerService) DeleteParticipant(ctx context.Context, bic string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var used bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM transactions WHERE sender_bic = $1 OR receiver_bic = $1
				UNION ALL
				SELECT 1 FROM journal_entries WHERE account_bic = $1
			)`, bic).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrParticipantInUse
		}
		if _, err := tx.Exec(ctx, "DELETE FROM participant_liquidity WHERE bic_code = $1", bic); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "DELETE FROM participant_statuses WHERE bic_code = $1", bic); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM participant_profiles WHERE bic_code = $1", bic)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrAccountNotFound
		}
		return nil
	})
}

func (s *LedgerService) ListParticipants(ctx context.Context) ([]ParticipantSummary, error) {
	_ = s.EnforceRealtimeLiquidityBlocks(ctx)
	rows, err := s.Pool.Query(ctx, `
		SELECT p.bic_code, p.name, COALESCE(p.sort_code, ''), COALESCE(st.status::text, 'ACTIVE'), COALESCE(l.balance, 0), p.currency,
		       COALESCE(st.is_closed, false), COALESCE(st.overdraft_limit, 0), st.block_reason
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
		if err := rows.Scan(&p.BIC, &p.Name, &p.SortCode, &p.Status, &p.Balance, &p.Currency, &p.IsClosed, &p.OverdraftLimit, &p.BlockReason); err != nil {
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
		SELECT msg_id, sender_bic, receiver_bic, COALESCE(sender_sort_code, ''), COALESCE(receiver_sort_code, ''), amount, status::text, created_at
		FROM transactions`
	args := []any{}
	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
		query += " ORDER BY created_at DESC LIMIT $2"
	} else {
		query += " ORDER BY created_at DESC LIMIT $1"
	}
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []PaymentSummary{}
	for rows.Next() {
		var p PaymentSummary
		if err := rows.Scan(&p.MsgID, &p.SenderBIC, &p.ReceiverBIC, &p.SenderSortCode, &p.ReceiverSortCode, &p.Amount, &p.Status, &p.CreatedAt); err != nil {
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
		if err := s.Pool.QueryRow(ctx, `
			SELECT l.balance + COALESCE(st.overdraft_limit,0)
			FROM participant_liquidity l
			LEFT JOIN participant_statuses st ON st.bic_code = l.bic_code
			WHERE l.bic_code = $1`, bic).Scan(&limits.RemainingIntradayLiquidity); err != nil {
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
	var senderSortCode, receiverSortCode *string
	var endToEndID *string
	var createdAt time.Time

	err := s.Pool.QueryRow(ctx,
		"SELECT id, status, amount, sender_bic, receiver_bic, sender_sort_code, receiver_sort_code, end_to_end_id, created_at FROM transactions WHERE msg_id = $1",
		msgID).Scan(&internalID, &status, &amount, &senderBic, &receiverBic, &senderSortCode, &receiverSortCode, &endToEndID, &createdAt)
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
	if senderSortCode != nil {
		details["sender_sort_code"] = *senderSortCode
	}
	if receiverSortCode != nil {
		details["receiver_sort_code"] = *receiverSortCode
	}
	if endToEndID != nil {
		details["end_to_end_id"] = *endToEndID
	}
	details["audit_trail"] = journal

	return details, nil
}

func (s *LedgerService) LookupBankByName(ctx context.Context, name string) (string, error) {
	var bic string
	err := s.Pool.QueryRow(ctx,
		"SELECT bic_code FROM participant_profiles WHERE name = $1", name).Scan(&bic)
	if err != nil {
		return "", ErrAccountNotFound
	}
	return bic, nil
}

func (s *LedgerService) RegisterParticipant(ctx context.Context, bic, name, sortCode string, initialBalance float64) (string, error) {
	apiKey, err := auth.GenerateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO participant_profiles (bic_code, name, sort_code, api_key) VALUES ($1, $2, $3, $4)", bic, name, sortCode, apiKey); err != nil {
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
	if err != nil {
		return "", err
	}
	return apiKey, nil
}

func (s *LedgerService) ValidateAPIKey(ctx context.Context, apiKey string) (string, error) {
	var bic string
	err := s.Pool.QueryRow(ctx, "SELECT bic_code FROM participant_profiles WHERE api_key = $1", apiKey).Scan(&bic)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", auth.ErrInvalidAPIKey
		}
		return "", err
	}
	return bic, nil
}

func (s *LedgerService) GetSortCode(ctx context.Context, bic string) (string, error) {
	var sortCode string
	err := s.Pool.QueryRow(ctx, "SELECT sort_code FROM participant_profiles WHERE bic_code = $1", bic).Scan(&sortCode)
	if err != nil {
		return "", err
	}
	return sortCode, nil
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
	if err != nil {
		return err
	}
	_, _ = s.Pool.Exec(ctx, `
		UPDATE participant_statuses st
		SET liquidity_breach_at = NULL, updated_at = NOW()
		FROM participant_liquidity l
		WHERE l.bic_code = st.bic_code AND st.bic_code = $1 AND l.balance >= -st.overdraft_limit`, bic)
	_, _ = s.ResolveGridlock(ctx)
	return nil
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

func (s *LedgerService) SettlePayment(ctx context.Context, msgID string, sender string, receiver string, amount float64, endToEndID string, senderSortCode, receiverSortCode string, klikSessionID string) (SettlementResult, error) {
	result, err := s.settlePaymentOnce(ctx, msgID, sender, receiver, amount, endToEndID, senderSortCode, receiverSortCode, klikSessionID)
	if err != nil {
		return result, err
	}
	if result.Status == "PDNG" {
		s.ResolveGridlock(ctx)
		result, err = s.settlePaymentOnce(ctx, msgID, sender, receiver, amount, endToEndID, senderSortCode, receiverSortCode, klikSessionID)
	}
	return result, err
}

func (s *LedgerService) settlePaymentOnce(ctx context.Context, msgID string, sender string, receiver string, amount float64, endToEndID string, senderSortCode, receiverSortCode string, klikSessionID string) (SettlementResult, error) {
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
			INSERT INTO transactions (msg_id, sender_bic, receiver_bic, sender_sort_code, receiver_sort_code, amount, status, end_to_end_id, klik_session_id)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, 'PENDING', $7, NULLIF($8, ''))
			ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
			RETURNING id, status, sender_bic, receiver_bic, amount`,
			msgID, sender, receiver, senderSortCode, receiverSortCode, amount, endToEndID, klikSessionID).Scan(&internalUUID, &currentStatus, &existingSender, &existingReceiver, &existingAmount)

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

		var balance, overdraftLimit float64
		err = tx.QueryRow(ctx,
			`SELECT l.balance, COALESCE(st.overdraft_limit,0)
			 FROM participant_liquidity l
			 JOIN participant_statuses st ON st.bic_code = l.bic_code
			 WHERE l.bic_code = $1 FOR UPDATE OF l, st`,
			sender).Scan(&balance, &overdraftLimit)
		if err != nil {
			return err
		}

		if balance-amount < -overdraftLimit {
			result = SettlementResult{Status: "PDNG", ReasonCode: "INSU"}
			_, _ = tx.Exec(ctx, "UPDATE transactions SET status = 'QUEUED' WHERE id = $1", internalUUID)
			_, _ = tx.Exec(ctx, `
				UPDATE participant_statuses
				SET liquidity_breach_at = COALESCE(liquidity_breach_at, NOW()), updated_at = NOW()
				WHERE bic_code = $1`, sender)
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

func (s *LedgerService) ResolveGridlock(ctx context.Context) (int, error) {
	settled := 0
	for {
		progress := false
		err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id, sender_bic, receiver_bic, amount
				FROM transactions
				WHERE status='QUEUED'
				ORDER BY created_at
				FOR UPDATE SKIP LOCKED`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id pgtype.UUID
				var sender, receiver string
				var amount float64
				if err := rows.Scan(&id, &sender, &receiver, &amount); err != nil {
					return err
				}
				var senderBalance, overdraftLimit float64
				if err := tx.QueryRow(ctx, `
					SELECT l.balance, COALESCE(st.overdraft_limit,0)
					FROM participant_liquidity l
					JOIN participant_statuses st ON st.bic_code = l.bic_code
					WHERE l.bic_code=$1 AND st.status='ACTIVE'
					FOR UPDATE OF l, st`, sender).Scan(&senderBalance, &overdraftLimit); err != nil {
					continue
				}
				if senderBalance-amount < -overdraftLimit {
					continue
				}
				if _, err := tx.Exec(ctx, `SELECT balance FROM participant_liquidity WHERE bic_code=$1 FOR UPDATE`, receiver); err != nil {
					continue
				}
				if _, err := tx.Exec(ctx, `UPDATE participant_liquidity SET balance=balance-$1, updated_at=NOW() WHERE bic_code=$2`, amount, sender); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE participant_liquidity SET balance=balance+$1, updated_at=NOW() WHERE bic_code=$2`, amount, receiver); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO journal_entries (transaction_id, account_bic, amount)
					VALUES ($1,$2,$3),($1,$4,$5)`, id, sender, -amount, receiver, amount); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE transactions SET status='SETTLED' WHERE id=$1`, id); err != nil {
					return err
				}
				progress = true
				settled++
			}
			return rows.Err()
		})
		if err != nil || !progress {
			return settled, err
		}
	}
}

func (s *LedgerService) EnforceRealtimeLiquidityBlocks(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE participant_statuses st
		SET status='SUSPENDED', block_reason='LIQUIDITY_LIMIT_EXCEEDED_2H',
		    blocked_at=COALESCE(blocked_at, NOW()), updated_at=NOW()
		FROM participant_liquidity l
		WHERE l.bic_code = st.bic_code
		  AND st.status='ACTIVE'
		  AND st.liquidity_breach_at IS NOT NULL
		  AND st.liquidity_breach_at <= NOW() - INTERVAL '2 hours'
		  AND l.balance < -st.overdraft_limit`)
	return err
}

func (s *LedgerService) UpdateOverdraftLimit(ctx context.Context, bic string, limit float64) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE participant_statuses
		SET overdraft_limit=$1, updated_at=NOW()
		WHERE bic_code=$2`, limit, bic)
	return err
}
