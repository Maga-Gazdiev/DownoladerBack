package download

import (
	"context"
	"errors"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
	"video-downloader/internal/service/video"
)

type Downloader interface {
	Download(ctx context.Context, rawURL, outputDir string) (model.File, error)
}

type Router struct {
	downloaders map[model.Platform]Downloader
}

func NewRouter(downloaders map[model.Platform]Downloader) *Router {
	copy := make(map[model.Platform]Downloader, len(downloaders))
	for p, d := range downloaders {
		copy[p] = d
	}
	return &Router{downloaders: copy}
}
func (r *Router) Download(ctx context.Context, rawURL, dir string) (model.File, error) {
	platform, err := video.Detect(rawURL)
	if err != nil {
		return model.File{}, apperrors.Permanent(err)
	}
	d := r.downloaders[platform]
	if d == nil {
		return model.File{}, apperrors.Permanent(errors.New("no downloader for platform"))
	}
	return d.Download(ctx, rawURL, dir)
}
