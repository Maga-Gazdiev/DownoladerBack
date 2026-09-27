package app

import (
	"context"
	"errors"
	"testing"
	"time"
	apperrors "video-downloader/internal/errors"
)

func TestCapacityAndShutdown(t *testing.T) {
	tasks := newTasks(context.Background(), 2)
	started := make(chan struct{}, 2)
	run := func(ctx context.Context) { started <- struct{}{}; <-ctx.Done() }
	for range 2 {
		if err := tasks.Start(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("tasks did not run concurrently")
		}
	}
	if err := tasks.Start(context.Background(), run); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("full: %v", err)
	}
	tasks.Close()
	if err := tasks.Start(context.Background(), run); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed: %v", err)
	}
}
func TestRequestCancellationDoesNotCancelAcceptedTask(t *testing.T) {
	tasks := newTasks(context.Background(), 1)
	defer tasks.Close()
	request, cancel := context.WithCancel(context.Background())
	inspect := make(chan struct{})
	result := make(chan error, 1)
	if err := tasks.Start(request, func(ctx context.Context) { <-inspect; result <- ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	cancel()
	close(inspect)
	if err := <-result; err != nil {
		t.Fatalf("request canceled task: %v", err)
	}
	if err := tasks.Start(request, func(context.Context) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("accepted canceled request: %v", err)
	}
}
