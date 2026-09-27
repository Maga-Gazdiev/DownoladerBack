package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"video-downloader/internal/config"
	"video-downloader/internal/infrastructure/command"
	"video-downloader/internal/infrastructure/potprovider"
	"video-downloader/internal/infrastructure/ytdlp"
)

func Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: downloader [serve|URL]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "serve" {
		return Run(ctx, cfg)
	}
	if !strings.HasPrefix(args[0], "https://") && !strings.HasPrefix(args[0], "http://") {
		return fmt.Errorf("unknown mode %q; use serve or a video URL", args[0])
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.JobTimeout)
	defer cancel()
	provider, err := potprovider.Start(ctx, command.Exec{}, cfg.POTProviderHome)
	if err != nil {
		return err
	}
	defer provider.Close()
	ctx = provider.Context()
	source := ytdlp.New(command.Exec{}, cfg.YTDLP, provider.URL, cfg.MaxUploadBytes, cfg.MemoryBytes)
	media, err := source.Download(ctx, args[0])
	if err != nil {
		return err
	}
	defer media.Close()
	r, err := media.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.Copy(os.Stdout, r)
	return err
}
