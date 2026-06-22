package ledger

import (
	"bacs-service/pkg/auth"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
var ErrParticipantInUse = errors.New("participant has related records")
var ErrParticipantNotFound = errors.New("participant not found")

// ── Participant operations ──

func (s *LedgerService) RegisterParticipant(ctx context.Context, bic, name string, initialBalance float64, sortCode, suCode string, isSU, isDSU bool) (string, error) {
	apiKey, err := auth.GenerateAPIKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO participant_profiles (bic_code, name, su_code, sort_code, api_key, is_service_user, is_destination_user) VALUES ($1,$2,$3,$4,$5,$6,$7)`, bic, name, suCode, sortCode, apiKey, isSU, isDSU); err != nil {
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

func (s *LedgerService) GetPosition(ctx context.Context, bic string) (map[string]interface{}, error) {
	var balance float64
	var overdraft float64
	err := s.Pool.QueryRow(ctx, `
		SELECT COALESCE(l.balance, 0), COALESCE(st.overdraft_limit, 0)
		FROM participant_profiles p
		LEFT JOIN participant_liquidity l ON l.bic_code = p.bic_code
		LEFT JOIN participant_statuses st ON st.bic_code = p.bic_code
		WHERE p.bic_code = $1`, bic).Scan(&balance, &overdraft)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"bic": bic, "balance": balance, "available": balance + overdraft, "earmarked": 0}, nil
}

func (s *LedgerService) ListParticipants(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.bic_code, p.name, p.su_code, p.sort_code, p.is_service_user, p.is_destination_user,
		       COALESCE(st.status::text,'ACTIVE'), COALESCE(l.balance,0), p.currency,
		       COALESCE(st.is_closed,false), st.block_reason, COALESCE(st.overdraft_limit,0)
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
		var suCode, sortCode, blockReason *string
		var isSU, isDSU, isClosed bool
		var balance float64
		var overdraftLimit float64
		if err := rows.Scan(&bic, &name, &suCode, &sortCode, &isSU, &isDSU, &status, &balance, &currency, &isClosed, &blockReason, &overdraftLimit); err != nil {
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
			"overdraft_limit":    overdraftLimit,
		}
		if suCode != nil {
			entry["su_code"] = *suCode
		}
		if sortCode != nil {
			entry["sort_code"] = *sortCode
		}
		if blockReason != nil {
			entry["block_reason"] = *blockReason
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *LedgerService) UpdateParticipant(ctx context.Context, bic, name, sortCode string, balance float64, suCode string, isSU, isDSU bool) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE participant_profiles
			SET name = $1, sort_code = $2, su_code = NULLIF($3, ''), is_service_user = $4, is_destination_user = $5
			WHERE bic_code = $6`, name, sortCode, suCode, isSU, isDSU, bic)
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
				SELECT 1 FROM bacs_submissions WHERE su_bic = $1
				UNION ALL
				SELECT 1 FROM bacs_transactions WHERE debtor_bic = $1 OR creditor_bic = $1
				UNION ALL
				SELECT 1 FROM bacs_bilateral_positions WHERE debtor_bic = $1 OR creditor_bic = $1
				UNION ALL
				SELECT 1 FROM bacs_net_positions WHERE bic_code = $1
				UNION ALL
				SELECT 1 FROM bacs_mandates WHERE su_bic = $1
				UNION ALL
				SELECT 1 FROM bacs_journal_entries WHERE account_bic = $1
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrParticipantNotFound
		}
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

