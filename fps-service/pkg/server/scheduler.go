package server

import (
	"context"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

func (s *Server) StartScheduler(ctx context.Context) {
	cfg := loadGlobalSchedule("fps")

	mode, _ := cfg["mode"].(string)
	demoVal, _ := cfg["demo_session_minutes"].(float64)
	isDemo := mode == "demo" && demoVal > 0

	interval := 60 * time.Second
	if isDemo {
		interval = time.Duration(math.Min(demoVal, 60)) * time.Second
	}

	warningPct := 80.0
	if wp, ok := cfg["limit_warning_pct"].(float64); ok && wp > 0 {
		warningPct = wp
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Scheduler started (mode=%s interval=%v warning_threshold=%.0f%%)", mode, interval, warningPct)

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			s.runScheduledTasks(ctx, cfg, warningPct)
		}
	}
}

func (s *Server) runScheduledTasks(ctx context.Context, cfg map[string]interface{}, warningPct float64) {
	if err := s.Ledger.ExecuteForwardDated(ctx); err != nil {
		log.Printf("Scheduler forward-dated: %v", err)
	}
	if err := s.Ledger.ExecuteStandingOrders(ctx); err != nil {
		log.Printf("Scheduler standing orders: %v", err)
	}
	if err := s.Ledger.EnforceRealtimeLiquidityBlocks(ctx); err != nil {
		log.Printf("Scheduler enforce liquidity blocks: %v", err)
	}
	if err := s.Ledger.CheckWarningThresholds(ctx, warningPct); err != nil {
		log.Printf("Scheduler warning thresholds: %v", err)
	}
	s.manageDNSCycles(ctx, cfg)
}

func (s *Server) manageDNSCycles(ctx context.Context, cfg map[string]interface{}) {
	mode, _ := cfg["mode"].(string)
	demoVal, _ := cfg["demo_session_minutes"].(float64)
	isDemo := mode == "demo" && demoVal > 0

	var nextCycleEnd time.Time
	if isDemo {
		nextCycleEnd = time.Now().Add(time.Duration(demoVal) * time.Minute)
	} else {
		nextCycleEnd = nextSettlementTime(cfg, time.Now())
	}

	if err := s.Ledger.CloseExpiredDNSCycles(ctx, nextCycleEnd); err != nil {
		log.Printf("Scheduler DNS cycles: %v", err)
	}
}

func nextSettlementTime(cfg map[string]interface{}, now time.Time) time.Time {
	raw, ok := cfg["settlement_times"].([]interface{})
	if !ok || len(raw) == 0 {
		return now.Add(2 * time.Hour)
	}

	var times []int
	for _, r := range raw {
		s, _ := r.(string)
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			continue
		}
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		times = append(times, h*60+m)
	}

	if len(times) == 0 {
		return now.Add(2 * time.Hour)
	}

	nowMinutes := now.Hour()*60 + now.Minute()
	for _, t := range times {
		if t > nowMinutes {
			return time.Date(now.Year(), now.Month(), now.Day(), t/60, t%60, 0, 0, now.Location())
		}
	}

	return time.Date(now.Year(), now.Month(), now.Day(), times[0]/60, times[0]%60, 0, 0, now.Location()).AddDate(0, 0, 1)
}
