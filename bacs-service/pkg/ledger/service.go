package ledger

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerService struct {
	Pool *pgxpool.Pool
}

func NewLedgerService(pool *pgxpool.Pool) *LedgerService {
	return &LedgerService{Pool: pool}
}

var ErrAccountNotFound = errors.New("account not found")
var ErrInsufficientFunds = errors.New("insufficient funds")

// ── Participant operations ──

func (s *LedgerService) RegisterParticipant(ctx context.Context, bic, name string, initialBalance float64, suCode string, isSU, isDSU bool) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO participant_profiles (bic_code, name, su_code, is_service_user, is_destination_user) VALUES ($1,$2,$3,$4,$5)`, bic, name, suCode, isSU, isDSU); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO participant_statuses (bic_code) VALUES ($1)`, bic); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO participant_liquidity (bic_code, balance) VALUES ($1,$2)`, bic, initialBalance); err != nil {
			return err
		}
		return nil
	})
}

func (s *LedgerService) ListParticipants(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.bic_code, p.name, p.su_code, p.is_service_user, p.is_destination_user,
		       COALESCE(st.status::text,'ACTIVE'), COALESCE(l.balance,0), p.currency,
		       COALESCE(st.is_closed,false), st.block_reason
		FROM participant_profiles p
		LEFT JOIN participant_statuses st ON st.bic_code = p.bic_code
		LEFT JOIN participant_liquidity l ON l.bic_code = p.bic_code
		ORDER BY p.bic_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var bic, name, status, currency string
		var suCode, blockReason *string
		var isSU, isDSU, isClosed bool
		var balance float64
		if err := rows.Scan(&bic, &name, &suCode, &isSU, &isDSU, &status, &balance, &currency, &isClosed, &blockReason); err != nil {
			return nil, err
		}
		entry := map[string]interface{}{
			"bic":                bic,
			"name":               name,
			"status":             status,
			"balance":            balance,
			"currency":           currency,
			"is_closed":          isClosed,
			"is_service_user":    isSU,
			"is_destination_user": isDSU,
		}
		if suCode != nil {
			entry["su_code"] = *suCode
		}
		if blockReason != nil {
			entry["block_reason"] = *blockReason
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *LedgerService) UpdateParticipantStatus(ctx context.Context, bic, status, reason string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE participant_statuses
			SET status = $1::participant_status, block_reason = NULLIF($2,''),
			    blocked_at = CASE WHEN $1='SUSPENDED' THEN NOW() ELSE NULL END,
			    updated_at = NOW()
			WHERE bic_code = $3`, status, reason, bic)
		return err
	})
}

func (s *LedgerService) BlockParticipant(ctx context.Context, bic, reason string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE participant_statuses
			SET status = 'SUSPENDED', block_reason = $1, blocked_at = NOW()
			WHERE bic_code = $2`, reason, bic)
		return err
	})
}

func (s *LedgerService) GetBlockDetails(ctx context.Context, bic string) (map[string]interface{}, error) {
	var status string
	var blockedAt *time.Time
	var reason *string
	err := s.Pool.QueryRow(ctx, `
		SELECT status::text, blocked_at, block_reason
		FROM participant_statuses WHERE bic_code = $1`, bic).Scan(&status, &blockedAt, &reason)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{
		"bic":    bic,
		"status": status,
	}
	if blockedAt != nil {
		m["blocked_at"] = blockedAt.Format(time.RFC3339)
	}
	if reason != nil {
		m["reason"] = *reason
	}
	return m, nil
}

func (s *LedgerService) UnblockParticipant(ctx context.Context, bic string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE participant_statuses
		SET status = 'ACTIVE', block_reason = NULL, blocked_at = NULL, updated_at = NOW()
		WHERE bic_code = $1`, bic)
	return err
}

// ── Cycle management ──