func (s *LedgerService) CloseInputDay(ctx context.Context, processingInterval, settlementInterval time.Duration) error {
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
			VALUES (CURRENT_DATE, CURRENT_DATE + $1::interval, CURRENT_DATE + $2::interval, 'OPEN')`,
			fmt.Sprintf("%d microseconds", processingInterval.Microseconds()),
			fmt.Sprintf("%d microseconds", settlementInterval.Microseconds()))
		return err
	})
}

func (s *LedgerService) ProcessCycle(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var cycleID int
		err := tx.QueryRow(ctx, `
			SELECT id FROM bacs_cycles WHERE status = 'PROCESSING'
			ORDER BY created_at ASC LIMIT 1 FOR UPDATE`).Scan(&cycleID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE bacs_transactions SET status = 'PROCESSED'
			WHERE submission_id IN (SELECT id FROM bacs_submissions WHERE cycle_id = $1 AND status = 'ACCEPTED')
			  AND status = 'PENDING'`, cycleID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE bacs_cycles SET status = 'AWAITING_SETTLEMENT'
			WHERE id = $1`, cycleID)
		return err
	})
}

func (s *LedgerService) SettleCycle(ctx context.Context) ([]string, error) {
	var bics []string
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var cycleID int
		err := tx.QueryRow(ctx, `
			SELECT c.id
			FROM bacs_cycles c
			WHERE c.status = 'AWAITING_SETTLEMENT'
			ORDER BY
				CASE WHEN EXISTS (
					SELECT 1 FROM bacs_submissions s
					WHERE s.cycle_id = c.id AND s.status = 'ACCEPTED'
				) THEN 0 ELSE 1 END,
				c.created_at ASC
			LIMIT 1 FOR UPDATE`).Scan(&cycleID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM bacs_bilateral_positions WHERE cycle_id = $1`, cycleID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM bacs_net_positions WHERE cycle_id = $1`, cycleID); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO bacs_bilateral_positions (cycle_id, debtor_bic, creditor_bic, gross_amount, net_amount)
			WITH gross AS (
				SELECT t.debtor_bic, t.creditor_bic, SUM(t.amount) AS amount
				FROM bacs_transactions t
				JOIN bacs_submissions s ON s.id = t.submission_id
				WHERE s.cycle_id = $1 AND s.status = 'ACCEPTED'
				  AND t.debtor_bic IS NOT NULL AND t.creditor_bic IS NOT NULL
				  AND t.status = 'PROCESSED'
				GROUP BY t.debtor_bic, t.creditor_bic
			),
			returns AS (
				SELECT t.debtor_bic, t.creditor_bic, SUM(r.amount) AS amount
				FROM bacs_returns r
				JOIN bacs_transactions t ON t.id = r.original_transaction_id
				JOIN bacs_submissions s ON s.id = t.submission_id
				WHERE s.cycle_id = $1
				GROUP BY t.debtor_bic, t.creditor_bic
			),
			netted AS (
				SELECT g.debtor_bic, g.creditor_bic, g.amount AS gross_amount,
					   GREATEST(g.amount - COALESCE(r.amount, 0), 0) AS net_amount
				FROM gross g
				LEFT JOIN returns r ON r.debtor_bic = g.creditor_bic AND r.creditor_bic = g.debtor_bic
			)
			SELECT $1, debtor_bic, creditor_bic, gross_amount, net_amount
			FROM netted
			WHERE net_amount > 0`, cycleID)
		if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			SELECT p.bic_code,
			       COALESCE(n.net_position, 0) AS net_position,
			       COALESCE(l.balance, 0),
			       COALESCE(st.overdraft_limit, 0)
			FROM participant_profiles p
			LEFT JOIN (
				SELECT bic_code, SUM(amount) AS net_position
				FROM (
					SELECT creditor_bic AS bic_code, net_amount AS amount
					FROM bacs_bilateral_positions WHERE cycle_id = $1
					UNION ALL
					SELECT debtor_bic AS bic_code, -net_amount AS amount
					FROM bacs_bilateral_positions WHERE cycle_id = $1
				) x
				GROUP BY bic_code
			) n ON n.bic_code = p.bic_code
			LEFT JOIN participant_liquidity l ON l.bic_code = p.bic_code
			LEFT JOIN participant_statuses st ON st.bic_code = p.bic_code
			ORDER BY p.bic_code`, cycleID)
		if err != nil {
			return err
		}

		type participantSettlement struct {
			bic            string
			netAmount      float64
			balance        float64
			overdraftLimit float64
		}
		var settlements []participantSettlement
		for rows.Next() {
			var item participantSettlement
			if err := rows.Scan(&item.bic, &item.netAmount, &item.balance, &item.overdraftLimit); err != nil {
				return err
			}
			settlements = append(settlements, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()

		anyFailed := false
		for _, item := range settlements {
			bics = append(bics, item.bic)
			posStatus := "SETTLED"
			if item.balance+item.netAmount < -item.overdraftLimit {
				posStatus = "FAILED"
				anyFailed = true
				if _, err = tx.Exec(ctx, `
					UPDATE participant_statuses
					SET status='SUSPENDED', block_reason='BACS_SESSION_LIQUIDITY_SHORTFALL',
					    blocked_at=NOW(), liquidity_breach_at=COALESCE(liquidity_breach_at, NOW()), updated_at=NOW()
					WHERE bic_code=$1`, item.bic); err != nil {
					return err
				}
			} else {
				if _, err = tx.Exec(ctx, `UPDATE participant_liquidity SET balance = balance + $1, updated_at = NOW() WHERE bic_code = $2`, item.netAmount, item.bic); err != nil {
					return err
				}
			}
			if _, err = tx.Exec(ctx, `
				INSERT INTO bacs_net_positions (cycle_id, bic_code, net_position, balance_before, overdraft_limit, status)
				VALUES ($1,$2,$3,$4,$5,$6)`, cycleID, item.bic, item.netAmount, item.balance, item.overdraftLimit, posStatus); err != nil {
				return err
			}
		}

		// Per-transaction journals and status updates
		txRows, err := tx.Query(ctx, `
			SELECT t.id, t.debtor_bic, t.creditor_bic, t.amount
			FROM bacs_transactions t
			JOIN bacs_submissions s ON s.id = t.submission_id
			WHERE s.cycle_id = $1 AND s.status = 'ACCEPTED' AND t.status = 'PROCESSED'
			ORDER BY t.id`, cycleID)
		if err != nil {
			return err
		}
		type txRec struct {
			id          int
			debtor      string
			creditor    string
			amount      float64
		}
		var allTxns []txRec
		for txRows.Next() {
			var r txRec
			if err := txRows.Scan(&r.id, &r.debtor, &r.creditor, &r.amount); err != nil {
				txRows.Close()
				return err
			}
			allTxns = append(allTxns, r)
		}
		txRows.Close()

		// Build set of failed BICs
		failedBICs := make(map[string]bool)
		for _, item := range settlements {
			if item.balance+item.netAmount < -item.overdraftLimit {
				failedBICs[item.bic] = true
			}
		}

		for _, t := range allTxns {
			if failedBICs[t.debtor] || failedBICs[t.creditor] {
				// Return this transaction
				if _, err = tx.Exec(ctx, `UPDATE bacs_transactions SET status = 'RETURNED' WHERE id = $1`, t.id); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO bacs_returns (original_transaction_id, reason_code, amount) VALUES ($1, 'LIQUIDITY', $2)`, t.id, t.amount); err != nil {
					return err
				}
			} else {
				// Settle this transaction
				if _, err = tx.Exec(ctx, `UPDATE bacs_transactions SET status = 'SETTLED' WHERE id = $1`, t.id); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `
					INSERT INTO bacs_journal_entries (transaction_id, account_bic, amount)
					VALUES ($1, $2, $3)`, t.id, t.debtor, -t.amount); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `
					INSERT INTO bacs_journal_entries (transaction_id, account_bic, amount)
					VALUES ($1, $2, $3)`, t.id, t.creditor, t.amount); err != nil {
					return err
				}
			}
		}

		cycleStatus := "SETTLED"
		if anyFailed {
			cycleStatus = "PARTIALLY_SETTLED"
		}
		_, err = tx.Exec(ctx, `UPDATE bacs_cycles SET status = $1 WHERE id = $2`, cycleStatus, cycleID)
		return err
	})
	return bics, err
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
	query := `SELECT s.id, s.filename, s.su_bic, s.total_volume, s.total_value, s.status::text, s.cycle_id, s.error_count, s.created_at,
		(
			SELECT string_agg(destination, ', ' ORDER BY destination)
			FROM (
				SELECT DISTINCT
					CASE
						WHEN dest_bic IS NOT NULL AND dest_bic <> '' AND dest_bic <> s.su_bic
							THEN dest_bic || ' (' || dest_sort_code || ')'
						ELSE dest_sort_code
					END AS destination
				FROM (
					SELECT
						CASE
							WHEN t.record_type = 'DIRECT_DEBIT' THEN t.debtor_bic
							ELSE t.creditor_bic
						END AS dest_bic,
						t.dest_sort_code
					FROM bacs_transactions t
					WHERE t.submission_id = s.id
				) tx
				WHERE dest_sort_code IS NOT NULL AND dest_sort_code <> ''
			) destinations
		) AS destinations
		FROM bacs_submissions s WHERE 1=1`
	args := []interface{}{}
	argIdx := 1
	if statusFilter != "" {
		query += " AND s.status = $" + fmt.Sprintf("%d", argIdx)
		args = append(args, statusFilter)
		argIdx++
	}
	if suBic != "" {
		query += " AND s.su_bic = $" + fmt.Sprintf("%d", argIdx)
		args = append(args, suBic)
	}
	query += " ORDER BY s.created_at DESC LIMIT 100"
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
		var destinations *string
		if err := rows.Scan(&id, &filename, &suBic2, &totalVolume, &totalValue, &status, &cycleID, &errorCount, &createdAt, &destinations); err != nil {
			return nil, err
		}
		item := map[string]interface{}{
			"id":           id,
			"filename":     filename,
			"su_bic":       suBic2,
			"total_volume": totalVolume,
			"total_value":  totalValue,
			"status":       status,
			"cycle_id":     cycleID,
			"error_count":  errorCount,
			"created_at":   createdAt.Format(time.RFC3339),
		}
		if destinations != nil {
			item["destinations"] = *destinations
		}
		result = append(result, item)
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
		var suBic string
		if err := tx.QueryRow(ctx, `SELECT su_bic FROM bacs_submissions WHERE id = $1`, submissionID).Scan(&suBic); err != nil {
			return err
		}

		sortMap, err := buildSortCodeMap(ctx, tx)
		if err != nil {
			return err
		}

		for _, d := range debits {
			destBic := lookupBICBySortCode(fmt.Sprint(d["dest_sort_code"]), suBic, sortMap)
			_, err := tx.Exec(ctx, `
				INSERT INTO bacs_transactions (submission_id, record_type, volume_header_no, dest_sort_code, dest_account, debtor_bic, creditor_bic, amount, originator_ref, reference, su_code, status)
				VALUES ($1, 'DIRECT_DEBIT', $2, $3, $4, $5, $6, $7, $8, $9, $10, 'PENDING')`,
				submissionID, d["volume_header_no"], d["dest_sort_code"], d["dest_account"],
				destBic, suBic, d["amount"], d["originator_ref"], d["reference"], d["su_code"])
			if err != nil {
				return err
			}
		}
		for _, c := range credits {
			destBic := lookupBICBySortCode(fmt.Sprint(c["dest_sort_code"]), suBic, sortMap)
			_, err := tx.Exec(ctx, `
				INSERT INTO bacs_transactions (submission_id, record_type, volume_header_no, dest_sort_code, dest_account, debtor_bic, creditor_bic, amount, originator_ref, reference, su_code, status)
				VALUES ($1, 'DIRECT_CREDIT', $2, $3, $4, $5, $6, $7, $8, $9, $10, 'PENDING')`,
				submissionID, c["volume_header_no"], c["dest_sort_code"], c["dest_account"],
				suBic, destBic, c["amount"], c["originator_ref"], c["reference"], c["su_code"])
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE bacs_submissions SET status = 'ACCEPTED' WHERE id = $1`, submissionID)
		return err
	})
}

func buildSortCodeMap(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT bic_code, sort_code FROM participant_profiles WHERE sort_code IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var bic, sc string
		if err := rows.Scan(&bic, &sc); err != nil {
			return nil, err
		}
		compact := strings.ReplaceAll(sc, "-", "")
		if len(compact) >= 6 {
			m[compact[:6]] = bic
		}
	}
	return m, rows.Err()
}

func lookupBICBySortCode(sortCode, fallback string, sortMap map[string]string) string {
	compact := strings.ReplaceAll(sortCode, "-", "")
	if len(compact) >= 6 {
		compact = compact[:6]
	}
	if bic, ok := sortMap[compact]; ok {
		return bic
	}
	return fallback
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
		INSERT INTO bacs_mandates (reference, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency, next_execution_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() + INTERVAL '1 day')
		RETURNING id`, ref, suBic, payerName, sortCode, account, amount, frequency).Scan(&id)
	return id, err
}

