package download

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"video-downloader/internal/service/model"
)

type blockingDownloader struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (d *blockingDownloader) Download(ctx context.Context, _, _ string) (model.File, error) {
	d.calls.Add(1)
	d.entered <- struct{}{}
	select {
	case <-d.release:
		return model.File{}, nil
	case <-ctx.Done():
		return model.File{}, ctx.Err()
	}
}

func TestGateSerializesAndCancelsWaiter(t *testing.T) {
	gate := NewGate()
	first := &blockingDownloader{entered: make(chan struct{}, 1), release: make(chan struct{})}
	second := &blockingDownloader{entered: make(chan struct{}, 1), release: make(chan struct{})}
	firstDone := make(chan error, 1)
	go func() {
		_, err := gate.Wrap(first).Download(context.Background(), "", "")
		firstDone <- err
	}()
	select {
	case <-first.entered:
	case <-time.After(time.Second):
		t.Fatal("first download did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := gate.Wrap(second).Download(ctx, "", "")
		secondDone <- err
	}()
	cancel()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second download error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled waiter remained blocked")
	}
	if second.calls.Load() != 0 {
		t.Fatal("canceled waiter started a download")
	}
	close(first.release)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first download did not finish")
	}
}