func (s *LedgerService) GetCurrentCycle(ctx context.Context) (map[string]interface{}, error) {
	var id int
	var inputDate, processingDate, settlementDate time.Time
	var status string
	err := s.Pool.QueryRow(ctx, `
		SELECT id, input_date, processing_date, settlement_date, status::text
		FROM bacs_cycles WHERE status = 'OPEN'
		ORDER BY created_at DESC LIMIT 1`).
		Scan(&id, &inputDate, &processingDate, &settlementDate, &status)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":               id,
		"input_date":       inputDate.Format("2006-01-02"),
		"processing_date":  processingDate.Format("2006-01-02"),
		"settlement_date":  settlementDate.Format("2006-01-02"),
		"status":           status,
	}, nil
}

func (s *LedgerService) CloseInputDay(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var cycleID int
		err := tx.QueryRow(ctx, `SELECT id FROM bacs_cycles WHERE status = 'OPEN' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`).Scan(&cycleID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE bacs_cycles SET status = 'PROCESSING' WHERE id = $1`, cycleID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO bacs_cycles (input_date, processing_date, settlement_date, status)
			VALUES (CURRENT_DATE, CURRENT_DATE+1, CURRENT_DATE+2, 'OPEN')`)
		return err
	})
}

func (s *LedgerService) ProcessCycle(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE bacs_cycles SET status = 'AWAITING_SETTLEMENT'
		WHERE status = 'PROCESSING'`)
	return err
}

func (s *LedgerService) SettleCycle(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var cycleID int
		err := tx.QueryRow(ctx, `SELECT id FROM bacs_cycles WHERE status = 'AWAITING_SETTLEMENT' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`).Scan(&cycleID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT su_bic, SUM(total_value)
			FROM bacs_submissions WHERE cycle_id = $1 AND status = 'ACCEPTED'
			GROUP BY su_bic`, cycleID)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var bic string
			var netAmount float64
			if err := rows.Scan(&bic, &netAmount); err != nil {
				return err
			}
			var balance float64
			err := tx.QueryRow(ctx, `SELECT balance FROM participant_liquidity WHERE bic_code = $1 FOR UPDATE`, bic).Scan(&balance)
			if err != nil {
				log.Printf("SettleCycle: participant %s not found, skipping", bic)
				continue
			}
			_, err = tx.Exec(ctx, `UPDATE participant_liquidity SET balance = balance + $1, updated_at = NOW() WHERE bic_code = $2`, netAmount, bic)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO bacs_journal_entries (submission_id, account_bic, amount) VALUES (NULL, $1, $2)`, bic, netAmount)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE bacs_cycles SET status = 'SETTLED' WHERE id = $1`, cycleID)
		return err
	})
}

func (s *LedgerService) ListCycles(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, input_date, processing_date, settlement_date, status::text
		FROM bacs_cycles ORDER BY created_at DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id int
		var inputDate, processingDate, settlementDate time.Time
		var status string
		if err := rows.Scan(&id, &inputDate, &processingDate, &settlementDate, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"id":               id,
			"input_date":       inputDate.Format("2006-01-02"),
			"processing_date":  processingDate.Format("2006-01-02"),
			"settlement_date":  settlementDate.Format("2006-01-02"),
			"status":           status,
		})
	}
	return result, rows.Err()
}

// ── Submission management ──

func (s *LedgerService) CreateSubmission(ctx context.Context, filename, suBic string, totalVolume int, totalValue float64, cycleID int) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO bacs_submissions (filename, su_bic, total_volume, total_value, cycle_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`, filename, suBic, totalVolume, totalValue, cycleID).Scan(&id)
	return id, err
}

func (s *LedgerService) GetSubmission(ctx context.Context, id string) (map[string]interface{}, error) {
	var filename, suBic, status string
	var totalVolume int
	var totalValue float64
	var cycleID, errorCount int
	var createdAt time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT filename, su_bic, total_volume, total_value, status::text, cycle_id, error_count, created_at
		FROM bacs_submissions WHERE id = $1`, id).
		Scan(&filename, &suBic, &totalVolume, &totalValue, &status, &cycleID, &errorCount, &createdAt)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"id":           id,
		"filename":     filename,
		"su_bic":       suBic,
		"total_volume": totalVolume,
		"total_value":  totalValue,
		"status":       status,
		"cycle_id":     cycleID,
		"error_count":  errorCount,
		"created_at":   createdAt.Format(time.RFC3339),
	}, nil
}