func (s *LedgerService) GetMandate(ctx context.Context, ref string) (map[string]interface{}, error) {
	var id int
	var suBic string
	var payerName, payerSortCode, payerAccount, frequency, status *string
	var amount *float64
	var nextExec *time.Time
	var createdAt time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT id, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency, status, next_execution_date, created_at
		FROM bacs_mandates WHERE reference = $1`, ref).
		Scan(&id, &suBic, &payerName, &payerSortCode, &payerAccount, &amount, &frequency, &status, &nextExec, &createdAt)
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
	if nextExec != nil {
		m["next_execution_date"] = nextExec.Format(time.RFC3339)
	}
	return m, nil
}

func (s *LedgerService) ListMandates(ctx context.Context, suBic string) ([]map[string]interface{}, error) {
	query := `SELECT id, reference, su_bic, payer_name, payer_sort_code, payer_account, amount, frequency, status, next_execution_date, created_at FROM bacs_mandates`
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
		var nextExec *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &ref, &suBic2, &payerName, &payerSortCode, &payerAccount, &amount, &frequency, &status, &nextExec, &createdAt); err != nil {
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
		if nextExec != nil {
			m["next_execution_date"] = nextExec.Format(time.RFC3339)
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

func (s *LedgerService) GetNettingReport(ctx context.Context, cycleDate, bic string) (map[string]interface{}, error) {
	var cycleID int
	var status string
	err := s.Pool.QueryRow(ctx, `SELECT id, status::text FROM bacs_cycles WHERE input_date = $1 ORDER BY created_at DESC LIMIT 1`, cycleDate).Scan(&cycleID, &status)
	if err != nil {
		return nil, err
	}

	bilateralQuery := `SELECT debtor_bic, creditor_bic, gross_amount, net_amount FROM bacs_bilateral_positions WHERE cycle_id = $1`
	args := []interface{}{cycleID}
	if bic != "" {
		bilateralQuery += ` AND (debtor_bic = $2 OR creditor_bic = $2)`
		args = append(args, bic)
	}
	bilateralQuery += ` ORDER BY debtor_bic, creditor_bic`
	rows, err := s.Pool.Query(ctx, bilateralQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bilateral := []map[string]interface{}{}
	for rows.Next() {
		var debtor, creditor string
		var gross, net float64
		if err := rows.Scan(&debtor, &creditor, &gross, &net); err != nil {
			return nil, err
		}
		bilateral = append(bilateral, map[string]interface{}{
			"debtor_bic":   debtor,
			"creditor_bic": creditor,
			"gross_amount": gross,
			"net_amount":   net,
		})
	}

	netQuery := `SELECT bic_code, net_position, balance_before, overdraft_limit, status FROM bacs_net_positions WHERE cycle_id = $1`
	netArgs := []interface{}{cycleID}
	if bic != "" {
		netQuery += ` AND bic_code = $2`
		netArgs = append(netArgs, bic)
	}
	netQuery += ` ORDER BY bic_code`
	netRows, err := s.Pool.Query(ctx, netQuery, netArgs...)
	if err != nil {
		return nil, err
	}
	defer netRows.Close()
	netPositions := []map[string]interface{}{}
	for netRows.Next() {
		var bank, posStatus string
		var position, balanceBefore, overdraftLimit float64
		if err := netRows.Scan(&bank, &position, &balanceBefore, &overdraftLimit, &posStatus); err != nil {
			return nil, err
		}
		netPositions = append(netPositions, map[string]interface{}{
			"bic":             bank,
			"net_position":    position,
			"balance_before":  balanceBefore,
			"overdraft_limit": overdraftLimit,
			"status":          posStatus,
		})
	}
	return map[string]interface{}{
		"cycle_id":      cycleID,
		"cycle_date":    cycleDate,
		"cycle_status":  status,
		"bilateral":     bilateral,
		"net_positions": netPositions,
	}, nil
}

// ── Limits ──

func (s *LedgerService) GetBACSLimits(ctx context.Context) (map[string]interface{}, error) {
	var totalLiquidity float64
	if err := s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM(balance),0) FROM participant_liquidity`).Scan(&totalLiquidity); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"max_file_size":              1000000,
		"max_transactions_per_file":  100000,
		"max_submission_value":       50000000.00,
		"total_system_liquidity":     totalLiquidity,
		"settlement_cycle":           "T+2",
		"currency":                   "GBP",
	}, nil
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

