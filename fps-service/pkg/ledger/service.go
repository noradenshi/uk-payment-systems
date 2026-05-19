package ledger

import (
	"context"
	"fmt"
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

const fpsSinglePaymentLimit = 1000000.00
const fpsDailyParticipantLimit = 10000000.00

type SettlementResult struct {
	Status     string
	ReasonCode string
}

func (s *LedgerService) RegisterParticipant(ctx context.Context, bic, name string, initialBalance float64, pType, sponsorBic string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO participant_profiles (bic_code, name, participant_type, sponsor_bic) VALUES ($1, $2, $3, NULLIF($4, ''))", bic, name, pType, sponsorBic); err != nil {
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

func (s *LedgerService) ListParticipants(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.bic_code, p.name, COALESCE(st.status::text, 'ACTIVE'), COALESCE(l.balance, 0), p.currency, p.participant_type, p.sponsor_bic
		FROM participant_profiles p LEFT JOIN participant_statuses st ON st.bic_code = p.bic_code LEFT JOIN participant_liquidity l ON l.bic_code = p.bic_code ORDER BY p.bic_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var bic, name, status, currency, pType string
		var balance float64
		var sponsorBic *string
		if err := rows.Scan(&bic, &name, &status, &balance, &currency, &pType, &sponsorBic); err != nil {
			return nil, err
		}
		entry := map[string]interface{}{"bic": bic, "name": name, "status": status, "balance": balance, "currency": currency, "participant_type": pType}
		if sponsorBic != nil {
			entry["sponsor_bic"] = *sponsorBic
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *LedgerService) UpdateParticipantStatus(ctx context.Context, bic, status, reason string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE participant_statuses SET status = $1::participant_status, block_reason = NULLIF($2, ''), blocked_at = CASE WHEN $1 = 'SUSPENDED' THEN NOW() ELSE NULL END, updated_at = NOW() WHERE bic_code = $3", status, reason, bic)
	return err
}

func (s *LedgerService) BlockParticipant(ctx context.Context, bic, reason string) error {
	return s.UpdateParticipantStatus(ctx, bic, "SUSPENDED", reason)
}

func (s *LedgerService) GetBlockDetails(ctx context.Context, bic string) (map[string]interface{}, error) {
	var status string
	var blockedAt *time.Time
	var reason *string
	err := s.Pool.QueryRow(ctx, "SELECT status::text, blocked_at, block_reason FROM participant_statuses WHERE bic_code = $1", bic).Scan(&status, &blockedAt, &reason)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"bic": bic, "status": status, "blocked_at": blockedAt, "reason": reason}, nil
}

func (s *LedgerService) UnblockParticipant(ctx context.Context, bic string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE participant_statuses SET status = 'ACTIVE', block_reason = NULL, blocked_at = NULL, updated_at = NOW() WHERE bic_code = $1", bic)
	return err
}

func (s *LedgerService) GetPosition(ctx context.Context, bic string) (map[string]interface{}, error) {
	var balance float64
	err := s.Pool.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code = $1", bic).Scan(&balance)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"bic": bic, "balance": balance, "available": balance, "earmarked": 0}, nil
}

func (s *LedgerService) SettleSIP(ctx context.Context, msgID, sender, receiver string, amount float64, endToEndID string) (SettlementResult, error) {
	var result SettlementResult
	if amount > fpsSinglePaymentLimit {
		return SettlementResult{Status: "RJCT", ReasonCode: "AM05"}, nil
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var internalUUID pgtype.UUID
		var currentStatus string
		var existingSender, existingReceiver string
		var existingAmount float64
		err := tx.QueryRow(ctx, `
			INSERT INTO fps_transactions (msg_id, sender_bic, receiver_bic, amount, status, end_to_end_id, payment_type)
			VALUES ($1, $2, $3, $4, 'PENDING', $5, 'SIP')
			ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
			RETURNING id, status, sender_bic, receiver_bic, amount`,
			msgID, sender, receiver, amount, endToEndID).Scan(&internalUUID, &currentStatus, &existingSender, &existingReceiver, &existingAmount)
		if err != nil {
			return fmt.Errorf("failed to init: %w", err)
		}
		if currentStatus == "SETTLED" {
			if existingSender != sender || existingReceiver != receiver || existingAmount != amount {
				result = SettlementResult{Status: "RJCT", ReasonCode: "AM05"}
				return nil
			}
			result = SettlementResult{Status: "ACTC", ReasonCode: ""}
			return nil
		}
		for _, bic := range []string{sender, receiver} {
			var ps string
			var closed bool
			err = tx.QueryRow(ctx, "SELECT status, is_closed FROM participant_statuses WHERE bic_code = $1 FOR UPDATE", bic).Scan(&ps, &closed)
			if err != nil || closed || ps != "ACTIVE" {
				result = SettlementResult{Status: "RJCT", ReasonCode: "AC04"}
				tx.Exec(ctx, "UPDATE fps_transactions SET status = 'REJECTED' WHERE id = $1", internalUUID)
				return nil
			}
		}
		var dayTotal float64
		tx.QueryRow(ctx, "SELECT COALESCE(SUM(amount),0) FROM fps_transactions WHERE sender_bic=$1 AND status='SETTLED' AND created_at>=CURRENT_DATE", sender).Scan(&dayTotal)
		if dayTotal+amount > fpsDailyParticipantLimit {
			result = SettlementResult{Status: "RJCT", ReasonCode: "AM05"}
			tx.Exec(ctx, "UPDATE fps_transactions SET status='REJECTED' WHERE id=$1", internalUUID)
			return nil
		}
		var balance float64
		err = tx.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code = $1 FOR UPDATE", sender).Scan(&balance)
		if err != nil {
			return err
		}
		if balance < amount {
			result = SettlementResult{Status: "PDNG", ReasonCode: "INSU"}
			tx.Exec(ctx, "UPDATE fps_transactions SET status='QUEUED' WHERE id=$1", internalUUID)
			return nil
		}
		var recvBalance float64
		if err := tx.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code=$1 FOR UPDATE", receiver).Scan(&recvBalance); err != nil {
			return fmt.Errorf("receiver lock failed: %w", err)
		}
		if _, err = tx.Exec(ctx, "UPDATE participant_liquidity SET balance=balance-$1 WHERE bic_code=$2", amount, sender); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE participant_liquidity SET balance=balance+$1 WHERE bic_code=$2", amount, receiver); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO fps_journal_entries (transaction_id, account_bic, amount) VALUES ($1,$2,$3),($1,$4,$5)", internalUUID, sender, -amount, receiver, amount)
		if err != nil {
			return err
		}
		result = SettlementResult{Status: "ACTC", ReasonCode: ""}
		_, err = tx.Exec(ctx, "UPDATE fps_transactions SET status='SETTLED' WHERE id=$1", internalUUID)
		return nil
	})
	return result, err
}

