package ytdlp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"

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
	media, err := d.downloadOnce(ctx, raw, platform, false)
	if platform != model.Instagram || !instagramEmptyResponse(err) {
		return media, err
	}
	// A fresh yt-dlp process can recover when Instagram returns incomplete
	// logged-out metadata. Try its supported iOS app ID once before giving up.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
	}
	return d.downloadOnce(ctx, raw, platform, true)
}

func instagramEmptyResponse(err error) bool {
	var coded interface{ FailureCode() string }
	return errors.As(err, &coded) && coded.FailureCode() == "instagram_empty_response"
}

func (d *Downloader) downloadOnce(ctx context.Context, raw string, platform model.Platform, instagramIOS bool) (*model.Media, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reserve := func(n int64) bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		if n < 0 || d.memoryBytes-d.used < n {
			return false
		}
		d.used += n
		return true
	}
	releaseBytes := func(n int64) {
		d.mu.Lock()
		d.used -= n
		d.mu.Unlock()
	}
	success := false
	var output *buffer
	defer func() {
		if !success && output != nil && output.reserved > 0 {
			releaseBytes(output.reserved)
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	output = &buffer{limit: d.maxBytes, cancel: cancel, reserve: reserve, release: releaseBytes}
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

	if instagramIOS {
		args = append(args, "--extractor-args", "instagram:app_id=ios")
	}
	args = append(args, "--", raw)
	err := d.runner.Stream(ctx, output, d.binary, args...)
	if output.memoryFull {
		return nil, apperrors.ErrBusy
	}
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
	var once sync.Once
	release := func() { once.Do(func() { releaseBytes(output.reserved) }) }
	return model.NewMedia(model.File{Hash: id, Name: id + ".mp4"}, output.data, release), nil
}

// Grow the output buffer with the media, reserving only the capacity it uses.
type buffer struct {
	data       []byte
	limit      int64
	cancel     context.CancelFunc
	overflow   bool
	memoryFull bool
	reserved   int64
	reserve    func(int64) bool
	release    func(int64)
}

func (b *buffer) Write(p []byte) (int, error) {
	if int64(len(p)) > b.limit-int64(len(b.data)) {
		b.overflow = true
		b.cancel()
		return 0, ErrTooLarge
	}
	needed := len(b.data) + len(p)
	if needed > cap(b.data) {
		newCap := cap(b.data) * 2
		if newCap < needed {
			newCap = needed
		}
		if int64(newCap) > b.limit {
			newCap = int(b.limit)
		}
		delta := int64(newCap - cap(b.data))
		if !b.reserve(delta) {
			b.memoryFull = true
			b.cancel()
			return 0, apperrors.ErrBusy
		}
		grown := make([]byte, len(b.data), newCap)
		copy(grown, b.data)
		b.data = grown
		b.reserved += delta
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
