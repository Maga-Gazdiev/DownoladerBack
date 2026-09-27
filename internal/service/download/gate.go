package download

import (
	"context"

	"video-downloader/internal/service/model"
)

// Gate limits simultaneous downloads and conversions in a memory-constrained process.
type Gate struct {
	slot chan struct{}
}

func NewGate() *Gate { return &Gate{slot: make(chan struct{}, 1)} }

func (g *Gate) Wrap(next Downloader) Downloader {
	return &limited{gate: g, next: next}
}

type limited struct {
	gate *Gate
	next Downloader
}

func (d *limited) Download(ctx context.Context, rawURL, dir string) (model.File, error) {
	select {
	case d.gate.slot <- struct{}{}:
	case <-ctx.Done():
		return model.File{}, ctx.Err()
	}
	defer func() { <-d.gate.slot }()
	return d.next.Download(ctx, rawURL, dir)
}
