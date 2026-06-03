package server

import (
	"context"
	"log"
	"math"
	"time"
)

func (s *Server) StartScheduler(ctx context.Context) {
	cfg := loadGlobalSchedule("fps")

	demoVal, _ := cfg["demo_session_minutes"].(float64)
	interval := 60 * time.Second
	if demoVal > 0 {
		interval = time.Duration(math.Min(demoVal, 60)) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Scheduler started (interval=%v)", interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			s.runScheduledTasks(ctx)
		}
	}
}

func (s *Server) runScheduledTasks(ctx context.Context) {
	if err := s.Ledger.ExecuteForwardDated(ctx); err != nil {
		log.Printf("Scheduler forward-dated: %v", err)
	}
	if err := s.Ledger.ExecuteStandingOrders(ctx); err != nil {
		log.Printf("Scheduler standing orders: %v", err)
	}
	if err := s.Ledger.CloseExpiredDNSCycles(ctx); err != nil {
		log.Printf("Scheduler DNS cycles: %v", err)
	}
}
