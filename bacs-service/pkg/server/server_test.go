package server

import (
	"context"
	"testing"
	"time"

	"bacs-service/pkg/events"
)

func TestStartScheduler_StopsOnCancel(t *testing.T) {
	s := &Server{Ledger: nil, Events: events.NewEventBus()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.StartScheduler(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
}
