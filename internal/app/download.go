package app

import (
	"os"

	"video-downloader/internal/config"
	"video-downloader/internal/infrastructure/command"
	"video-downloader/internal/infrastructure/ffmpeg"
	"video-downloader/internal/infrastructure/gallerydl"
	"video-downloader/internal/infrastructure/gostreampuller"
	"video-downloader/internal/service/download"
	"video-downloader/internal/service/model"
)

func downloader(cfg config.Config) (download.Downloader, error) {
	d, err := sourceDownloader(cfg)
	if err != nil {
		return nil, err
	}
	return download.NewPrepared(d, ffmpeg.NewMP4(command.Exec{}, cfg.MaxUploadBytes)), nil
}

func sourceDownloader(cfg config.Config) (download.Downloader, error) {
	runner := command.Exec{}
	var video download.Downloader = gostreampuller.NewWithCookies(runner, cfg.YTDLP, cfg.CookiesBrowser, cfg.CookiesFile)
	if cfg.Backend == "gostreampuller" {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		video = gostreampuller.NewNative(runner, executable)
	}
	return download.NewRouter(map[model.Platform]download.Downloader{
		model.YouTube: video, model.Instagram: video, model.TikTok: gallerydl.New(runner, cfg.GalleryDL),
	}), nil
}
