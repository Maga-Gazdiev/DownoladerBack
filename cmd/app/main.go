package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"video-downloader/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Execute(ctx, os.Args[1:]); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
