package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"video-downloader/internal/config"
	"video-downloader/internal/infrastructure/gostreampuller"
	"video-downloader/internal/service/video"
)

func Execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		args = []string{"serve"}
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if args[0] == "__gostreampuller" {
		if len(args) != 3 {
			return errors.New("invalid helper arguments")
		}
		if _, err := video.Detect(args[1]); err != nil {
			return err
		}
		// Avoid upstream auto-installation during worker execution.
		if err := os.Setenv("GOSTREAMPULLER_NO_AUTO_INSTALL", "1"); err != nil {
			return err
		}
		return gostreampuller.RunNative(args[1], args[2], cfg.VideoFormat, cfg.VideoResolution, cfg.VideoCodec)
	}
	if strings.HasPrefix(args[0], "http://") || strings.HasPrefix(args[0], "https://") {
		if len(args) > 2 {
			return errors.New("usage: downloader <URL> [output-dir]")
		}
		d, err := downloader(cfg)
		if err != nil {
			return err
		}
		dir := cfg.DownloadDir
		if len(args) == 2 {
			dir = args[1]
		}
		jobCtx, cancel := context.WithTimeout(ctx, cfg.JobTimeout)
		defer cancel()
		file, err := d.Download(jobCtx, args[0], dir)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(file)
	}
	if len(args) != 1 {
		return errors.New("service mode takes no additional arguments")
	}

	if args[0] != "serve" {
		return fmt.Errorf("unknown mode %q; use serve or a video URL", args[0])
	}
	return Run(ctx, cfg)
}