// ── Scheduler ──

func (s *LedgerService) AdvanceCycles(ctx context.Context, processingDuration, settlementDuration time.Duration) error {
	var hasWork bool
	pgInterval := fmt.Sprintf("%d microseconds", processingDuration.Microseconds())
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bacs_cycles WHERE status='OPEN' AND created_at + $1::interval <= NOW())", pgInterval).Scan(&hasWork); err == nil && hasWork {
		if err := s.CloseInputDay(ctx, processingDuration, settlementDuration); err != nil {
			log.Printf("AdvanceCycles CloseInputDay: %v", err)
		}
	}
	totalInterval := fmt.Sprintf("%d microseconds", (processingDuration + settlementDuration).Microseconds())
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bacs_cycles WHERE status='PROCESSING' AND created_at + $1::interval <= NOW())", totalInterval).Scan(&hasWork); err == nil && hasWork {
		if err := s.ProcessCycle(ctx); err != nil {
			log.Printf("AdvanceCycles ProcessCycle: %v", err)
		}
	}
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM bacs_cycles WHERE status='AWAITING_SETTLEMENT' AND created_at + $1::interval <= NOW())", totalInterval).Scan(&hasWork); err == nil && hasWork {
		if _, err := s.SettleCycle(ctx); err != nil {
			log.Printf("AdvanceCycles SettleCycle: %v", err)
		}
	}
	return nil
}

