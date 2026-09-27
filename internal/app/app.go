package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"video-downloader/internal/config"
	"video-downloader/internal/handler/api"
	telegramhandler "video-downloader/internal/handler/telegram"
	"video-downloader/internal/infrastructure/command"
	"video-downloader/internal/infrastructure/telegram"
	"video-downloader/internal/infrastructure/ytdlp"
	telegramservice "video-downloader/internal/service/telegram"
	"video-downloader/internal/service/web"
)

func Run(ctx context.Context, cfg config.Config) error {
	if cfg.Secret == "" || cfg.WebToken == "" || cfg.Token == "" {
		return errors.New("TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_SECRET and WEB_API_TOKEN are required")
	}
	source := ytdlp.New(command.Exec{}, cfg.YTDLP, cfg.CookiesBrowser, cfg.CookiesFile, cfg.MaxUploadBytes, cfg.MemoryBytes)
	client := telegram.New(cfg.Token, cfg.TelegramURL, &http.Client{Timeout: cfg.SendTimeout}, cfg.MaxUploadBytes)
	tasks := newTasks(ctx, cfg.Concurrency)
	webService := web.New(tasks, source, cfg.JobTimeout, cfg.WebTTL, cfg.MaxJobs)
	telegramService := telegramservice.New(tasks, source, client, cfg.JobTimeout, cfg.SendTimeout)
	defer webService.Close()
	defer tasks.Close()
	webhook := telegramhandler.NewWebhook(telegramService, cfg.Secret)

	slog.Info("application started", "address", cfg.HTTPAddr, "concurrency", cfg.Concurrency, "memory_bytes", cfg.MemoryBytes)
	return serve(ctx, cfg.HTTPAddr, webhook, api.NewWebAPI(webService, cfg.WebToken))
}
