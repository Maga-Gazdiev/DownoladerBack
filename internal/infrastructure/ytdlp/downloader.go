package ytdlp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
)

var ErrTooLarge = errors.New("video exceeds memory buffer limit")

type Runner interface {
	Stream(context.Context, io.Writer, string, ...string) error
}
type Downloader struct {
	runner                      Runner
	binary, providerURL         string
	maxBytes, memoryBytes, used int64
	mu                          sync.Mutex
}

func New(r Runner, binary, providerURL string, maxBytes, memoryBytes int64) *Downloader {
	return &Downloader{runner: r, binary: binary, providerURL: providerURL, maxBytes: maxBytes, memoryBytes: memoryBytes}
}
func (d *Downloader) Download(ctx context.Context, raw string) (*model.Media, error) {
	platform, err := model.DetectPlatform(raw)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	if d.memoryBytes-d.used < d.maxBytes {
		d.mu.Unlock()
		return nil, apperrors.ErrBusy
	}
	d.used += d.maxBytes
	d.mu.Unlock()
	release := func() { d.mu.Lock(); d.used -= d.maxBytes; d.mu.Unlock() }
	success := false
	defer func() {
		if !success {
			release()
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &buffer{limit: d.maxBytes, cancel: cancel}
	// FFmpeg reads media URLs and writes fragmented MP4 to stdout. Restrict
	// protocols to those it can stream directly, without native fragment files.
	protocol := "[protocol~='^(https?|m3u8(_native)?)$']"
	format := "bestvideo[ext=mp4][vcodec^=avc1]" + protocol + "+bestaudio[ext=m4a]" + protocol + "/best[ext=mp4]" + protocol
	args := []string{"--ignore-config", "--no-playlist", "--playlist-items", "1", "--no-cache-dir", "--no-part", "--no-progress", "--no-simulate", "--downloader", "ffmpeg", "--downloader-args", "ffmpeg_o:-f mp4 -movflags +frag_keyframe+empty_moov+default_base_moof", "-f", format, "-o", "-"}
	if platform == model.YouTube {
		args = append(args, "--js-runtimes", "node", "--no-cookies", "--no-cookies-from-browser",
			"--extractor-args", "youtube:player_client=mweb")
		if d.providerURL != "" {
			args = append(args, "--extractor-args", "youtubepot-bgutilhttp:base_url="+d.providerURL)
		}
	}

	args = append(args, "--", raw)
	err = d.runner.Stream(ctx, output, d.binary, args...)
	if output.overflow {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(output.data) == 0 {
		return nil, errors.New("empty video")
	}
	hash := sha256.Sum256(output.data)
	id := hex.EncodeToString(hash[:])
	success = true
	return model.NewMedia(model.File{Hash: id, Name: id + ".mp4"}, output.data, release), nil
}

// Allocate at most one reserved buffer, without geometric growth or copies.
type buffer struct {
	data     []byte
	limit    int64
	cancel   context.CancelFunc
	overflow bool
}

func (b *buffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(len(b.data)) {
		b.overflow = true
		b.cancel()
		return 0, ErrTooLarge
	}
	if b.data == nil {
		b.data = make([]byte, 0, int(b.limit))
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
