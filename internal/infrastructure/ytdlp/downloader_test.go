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

type codedExtractorError string

func (e codedExtractorError) Error() string       { return string(e) }
func (e codedExtractorError) FailureCode() string { return string(e) }

func TestInstagramEmptyResponseRetriesWithIOS(t *testing.T) {
	calls := 0
	d := New(runnerFunc(func(_ context.Context, w io.Writer, _ string, args ...string) error {
		calls++
		ios := strings.Contains(strings.Join(args, " "), "instagram:app_id=ios")
		if ios != (calls == 2) {
			t.Errorf("unexpected extractor mode on attempt %d: %v", calls, args)
		}
		if _, err := io.WriteString(w, "video"); err != nil {
			return err
		}
		if calls == 1 {
			return codedExtractorError("instagram_empty_response")
		}
		return nil
	}), "yt-dlp", "", 5, 5)
	media, err := d.Download(context.Background(), "https://www.instagram.com/reel/test/")
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	if calls != 2 || media.Size() != 5 {
		t.Fatalf("retry did not return the expected video: calls=%d size=%d", calls, media.Size())
	}
}

func TestInstagramAuthenticationDoesNotRetry(t *testing.T) {
	calls := 0
	d := New(runnerFunc(func(context.Context, io.Writer, string, ...string) error {
		calls++
		return apperrors.ErrAuthentication
	}), "yt-dlp", "", 5, 5)
	if _, err := d.Download(context.Background(), "https://www.instagram.com/reel/test/"); !errors.Is(err, apperrors.ErrAuthentication) {
		t.Fatalf("authentication failure = %v", err)
	}
	if calls != 1 {
		t.Fatalf("authentication error retried %d times", calls)
	}
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

func TestMemoryReservationGrowsWithDownloadedMedia(t *testing.T) {
	d := New(runnerFunc(func(_ context.Context, w io.Writer, _ string, _ ...string) error {
		_, err := w.Write([]byte("video"))
		return err
	}), "yt-dlp", "", 100, 20)
	media := make([]interface{ Close() }, 0, 4)
	for range 4 {
		m, err := d.Download(context.Background(), "https://youtu.be/test")
		if err != nil {
			t.Fatalf("small video did not fit in memory budget: %v", err)
		}
		media = append(media, m)
	}
	if _, err := d.Download(context.Background(), "https://youtu.be/test"); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("expected memory limit after 20 reserved bytes, got %v", err)
	}
	media[0].Close()
	m, err := d.Download(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatalf("reservation was not released with media: %v", err)
	}
	m.Close()
	for _, m := range media[1:] {
		m.Close()
	}
}

func TestMemoryReservationFailureReleasesPartialBuffer(t *testing.T) {
	calls := 0
	d := New(runnerFunc(func(ctx context.Context, w io.Writer, _ string, _ ...string) error {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte("12345678"))
			_, err := w.Write([]byte("more"))
			if !errors.Is(err, apperrors.ErrBusy) || ctx.Err() == nil {
				t.Errorf("expected memory exhaustion to cancel download: %v %v", err, ctx.Err())
			}
			return ctx.Err()
		}
		_, err := w.Write([]byte("ok"))
		return err
	}), "yt-dlp", "", 16, 8)
	if _, err := d.Download(context.Background(), "https://youtu.be/test"); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("expected memory exhaustion, got %v", err)
	}
	m, err := d.Download(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatalf("partial reservation leaked: %v", err)
	}
	m.Close()
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
