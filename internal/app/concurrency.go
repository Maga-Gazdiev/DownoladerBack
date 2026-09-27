package app

import (
	"context"
	"sync"

	apperrors "video-downloader/internal/errors"
)

func (s *tasks) Start(ctx context.Context, run func(context.Context)) error {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.stopped || s.ctx.Err() != nil {
		return context.Canceled
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return apperrors.ErrBusy
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.slots }()
		run(s.ctx)
	}()
	return nil
}
func (s *tasks) Close() {
	s.workMu.Lock()
	s.stopped = true
	s.cancel()
	s.workMu.Unlock()
	s.wg.Wait()
}

type tasks struct {
	ctx     context.Context
	cancel  context.CancelFunc
	workMu  sync.Mutex
	slots   chan struct{}
	stopped bool
	wg      sync.WaitGroup
}

func newTasks(ctx context.Context, limit int) *tasks {
	ctx, cancel := context.WithCancel(ctx)
	return &tasks{ctx: ctx, cancel: cancel, slots: make(chan struct{}, limit)}
}
