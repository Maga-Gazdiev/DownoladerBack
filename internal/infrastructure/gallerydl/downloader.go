package gallerydl

import (
	"context"

	"video-downloader/internal/service/model"
	"video-downloader/internal/storage/media"
)

type Downloader struct {
	runner Runner
	binary string
}

func New(r Runner, binary string) *Downloader { return &Downloader{runner: r, binary: binary} }
func (d *Downloader) Download(ctx context.Context, rawURL, dir string) (model.File, error) {
	return media.Save(ctx, dir, func(stage string) error {
		return d.runner.Run(ctx, d.binary, "--ignore-config", "-D", stage, "--", rawURL)
	})
}

type Runner interface {
	Run(context.Context, string, ...string) error
}