func (s *LedgerService) ListSubmissions(ctx context.Context, statusFilter, suBic string) ([]map[string]interface{}, error) {
	query := `SELECT id, filename, su_bic, total_volume, total_value, status::text, cycle_id, error_count, created_at
		FROM bacs_submissions WHERE 1=1`
	args := []interface{}{}
	argIdx := 1
	if statusFilter != "" {
		query += " AND status = $" + fmt.Sprintf("%d", argIdx)
		args = append(args, statusFilter)
		argIdx++
	}
	if suBic != "" {
		query += " AND su_bic = $" + fmt.Sprintf("%d", argIdx)
		args = append(args, suBic)
	}
	query += " ORDER BY created_at DESC LIMIT 100"
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, filename, suBic2, status string
		var totalVolume int
		var totalValue float64
		var cycleID, errorCount int
		var createdAt time.Time
		if err := rows.Scan(&id, &filename, &suBic2, &totalVolume, &totalValue, &status, &cycleID, &errorCount, &createdAt); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"id":           id,
			"filename":     filename,
			"su_bic":       suBic2,
			"total_volume": totalVolume,
			"total_value":  totalValue,
			"status":       status,
			"cycle_id":     cycleID,
			"error_count":  errorCount,
			"created_at":   createdAt.Format(time.RFC3339),
		})
	}
	return result, rows.Err()
}

func (s *LedgerService) RecallSubmission(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE bacs_submissions SET status = 'RECALLED' WHERE id = $1 AND status = 'RECEIVED'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("submission cannot be recalled: only RECEIVED submissions can be recalled")
	}
	return nil
}

// ── Transaction processing ──

