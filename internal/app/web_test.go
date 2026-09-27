package app

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
	"video-downloader/internal/service/web"
)

type downloadFunc func(context.Context, string) (*model.Media, error)

func (f downloadFunc) Download(ctx context.Context, raw string) (*model.Media, error) {
	return f(ctx, raw)
}
func awaitJob(t *testing.T, s *web.Service, id, status string) model.WebJob {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		job, err := s.Get(context.Background(), id)
		if err == nil && job.Status == status {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job did not reach %s", status)
	return model.WebJob{}
}
func TestExpiryKeepsOpenReaderAlive(t *testing.T) {
	released := make(chan struct{})
	tasks := newTasks(context.Background(), 2)
	defer tasks.Close()
	s := web.New(tasks, downloadFunc(func(context.Context, string) (*model.Media, error) {
		return model.NewMedia(model.File{Name: "video.mp4"}, []byte("video"), func() { close(released) }), nil
	}), time.Second, 40*time.Millisecond, 10)
	defer s.Close()
	job, err := s.Create(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	awaitJob(t, s, job.ID, "ready")
	reader, _, err := s.Open(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	deadline := time.Now().Add(time.Second)
	for {
		_, err = s.Get(context.Background(), job.ID)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("TTL did not remove task")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-released:
		t.Fatal("buffer released with active reader")
	default:
	}
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "video" {
		t.Fatalf("expired reader: %q %v", data, err)
	}
	reader.Close()
	select {
	case <-released:
	default:
		t.Fatal("buffer not released")
	}
}
func TestTimeoutIsVisible(t *testing.T) {
	tasks := newTasks(context.Background(), 2)
	defer tasks.Close()
	s := web.New(tasks, downloadFunc(func(ctx context.Context, _ string) (*model.Media, error) { <-ctx.Done(); return nil, ctx.Err() }), 10*time.Millisecond, time.Minute, 1)
	defer s.Close()
	job, err := s.Create(context.Background(), "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	job = awaitJob(t, s, job.ID, "failed")
	if job.Error != "Превышено время скачивания. Попробуйте более короткое видео." {
		t.Fatal(job.Error)
	}
	if _, err = s.Create(context.Background(), "https://youtu.be/test"); !errors.Is(err, apperrors.ErrBusy) {
		t.Fatalf("job cap: %v", err)
	}
}
