package ytdlp

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	apperrors "video-downloader/internal/errors"
)

type runnerFunc func(context.Context, io.Writer, string, ...string) error

func (f runnerFunc) Stream(ctx context.Context, w io.Writer, b string, args ...string) error {
	return f(ctx, w, b, args...)
}
func TestMemoryRemainsReservedForOpenReaders(t *testing.T) {
	d := New(runnerFunc(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		_, err := w.Write([]byte("video"))
		return err
	}), "yt-dlp", "", 8, 8)
	media, err := d.Download(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := media.Open()
	if err != nil {
		t.Fatal(err)
	}
	media.Close()
	media.Close()
	if _, err := d.Download(context.Background(), "https://youtu.be/test"); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("released while reader open: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "video" {
		t.Fatalf("reader: %q %v", data, err)
	}
	reader.Close()
	reader.Close()
	media, err = d.Download(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	media.Close()
}
func TestOverflowCancelsProcessAndReleasesReservation(t *testing.T) {
	calls := 0
	d := New(runnerFunc(func(ctx context.Context, w io.Writer, _ string, _ ...string) error {
		calls++
		if calls == 1 {
			_, err := w.Write([]byte("oversized"))
			if !errors.Is(err, ErrTooLarge) || ctx.Err() == nil {
				t.Errorf("overflow did not cancel: %v %v", err, ctx.Err())
			}
			return ctx.Err()
		}
		_, err := w.Write([]byte("ok"))
		return err
	}), "yt-dlp", "", 4, 4)
	if _, err := d.Download(context.Background(), "https://youtu.be/test"); !errors.Is(err, ErrTooLarge) {
		t.Fatal(err)
	}
	media, err := d.Download(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	media.Close()
}
func TestFailureReleasesReservation(t *testing.T) {
	for _, downloadErr := range []error{errors.New("failed"), context.DeadlineExceeded, nil} {
		d := New(runnerFunc(func(context.Context, io.Writer, string, ...string) error { return downloadErr }), "yt-dlp", "", 4, 4)
		for range 2 {
			if _, err := d.Download(context.Background(), "https://youtu.be/test"); err == nil || errors.Is(err, apperrors.ErrBusy) {
				t.Fatalf("empty/failed download: %v", err)
			}
		}
	}
}

func TestYouTubeUsesAnonymousProvider(t *testing.T) {
	for _, raw := range []string{"https://youtu.be/test", "https://www.tiktok.com/@test/video/123"} {
		t.Run(raw, func(t *testing.T) {
			d := New(runnerFunc(func(_ context.Context, w io.Writer, _ string, args ...string) error {
				joined := strings.Join(args, " ")
				youtube := strings.Contains(raw, "youtu.be")
				for _, arg := range []string{"youtube:player_client=mweb", "youtubepot-bgutilhttp:base_url=http://127.0.0.1:12345", "--no-cookies", "--no-cookies-from-browser"} {
					if strings.Contains(joined, arg) != youtube {
						t.Errorf("unexpected platform options: %s", joined)
					}
				}
				for _, arg := range args {
					if arg == "--cookies" || arg == "--cookies-from-browser" {
						t.Fatal("account cookies enabled")
					}
				}
				_, err := io.WriteString(w, "video")
				return err
			}), "yt-dlp", "http://127.0.0.1:12345", 10, 10)
			media, err := d.Download(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			media.Close()
		})
	}
}