func (s *LedgerService) StoreTransactions(ctx context.Context, submissionID string, debits []map[string]interface{}, credits []map[string]interface{}) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, d := range debits {
			_, err := tx.Exec(ctx, `
				INSERT INTO bacs_transactions (submission_id, record_type, volume_header_no, dest_sort_code, dest_account, amount, originator_ref, reference, su_code, status)
				VALUES ($1, 'DIRECT_DEBIT', $2, $3, $4, $5, $6, $7, $8, 'PENDING')`,
				submissionID, d["volume_header_no"], d["dest_sort_code"], d["dest_account"],
				d["amount"], d["originator_ref"], d["reference"], d["su_code"])
			if err != nil {
				return err
			}
		}
		for _, c := range credits {
			_, err := tx.Exec(ctx, `
				INSERT INTO bacs_transactions (submission_id, record_type, volume_header_no, dest_sort_code, dest_account, amount, originator_ref, reference, su_code, status)
				VALUES ($1, 'DIRECT_CREDIT', $2, $3, $4, $5, $6, $7, $8, 'PENDING')`,
				submissionID, c["volume_header_no"], c["dest_sort_code"], c["dest_account"],
				c["amount"], c["originator_ref"], c["reference"], c["su_code"])
			if err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE bacs_submissions SET status = 'ACCEPTED' WHERE id = $1`, submissionID)
		return err
	})
}

func (s *LedgerService) GetTransactions(ctx context.Context, submissionID string) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, record_type::text, volume_header_no, dest_sort_code, dest_account, amount, originator_ref, reference, su_code, status, created_at
		FROM bacs_transactions WHERE submission_id = $1 ORDER BY id`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id int
		var recordType, destSortCode, destAccount, suCode, status string
		var volumeHeaderNo int
		var amount float64
		var originatorRef, reference *string
		var createdAt time.Time
		if err := rows.Scan(&id, &recordType, &volumeHeaderNo, &destSortCode, &destAccount, &amount, &originatorRef, &reference, &suCode, &status, &createdAt); err != nil {
			return nil, err
		}
		t := map[string]interface{}{
			"id":                id,
			"record_type":       recordType,
			"volume_header_no": volumeHeaderNo,
			"dest_sort_code":   destSortCode,
			"dest_account":     destAccount,
			"amount":           amount,
			"su_code":          suCode,
			"status":           status,
			"created_at":       createdAt.Format(time.RFC3339),
		}
		if originatorRef != nil {
			t["originator_ref"] = *originatorRef
		}
		if reference != nil {
			t["reference"] = *reference
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// ── Mandate (AUDDIS) management ──

func (s *LedgerService) CreateMandate(ctx context.Context, ref, suBic, payerName, sortCode, account string, amount float64, frequency string) (int, error) {
	var id int
	if frequency == "" {
		frequency = "MONTHLY"
	}
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO bacs_mandates (reference, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`, ref, suBic, payerName, sortCode, account, amount, frequency).Scan(&id)
	return id, err
}

func (s *LedgerService) GetMandate(ctx context.Context, ref string) (map[string]interface{}, error) {
	var id int
	var suBic string
	var payerName, payerSortCode, payerAccount, frequency, status *string
	var amount *float64
	var createdAt time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT id, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency, status, created_at
		FROM bacs_mandates WHERE reference = $1`, ref).
		Scan(&id, &suBic, &payerName, &payerSortCode, &payerAccount, &amount, &frequency, &status, &createdAt)
	if err != nil {
		return nil, err
	}
	m := map[string]interface{}{
		"id":         id,
		"reference":  ref,
		"su_bic":     suBic,
		"created_at": createdAt.Format(time.RFC3339),
	}
	if payerName != nil {
		m["payer_name"] = *payerName
	}
	if payerSortCode != nil {
		m["payer_sort_code"] = *payerSortCode
	}
	if payerAccount != nil {
		m["payer_account"] = *payerAccount
	}
	if amount != nil {
		m["amount"] = *amount
	}
	if frequency != nil {
		m["frequency"] = *frequency
	}
	if status != nil {
		m["status"] = *status
	}
	return m, nil
}

func (s *LedgerService) ListMandates(ctx context.Context, suBic string) ([]map[string]interface{}, error) {
	query := `SELECT id, reference, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency, status, created_at FROM bacs_mandates`
	args := []interface{}{}
	if suBic != "" {
		query += " WHERE su_bic = $1"
		args = append(args, suBic)
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id int
		var ref, suBic2 string
		var payerName, payerSortCode, payerAccount, frequency, status *string
		var amount *float64
		var createdAt time.Time
		if err := rows.Scan(&id, &ref, &suBic2, &payerName, &payerSortCode, &payerAccount, &amount, &frequency, &status, &createdAt); err != nil {
			return nil, err
		}
		m := map[string]interface{}{
			"id":         id,
			"reference":  ref,
			"su_bic":     suBic2,
			"created_at": createdAt.Format(time.RFC3339),
		}
		if payerName != nil {
			m["payer_name"] = *payerName
		}
		if payerSortCode != nil {
			m["payer_sort_code"] = *payerSortCode
		}
		if payerAccount != nil {
			m["payer_account"] = *payerAccount
		}
		if amount != nil {
			m["amount"] = *amount
		}
		if frequency != nil {
			m["frequency"] = *frequency
		}
		if status != nil {
			m["status"] = *status
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *LedgerService) AmendMandate(ctx context.Context, ref string, amount float64, frequency string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE bacs_mandates SET amount = $1, frequency = $2 WHERE reference = $3 AND status = 'ACTIVE'`, amount, frequency, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("mandate not found or not ACTIVE")
	}
	return nil
}

func (s *LedgerService) CancelMandate(ctx context.Context, ref string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE bacs_mandates SET status = 'CANCELLED' WHERE reference = $1 AND status = 'ACTIVE'`, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("mandate not found or not ACTIVE")
	}
	return nil
}

func (s *LedgerService) ClaimMandate(ctx context.Context, ref, sortCode, account string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE bacs_mandates SET payer_sort_code = $1, payer_account = $2 WHERE reference = $3`, sortCode, account, ref)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("mandate not found")
	}
	return nil
}

// ── Return (ARUDD) management ──

func (s *LedgerService) CreateReturn(ctx context.Context, origTransID int, reasonCode string, amount float64) (int, error) {
	var id int
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO bacs_returns (original_transaction_id, reason_code, amount)
		VALUES ($1, $2, $3) RETURNING id`, origTransID, reasonCode, amount).Scan(&id)
	return id, err
}

func (s *LedgerService) ListReturns(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT r.id, r.original_transaction_id, r.reason_code, r.amount, r.return_date,
		       t.dest_sort_code, t.dest_account, t.su_code
		FROM bacs_returns r
		LEFT JOIN bacs_transactions t ON t.id = r.original_transaction_id
		ORDER BY r.return_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, origTransID int
		var reasonCode string
		var amount float64
		var returnDate time.Time
		var destSortCode, destAccount, suCode *string
		if err := rows.Scan(&id, &origTransID, &reasonCode, &amount, &returnDate, &destSortCode, &destAccount, &suCode); err != nil {
			return nil, err
		}
		m := map[string]interface{}{
			"id":                       id,
			"original_transaction_id":  origTransID,
			"reason_code":              reasonCode,
			"amount":                   amount,
			"return_date":              returnDate.Format(time.RFC3339),
		}
		if destSortCode != nil {
			m["dest_sort_code"] = *destSortCode
		}
		if destAccount != nil {
			m["dest_account"] = *destAccount
		}
		if suCode != nil {
			m["su_code"] = *suCode
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ── Reports ──

func (s *LedgerService) GetCycleReports(ctx context.Context, cycleDate, bic string) ([]map[string]interface{}, error) {
	query := `SELECT s.id, s.filename, s.su_bic, s.total_volume, s.total_value, s.status::text, s.created_at
		FROM bacs_submissions s JOIN bacs_cycles c ON c.id = s.cycle_id
		WHERE c.input_date = $1`
	args := []interface{}{cycleDate}
	if bic != "" {
		query += " AND s.su_bic = $2"
		args = append(args, bic)
	}
	query += " ORDER BY s.created_at DESC"
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, filename, suBic, status string
		var totalVolume int
		var totalValue float64
		var createdAt time.Time
		if err := rows.Scan(&id, &filename, &suBic, &totalVolume, &totalValue, &status, &createdAt); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"id":           id,
			"filename":     filename,
			"su_bic":       suBic,
			"total_volume": totalVolume,
			"total_value":  totalValue,
			"status":       status,
			"created_at":   createdAt.Format(time.RFC3339),
		})
	}
	return result, rows.Err()
}

func (s *LedgerService) GetCycleSummary(ctx context.Context, cycleDate string) (map[string]interface{}, error) {
	var totalSubmissions, totalVolume int
	var totalValue float64
	err := s.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(s.total_volume),0), COALESCE(SUM(s.total_value),0)
		FROM bacs_submissions s JOIN bacs_cycles c ON c.id = s.cycle_id
		WHERE c.input_date = $1`, cycleDate).Scan(&totalSubmissions, &totalVolume, &totalValue)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"cycle_date":      cycleDate,
		"total_submissions": totalSubmissions,
		"total_volume":    totalVolume,
		"total_value":     totalValue,
	}, nil
}

// ── Limits ──

func (s *LedgerService) GetBACSLimits(ctx context.Context) (map[string]interface{}, error) {
	var totalLiquidity float64
	s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(balance),0) FROM participant_liquidity`).Scan(&totalLiquidity)
	return map[string]interface{}{
		"max_file_size":              1000000,
		"max_transactions_per_file":  100000,
		"max_submission_value":       50000000.00,
		"total_system_liquidity":     totalLiquidity,
		"settlement_cycle":           "T+2",
		"currency":                   "GBP",
	}, nil
}

// ── Schedule ──

func (s *LedgerService) GetSchedule(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, input_date, processing_date, settlement_date, status::text
		FROM bacs_cycles ORDER BY input_date ASC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id int
		var inputDate, processingDate, settlementDate time.Time
		var status string
		if err := rows.Scan(&id, &inputDate, &processingDate, &settlementDate, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"id":               id,
			"input_date":       inputDate.Format("2006-01-02"),
			"processing_date":  processingDate.Format("2006-01-02"),
			"settlement_date":  settlementDate.Format("2006-01-02"),
			"status":           status,
		})
	}
	return result, rows.Err()
}
