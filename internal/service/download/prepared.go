package download

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"video-downloader/internal/service/model"
	"video-downloader/internal/storage/media"
)

type Converter interface {
	Convert(context.Context, string, string) error
}

type Prepared struct {
	source    Downloader
	converter Converter
}

func NewPrepared(source Downloader, converter Converter) *Prepared {
	return &Prepared{source: source, converter: converter}
}

// Download hashes the final converted file, not the original source video.
func (d *Prepared) Download(ctx context.Context, url, dir string) (model.File, error) {
	return media.Save(ctx, dir, func(stage string) error {
		file, err := d.source.Download(ctx, url, stage)
		if err != nil {
			return err
		}
		slog.Info("telegram source downloaded", "file_hash", file.Hash)
		input := filepath.Join(stage, file.Name)
		if err := d.converter.Convert(ctx, input, filepath.Join(stage, "telegram.mp4")); err != nil {
			return err
		}
		slog.Info("telegram video converted", "file_hash", file.Hash)
		return os.Remove(input)
	})
}
