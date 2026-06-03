package server

import (
	"context"
	"log"
	"math"
	"time"
)

func (s *Server) StartScheduler(ctx context.Context) {
	cfg := loadGlobalSchedule("bacs")

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
			if err := s.Ledger.AdvanceCycles(ctx); err != nil {
				log.Printf("Scheduler advance cycles: %v", err)
			}
		}
	}
}
