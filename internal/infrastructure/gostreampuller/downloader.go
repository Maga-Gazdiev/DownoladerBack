// Package gostreampuller provides the original yt-dlp invocation and the optional
// upstream library backend behind the same Downloader port.
package gostreampuller

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	stream "github.com/debargha2001/gostreampuller/downloader"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
	"video-downloader/internal/service/video"
	"video-downloader/internal/storage/media"
)

type Downloader struct {
	runner                 Runner
	binary, cookiesBrowser string
	cookiesFile            string
	native                 bool
}

func NewWithCookies(r Runner, binary, browser, cookiesFile string) *Downloader {
	d := New(r, binary, browser)
	d.cookiesFile = cookiesFile
	return d
}

func New(r Runner, binary, cookiesBrowser string) *Downloader {
	return &Downloader{runner: r, binary: binary, cookiesBrowser: cookiesBrowser}
}

// NewNative runs the private helper because upstream v1.1.0 cannot accept a caller context.
func NewNative(r Runner, executable string) *Downloader {
	return &Downloader{runner: r, binary: executable, native: true}
}
func (d *Downloader) Download(ctx context.Context, rawURL, dir string) (model.File, error) {
	return media.Save(ctx, dir, func(stage string) error {
		if d.native {
			return d.runner.Run(ctx, d.binary, "__gostreampuller", rawURL, stage)
		}
		args := []string{"--ignore-config", "--no-playlist", "-N", "16", "-o", filepath.Join(stage, "video.%(ext)s")}
		platform, err := video.Detect(rawURL)
		if err != nil {
			return apperrors.Permanent(err)
		}
		if platform == model.YouTube {
			// Current YouTube players require JavaScript challenge evaluation.
			// Anonymous downloads are attempted first; cookies are an optional fallback.
			args = append(args, "--js-runtimes", "node")
			if d.cookiesFile != "" {
				// yt-dlp writes its cookie jar on exit; keep the mounted original read-only.
				f, err := os.Open(d.cookiesFile)
				if err != nil {
					return fmt.Errorf("open YouTube cookies file: %w", err)
				}
				body, err := io.ReadAll(io.LimitReader(f, 5<<20))
				f.Close()
				if err != nil {
					return err
				}
				cookiePath := filepath.Join(stage, "cookies.txt")
				if err := os.WriteFile(cookiePath, body, 0600); err != nil {
					return err
				}
				args = append(args, "--cookies", cookiePath)
			} else if d.cookiesBrowser != "" {
				args = append(args, "--cookies-from-browser", d.cookiesBrowser)
			}
		}
		args = append(args, "--", rawURL)
		return d.runner.Run(ctx, d.binary, args...)
	})
}

// RunNative is called only in the helper process, never in the webhook.
func RunNative(rawURL, dir, format, resolution, codec string) error {
	_, err := stream.DownloadVideoToDir(rawURL, format, resolution, codec, dir)
	return err
}

type Runner interface {
	Run(context.Context, string, ...string) error
}
