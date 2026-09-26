package worker

import (
	"context"
	"log/slog"
	"time"
)

type Cleaner interface {
	Sweep(context.Context, time.Time) error
}
type Cleanup struct {
	cleaner  Cleaner
	interval time.Duration
}

func NewCleanup(c Cleaner, interval time.Duration) *Cleanup {
	return &Cleanup{cleaner: c, interval: interval}
}
func (w *Cleanup) Run(ctx context.Context) error {
	clean := func() {
		if err := w.cleaner.Sweep(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Warn("file cleanup failed", "error", err)
		}
	}
	clean()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			clean()
		}
	}
}
