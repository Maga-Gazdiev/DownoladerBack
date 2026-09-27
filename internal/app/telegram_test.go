package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	apperrors "video-downloader/internal/errors"

	"video-downloader/internal/model"
	telegramservice "video-downloader/internal/service/telegram"
	"video-downloader/internal/service/web"
)

type source struct{ calls atomic.Int32 }

func (s *source) Download(context.Context, string) (*model.Media, error) {
	s.calls.Add(1)
	return model.NewMedia(model.File{Name: "test.mp4"}, []byte("video"), nil), nil
}

type client struct {
	started chan struct{}
	calls   atomic.Int32
}

func (c *client) SendVideo(ctx context.Context, _ int64, _ *model.Media) error {
	c.calls.Add(1)
	close(c.started)
	<-ctx.Done()
	return ctx.Err()
}
func TestDuplicateUpdateDoesNotStartAnotherTask(t *testing.T) {
	d := &source{}
	c := &client{started: make(chan struct{})}
	tasks := newTasks(context.Background(), 2)
	s := telegramservice.New(tasks, d, c, time.Minute, time.Minute)
	defer tasks.Close()
	if err := s.Submit(context.Background(), 1, 42, "https://youtu.be/test"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("not started")
	}
	for range 10 {
		if err := s.Submit(context.Background(), 1, 42, "https://youtu.be/test"); err != nil {
			t.Fatal(err)
		}
	}
	if d.calls.Load() != 1 || c.calls.Load() != 1 {
		t.Fatal("duplicate delivery")
	}
	tasks.Close()
}

func TestWebAndTelegramShareConcurrencyLimit(t *testing.T) {
	d := &source{}
	c := &client{started: make(chan struct{})}
	tasks := newTasks(context.Background(), 1)
	s := telegramservice.New(tasks, d, c, time.Minute, time.Minute)
	w := web.New(tasks, d, time.Minute, time.Minute, 10)
	defer w.Close()
	defer tasks.Close()
	if err := s.Submit(context.Background(), 1, 42, "https://youtu.be/test"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("Telegram task did not start")
	}
	if _, err := w.Create(context.Background(), "https://youtu.be/test"); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("web exceeded shared limit: %v", err)
	}
	tasks.Close()
	if _, err := w.Create(context.Background(), "https://youtu.be/test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("web accepted task after shutdown: %v", err)
	}
	if err := s.Submit(context.Background(), 2, 42, "https://youtu.be/test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Telegram accepted task after shutdown: %v", err)
	}
}