func (s *LedgerService) CreateForwardDated(ctx context.Context, msgID, sender, receiver string, amount float64, execDate time.Time) error {
	_, err := s.Pool.Exec(ctx, "INSERT INTO fps_forward_dated (msg_id, sender_bic, receiver_bic, amount, execution_date, status) VALUES ($1,$2,$3,$4,$5,'SCHEDULED')", msgID, sender, receiver, amount, execDate)
	return err
}

func (s *LedgerService) ListForwardDated(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id, msg_id, sender_bic, receiver_bic, amount, execution_date, status FROM fps_forward_dated ORDER BY execution_date")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var id, msgID, sender, receiver, status string
		var amount float64
		var execDate time.Time
		if err := rows.Scan(&id, &msgID, &sender, &receiver, &amount, &execDate, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"id": id, "msg_id": msgID, "sender_bic": sender, "receiver_bic": receiver, "amount": amount, "execution_date": execDate, "status": status})
	}
	return result, nil
}

func (s *LedgerService) CancelForwardDated(ctx context.Context, id string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, "DELETE FROM fps_forward_dated WHERE id=$1::uuid AND status='SCHEDULED'", id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *LedgerService) CreateStandingOrder(ctx context.Context, ref, sender, receiver string, amount float64, freq string, nextDate, endDate time.Time) error {
	_, err := s.Pool.Exec(ctx, "INSERT INTO fps_standing_orders (reference, sender_bic, receiver_bic, amount, frequency, next_date, end_date, status) VALUES ($1,$2,$3,$4,$5,$6,$7,'ACTIVE')", ref, sender, receiver, amount, freq, nextDate, endDate)
	return err
}

func (s *LedgerService) ListStandingOrders(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id, reference, sender_bic, receiver_bic, amount, frequency, next_date, end_date, status FROM fps_standing_orders ORDER BY next_date")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var id, ref, sender, receiver, freq, status string
		var amount float64
		var nextDate, endDate time.Time
		if err := rows.Scan(&id, &ref, &sender, &receiver, &amount, &freq, &nextDate, &endDate, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"id": id, "reference": ref, "sender_bic": sender, "receiver_bic": receiver, "amount": amount, "frequency": freq, "next_date": nextDate, "end_date": endDate, "status": status})
	}
	return result, nil
}

