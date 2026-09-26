package ffmpeg

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	apperrors "video-downloader/internal/errors"
)

// MP4 converts media using FFmpeg and enforces the configured output size.
type MP4 struct {
	runner   Runner
	maxBytes int64
}

func NewMP4(r Runner, maxBytes int64) *MP4 {
	return &MP4{runner: r, maxBytes: maxBytes}
}

func (d *MP4) Convert(ctx context.Context, input, output string) error {
	// Re-encode even MP4 inputs: their codecs may be VP9/AV1/Opus.
	err := d.runner.Run(ctx, "ffmpeg", "-nostdin", "-y", "-i", input,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "fast", "-crf", "23",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
		"-movflags", "+faststart", output)
	if err != nil {
		return fmt.Errorf("prepare Telegram MP4: %w", err)
	}
	return d.fit(ctx, input, output, filepath.Dir(output))
}

func (d *MP4) fit(ctx context.Context, input, output, stage string) error {
	info, err := os.Stat(output)
	if err != nil {
		return err
	}
	if info.Size() <= d.maxBytes {
		return nil
	}
	duration, err := d.duration(ctx, output)
	if err != nil {
		return err
	}
	// Reserve 10% for container overhead and rate-control variation.
	bitrate := int64(float64(d.maxBytes)*8*0.90/duration) - 96000
	if bitrate < 64000 {
		return apperrors.Permanent(fmt.Errorf("video too long for upload limit %d", d.maxBytes))
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := d.encodeTwoPass(ctx, input, output, stage, bitrate); err != nil {
			return err
		}
		info, err = os.Stat(output)
		if err != nil {
			return err
		}
		if info.Size() > 0 && info.Size() <= d.maxBytes {
			return nil
		}
		bitrate = bitrate * 3 / 4
	}
	return apperrors.Permanent(fmt.Errorf("compressed video still exceeds upload limit %d", d.maxBytes))
}

func (d *MP4) duration(ctx context.Context, path string) (float64, error) {
	body, err := d.runner.Output(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	if err != nil {
		return 0, fmt.Errorf("probe video duration: %w", err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(body)), 64)
	if err != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0, fmt.Errorf("invalid video duration")
	}
	return duration, nil
}

func (d *MP4) encodeTwoPass(ctx context.Context, input, output, stage string, bitrate int64) error {
	base := []string{"-nostdin", "-y", "-i", input, "-map", "0:v:0", "-c:v", "libx264", "-preset", "fast",
		"-b:v", strconv.FormatInt(bitrate, 10), "-vf", "scale=w='min(1280,iw)':h='min(720,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2", "-pix_fmt", "yuv420p",
		"-passlogfile", filepath.Join(stage, "encode-pass")}
	first := append(append([]string{}, base...), "-pass", "1", "-an", "-f", "null", os.DevNull)
	if err := d.runner.Run(ctx, "ffmpeg", first...); err != nil {
		return fmt.Errorf("MP4 size pass 1: %w", err)
	}
	second := append(append([]string{}, base...), "-pass", "2", "-map", "0:a:0?", "-c:a", "aac", "-b:a", "96k", "-ac", "2", "-movflags", "+faststart", output)
	if err := d.runner.Run(ctx, "ffmpeg", second...); err != nil {
		return fmt.Errorf("MP4 size pass 2: %w", err)
	}
	return nil
}

type Runner interface {
	Run(context.Context, string, ...string) error
	Output(context.Context, string, ...string) ([]byte, error)
}
