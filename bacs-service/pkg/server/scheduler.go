package server

import (
	"context"
	"log"
	"math"
	"time"
)

func getDuration(cfg map[string]interface{}, key string, defaultVal float64) time.Duration {
	raw, _ := cfg[key].(float64)
	if raw <= 0 {
		raw = defaultVal
	}
	return time.Duration(raw) * time.Minute
}

func (s *Server) StartScheduler(ctx context.Context) {
	cfg := loadGlobalSchedule("bacs")

	demoVal, _ := cfg["demo_session_minutes"].(float64)
	interval := 60 * time.Second
	if demoVal > 0 {
		interval = time.Duration(math.Min(demoVal, 60)) * time.Second
	}

	processingDuration := getDuration(cfg, "processing_duration_minutes", 1440)
	settlementDuration := getDuration(cfg, "settlement_duration_minutes", 1440)
	if demoVal > 0 {
		demoDur := time.Duration(demoVal) * time.Minute
		if processingDuration > demoDur {
			processingDuration = demoDur
		}
		if settlementDuration > demoDur {
			settlementDuration = demoDur
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Scheduler started (interval=%v processing=%v settlement=%v)", interval, processingDuration, settlementDuration)

	for {
		select {
		case <-ctx.Done():
			log.Println("Scheduler stopped")
			return
		case <-ticker.C:
			if err := s.Ledger.AdvanceCycles(ctx, processingDuration, settlementDuration); err != nil {
				log.Printf("Scheduler advance cycles: %v", err)
			}
			if n, err := s.Ledger.ExecuteMandates(ctx); err != nil {
				log.Printf("Scheduler execute mandates: %v", err)
			} else if n > 0 {
				log.Printf("Scheduler executed %d mandate(s)", n)
			}
		}
	}
}