func (s *LedgerService) GetStandingOrder(ctx context.Context, id string) (map[string]interface{}, error) {
	var ref, sender, receiver, freq, status string
	var amount float64
	var nextDate, endDate time.Time
	err := s.Pool.QueryRow(ctx, "SELECT reference, sender_bic, receiver_bic, amount, frequency, next_date, end_date, status FROM fps_standing_orders WHERE id=$1::uuid", id).Scan(&ref, &sender, &receiver, &amount, &freq, &nextDate, &endDate, &status)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "reference": ref, "sender_bic": sender, "receiver_bic": receiver, "amount": amount, "frequency": freq, "next_date": nextDate, "end_date": endDate, "status": status}, nil
}

func (s *LedgerService) UpdateStandingOrder(ctx context.Context, id, freq string, amount float64, nextDate, endDate time.Time) error {
	_, err := s.Pool.Exec(ctx, "UPDATE fps_standing_orders SET frequency=COALESCE(NULLIF($1,''), frequency), amount=COALESCE(NULLIF($2,0), amount), next_date=COALESCE($3, next_date), end_date=COALESCE($4, end_date) WHERE id=$5::uuid", freq, amount, nextDate, endDate, id)
	return err
}

func (s *LedgerService) CancelStandingOrder(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE fps_standing_orders SET status='CANCELLED' WHERE id=$1::uuid", id)
	return err
}

func (s *LedgerService) CreateBulkSubmission(ctx context.Context, filename, sender string, totalItems int, totalValue float64) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, "INSERT INTO fps_bulk_submissions (filename, sender_bic, total_items, total_value, status) VALUES ($1,$2,$3,$4,'RECEIVED') RETURNING id", filename, sender, totalItems, totalValue).Scan(&id)
	return id, err
}

func (s *LedgerService) GetBulkSubmission(ctx context.Context, id string) (map[string]interface{}, error) {
	var filename, sender, status, uuid string
	var items int
	var value float64
	var createdAt time.Time
	err := s.Pool.QueryRow(ctx, "SELECT id, filename, sender_bic, total_items, total_value, status, created_at FROM fps_bulk_submissions WHERE id=$1::uuid", id).Scan(&uuid, &filename, &sender, &items, &value, &status, &createdAt)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": uuid, "filename": filename, "sender_bic": sender, "total_items": items, "total_value": value, "status": status, "created_at": createdAt}, nil
}

func (s *LedgerService) ListBulkSubmissions(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id, filename, sender_bic, total_items, total_value, status, created_at FROM fps_bulk_submissions ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var uuid, filename, sender, status string
		var items int
		var value float64
		var createdAt time.Time
		if err := rows.Scan(&uuid, &filename, &sender, &items, &value, &status, &createdAt); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"id": uuid, "filename": filename, "sender_bic": sender, "total_items": items, "total_value": value, "status": status, "created_at": createdAt})
	}
	return result, nil
}

func (s *LedgerService) GetCurrentDNS(ctx context.Context) (map[string]interface{}, error) {
	var id int
	var start, end time.Time
	var status string
	err := s.Pool.QueryRow(ctx, "SELECT id, cycle_start, cycle_end, status FROM fps_dns_cycles WHERE status='OPEN' LIMIT 1").Scan(&id, &start, &end, &status)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "cycle_start": start, "cycle_end": end, "status": status}, nil
}

func (s *LedgerService) CloseDNSCycle(ctx context.Context) ([]map[string]interface{}, error) {
	var cycleID int
	var start, end time.Time
	err := s.Pool.QueryRow(ctx, "SELECT id, cycle_start, cycle_end FROM fps_dns_cycles WHERE status='OPEN' LIMIT 1 FOR UPDATE").Scan(&cycleID, &start, &end)
	if err != nil {
		return nil, fmt.Errorf("no open DNS cycle")
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT bic_code, COALESCE(SUM(CASE WHEN bic_code = sender_bic THEN -amount WHEN bic_code = receiver_bic THEN amount ELSE 0 END), 0) AS net
		FROM fps_transactions, participant_profiles
		WHERE (sender_bic = bic_code OR receiver_bic = bic_code) AND status = 'QUEUED'
		GROUP BY bic_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var netResults []map[string]interface{}
	for rows.Next() {
		var bic string
		var net float64
		if err := rows.Scan(&bic, &net); err != nil {
			return nil, err
		}
		netResults = append(netResults, map[string]interface{}{"bic": bic, "net_position": net})
	}
	s.Pool.Exec(ctx, "UPDATE fps_transactions SET status='SETTLED' WHERE status='QUEUED'")
	s.Pool.Exec(ctx, "UPDATE fps_dns_cycles SET status='CLOSED', settled_at=NOW() WHERE id=$1", cycleID)
	return netResults, nil
}

func (s *LedgerService) GetDNSHistory(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := s.Pool.Query(ctx, "SELECT id, cycle_start, cycle_end, settled_at, status FROM fps_dns_cycles WHERE status='CLOSED' ORDER BY cycle_start DESC LIMIT 20")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var id int
		var start, end time.Time
		var settledAt *time.Time
		var status string
		if err := rows.Scan(&id, &start, &end, &settledAt, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"id": id, "cycle_start": start, "cycle_end": end, "settled_at": settledAt, "status": status})
	}
	return result, nil
}

