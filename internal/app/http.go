package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

func serve(ctx context.Context, addr string, webhook http.Handler, webAPI http.Handler) error {
	mux := routes(webhook, webAPI)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	slog.Info("HTTP listening", "address", addr)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	err := <-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
