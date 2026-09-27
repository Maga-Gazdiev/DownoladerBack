package potprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type runnerFunc func(context.Context, io.Writer, string, ...string) error

func (f runnerFunc) Stream(ctx context.Context, w io.Writer, b string, args ...string) error {
	return f(ctx, w, b, args...)
}
func TestLifecycle(t *testing.T) {
	exited := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ io.Writer, b string, args ...string) error {
		defer close(exited)
		if b != "node" || args[1] != "--host" || args[2] != "127.0.0.1" {
			t.Errorf("unexpected invocation: %s %v", b, args)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:"+args[4])
		if err != nil {
			return err
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"version": Version})
		})}
		done := make(chan struct{})
		go func() { defer close(done); server.Serve(listener) }()
		<-ctx.Done()
		server.Close()
		<-done
		return ctx.Err()
	})
	p, err := Start(context.Background(), runner, "/provider")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.URL, "http://127.0.0.1:") {
		t.Fatal(p.URL)
	}
	p.Close()
	p.Close()
	if p.Context().Err() == nil || p.Err() != nil {
		t.Fatal("unexpected shutdown state")
	}
	select {
	case <-exited:
	default:
		t.Fatal("child still running")
	}
}
func TestStartupFailure(t *testing.T) {
	failure := errors.New("node missing")
	_, err := Start(context.Background(), runnerFunc(func(context.Context, io.Writer, string, ...string) error { return failure }), "/provider")
	if !errors.Is(err, failure) {
		t.Fatalf("lost startup error: %v", err)
	}
}
func TestCancellationDuringStartup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := Start(ctx, runnerFunc(func(ctx context.Context, _ io.Writer, _ string, _ ...string) error { <-ctx.Done(); return ctx.Err() }), "/provider")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestUnexpectedExitCancelsApplicationContext(t *testing.T) {
	crash := make(chan struct{})
	failure := errors.New("provider crashed")
	runner := runnerFunc(func(ctx context.Context, _ io.Writer, _ string, args ...string) error {
		listener, err := net.Listen("tcp", "127.0.0.1:"+args[4])
		if err != nil {
			return err
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"version": Version})
		})}
		done := make(chan struct{})
		go func() { defer close(done); server.Serve(listener) }()
		defer func() { server.Close(); <-done }()
		select {
		case <-crash:
			return failure
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	p, err := Start(context.Background(), runner, "/provider")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	close(crash)
	select {
	case <-p.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("application context still active")
	}
	p.Close()
	if !errors.Is(p.Err(), failure) {
		t.Fatalf("lost provider error: %v", p.Err())
	}
}