func (s *LedgerService) TopUpLiquidity(ctx context.Context, bic string, amount float64) error {
	_, err := s.Pool.Exec(ctx, "UPDATE participant_liquidity SET balance=balance+$1, updated_at=NOW() WHERE bic_code=$2", amount, bic)
	return err
}

func (s *LedgerService) GetFPSLimits(ctx context.Context, bic string) (map[string]interface{}, error) {
	result := map[string]interface{}{"currency": "GBP", "single_payment_limit": fpsSinglePaymentLimit, "daily_participant_limit": fpsDailyParticipantLimit}
	var totalLiquidity float64
	s.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(balance),0) FROM participant_liquidity").Scan(&totalLiquidity)
	result["total_available_liquidity"] = totalLiquidity
	if bic != "" {
		var bal float64
		s.Pool.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code=$1", bic).Scan(&bal)
		result["remaining_intraday_liquidity"] = bal
	} else {
		result["remaining_intraday_liquidity"] = totalLiquidity
	}
	return result, nil
}

func (s *LedgerService) GetPrefundedBalance(ctx context.Context, bic string) (map[string]interface{}, error) {
	var bal float64
	err := s.Pool.QueryRow(ctx, "SELECT balance FROM participant_liquidity WHERE bic_code=$1", bic).Scan(&bal)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"bic": bic, "prefunded_balance": bal}, nil
}

func (s *LedgerService) ListPayments(ctx context.Context, statusFilter string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := "SELECT msg_id, sender_bic, receiver_bic, amount, status::text, created_at, payment_type FROM fps_transactions"
	var args []interface{}
	if statusFilter != "" {
		q += " WHERE status = $1"
		args = append(args, statusFilter)
	}
	q += " ORDER BY created_at DESC LIMIT $2"
	args = append(args, limit)
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var msgID, sender, receiver, status, pType string
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&msgID, &sender, &receiver, &amount, &status, &createdAt, &pType); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{"msg_id": msgID, "sender_bic": sender, "receiver_bic": receiver, "amount": amount, "status": status, "created_at": createdAt, "payment_type": pType})
	}
	return result, nil
}

func (s *LedgerService) GetPaymentDetails(ctx context.Context, msgID string) (map[string]interface{}, error) {
	var id, sender, receiver, status, endToEndID, pType string
	var amount float64
	var createdAt time.Time
	err := s.Pool.QueryRow(ctx, "SELECT id::text, sender_bic, receiver_bic, amount, status::text, COALESCE(end_to_end_id,''), payment_type, created_at FROM fps_transactions WHERE msg_id=$1", msgID).Scan(&id, &sender, &receiver, &amount, &status, &endToEndID, &pType, &createdAt)
	if err != nil {
		return nil, err
	}
	rows, _ := s.Pool.Query(ctx, "SELECT account_bic, amount FROM fps_journal_entries WHERE transaction_id=$1::uuid", id)
	defer rows.Close()
	var journal []map[string]interface{}
	for rows.Next() {
		var bic string
		var amt float64
		if err := rows.Scan(&bic, &amt); err == nil {
			journal = append(journal, map[string]interface{}{"bic": bic, "amount": amt})
		}
	}
	return map[string]interface{}{"msg_id": msgID, "sender_bic": sender, "receiver_bic": receiver, "amount": amount, "status": status, "end_to_end_id": endToEndID, "payment_type": pType, "created_at": createdAt, "audit_trail": journal}, nil
}

func (s *LedgerService) RecallPayment(ctx context.Context, msgID string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, "UPDATE fps_transactions SET status='REJECTED' WHERE msg_id=$1 AND status='PENDING'", msgID)
	return tag.RowsAffected() > 0, err
}
