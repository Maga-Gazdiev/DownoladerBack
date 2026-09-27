package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"video-downloader/internal/config"
	"video-downloader/internal/handler/api"
	"video-downloader/internal/handler/queue"
	telegramhandler "video-downloader/internal/handler/telegram"
	"video-downloader/internal/infrastructure/backblaze"
	"video-downloader/internal/infrastructure/command"
	"video-downloader/internal/infrastructure/ffmpeg"
	"video-downloader/internal/infrastructure/rabbitmq"
	"video-downloader/internal/infrastructure/telegram"
	"video-downloader/internal/service/download"
	"video-downloader/internal/service/model"
	telegramservice "video-downloader/internal/service/telegram"
	"video-downloader/internal/service/web"
	"video-downloader/internal/storage/files"
	"video-downloader/internal/worker"
)

func Run(ctx context.Context, cfg config.Config) error {
	if cfg.RabbitURL == "" || cfg.Secret == "" || cfg.WebToken == "" || cfg.Token == "" {
		return errors.New("RABBIT_AMQP_URL, TELEGRAM_BOT_TOKEN, TELEGRAM_WEBHOOK_SECRET and WEB_API_TOKEN are required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	broker, err := rabbitmq.New(ctx, cfg.RabbitURL, cfg.QueuePrefix, cfg.MaxRetries)
	if err != nil {
		return err
	}
	defer broker.Close()
	store, err := files.New(cfg.DownloadDir)
	if err != nil {
		return err
	}
	var webStore interface {
		web.WebJobs
		worker.Cleaner
	}
	if cfg.B2Endpoint != "" {
		remote, err := backblaze.NewWeb(cfg.DownloadDir, cfg.B2Endpoint, cfg.B2Bucket, cfg.B2KeyID, cfg.B2ApplicationKey)
		if err != nil {
			return err
		}
		if err := remote.Check(ctx); err != nil {
			return err
		}
		webStore = remote
	} else {
		local, err := files.NewWeb(cfg.DownloadDir)
		if err != nil {
			return err
		}
		webStore = local
	}
	source, err := sourceDownloader(cfg)
	if err != nil {
		return err
	}
	prepared := download.NewPrepared(source, ffmpeg.NewMP4(command.Exec{}, cfg.MaxUploadBytes))
	downloadService := telegramservice.NewDownload(prepared, broker, store)
	client := telegram.New(cfg.Token, cfg.TelegramURL, &http.Client{Timeout: cfg.SendTimeout}, cfg.MaxUploadBytes)
	sendService := telegramservice.NewSend(client, store)
	webService := web.NewWeb(broker, webStore, source, cfg.WebTTL)
	webhook := telegramhandler.NewWebhook(telegramservice.NewWebhook(broker), cfg.Secret)
	webAPI := api.NewWebAPI(webService, cfg.WebToken)

	// Every component reports completion; wait for all of them before closing the broker.
	components := []func() error{
		func() error { return serve(ctx, cfg.HTTPAddr, webhook, broker.Done(), webAPI) },
		func() error {
			return broker.Consume(ctx, model.DownloadQueue, cfg.JobTimeout, queue.JSONJob(downloadService.Process))
		},
		func() error {
			return broker.Consume(ctx, model.SendQueue, cfg.SendTimeout, queue.JSONJob(sendService.Process))
		},
		func() error {
			return broker.Consume(ctx, model.WebQueue, cfg.JobTimeout, queue.JSONJob(webService.Process))
		},
		func() error { return worker.NewCleanup(webStore, cfg.CleanupInterval).Run(ctx) },
	}
	done := make(chan error, len(components))
	for _, run := range components {
		go func() { done <- run() }()
	}
	slog.Info("application started", "address", cfg.HTTPAddr, "consumers", 3)
	var result error
	for range components {
		err := <-done
		if err != nil && !errors.Is(err, context.Canceled) {
			result = errors.Join(result, err)
		}
		cancel()
	}
	return result
}