// ── AUDDIS (Automated Direct Debit Instruction Service) ──

func (s *LedgerService) ExecuteMandates(ctx context.Context) (int, error) {
	executed := 0
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.id, m.reference, m.su_bic, m.payer_sort_code, m.payer_account, m.amount, m.frequency
			FROM bacs_mandates m
			WHERE m.status = 'ACTIVE'
			  AND m.next_execution_date IS NOT NULL
			  AND m.next_execution_date <= NOW()
			ORDER BY m.next_execution_date ASC
			FOR UPDATE OF m SKIP LOCKED`)
		if err != nil {
			return err
		}
		defer rows.Close()

		type mandateItem struct {
			id         int
			ref        string
			suBic      string
			sortCode   string
			account    string
			amount     float64
			frequency  string
		}
		var mandates []mandateItem
		for rows.Next() {
			var m mandateItem
			if err := rows.Scan(&m.id, &m.ref, &m.suBic, &m.sortCode, &m.account, &m.amount, &m.frequency); err != nil {
				return err
			}
			mandates = append(mandates, m)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()

		if len(mandates) == 0 {
			return nil
		}

		// Find or create an OPEN cycle
		var cycleID int
		err = tx.QueryRow(ctx, `SELECT id FROM bacs_cycles WHERE status = 'OPEN' ORDER BY created_at DESC LIMIT 1 FOR UPDATE`).Scan(&cycleID)
		if err != nil {
			// Create a new cycle
			err = tx.QueryRow(ctx, `
				INSERT INTO bacs_cycles (input_date, processing_date, settlement_date, status)
				VALUES (CURRENT_DATE, CURRENT_DATE + 1, CURRENT_DATE + 2, 'OPEN')
				RETURNING id`).Scan(&cycleID)
			if err != nil {
				return err
			}
		}

		for _, m := range mandates {
			// Create a submission for this mandate execution
			var submissionID string
			err = tx.QueryRow(ctx, `
				INSERT INTO bacs_submissions (filename, su_bic, total_volume, total_value, status, cycle_id)
				VALUES ($1, $2, 1, $3, 'ACCEPTED', $4)
				RETURNING id`,
				fmt.Sprintf("AUDDIS-%s-%s", m.ref, time.Now().Format("20060102")),
				m.suBic, m.amount, cycleID).Scan(&submissionID)
			if err != nil {
				return err
			}

			// Create a direct debit transaction
			_, err = tx.Exec(ctx, `
				INSERT INTO bacs_transactions (submission_id, record_type, volume_header_no, dest_sort_code, dest_account, debtor_bic, creditor_bic, amount, originator_ref, reference, su_code, status)
				VALUES ($1, 'DIRECT_DEBIT', 1, $2, $3, $4, $5, $6, $7, $7, '', 'PENDING')`,
				submissionID, m.sortCode, m.account, m.suBic, m.suBic, m.amount, m.ref)
			if err != nil {
				return err
			}

			// Update next execution date
			var nextDate time.Time
			switch m.frequency {
			case "WEEKLY":
				nextDate = time.Now().AddDate(0, 0, 7)
			case "MONTHLY":
				nextDate = time.Now().AddDate(0, 1, 0)
			case "QUARTERLY":
				nextDate = time.Now().AddDate(0, 3, 0)
			case "YEARLY":
				nextDate = time.Now().AddDate(1, 0, 0)
			default:
				nextDate = time.Now().AddDate(0, 1, 0)
			}
			_, err = tx.Exec(ctx, `UPDATE bacs_mandates SET next_execution_date = $1 WHERE id = $2`, nextDate, m.id)
			if err != nil {
				return err
			}
			executed++
		}
		return nil
	})
	return executed, err
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
