package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
	"video-downloader/internal/service/video"
)

var ErrWebNotReady = errors.New("file is not ready")
var ErrWebExpired = errors.New("download expired")

type Web struct {
	publisher  Publisher
	jobs       WebJobs
	downloader Downloader
	ttl        time.Duration
}

func NewWeb(p Publisher, j WebJobs, d Downloader, ttl time.Duration) *Web {
	return &Web{publisher: p, jobs: j, downloader: d, ttl: ttl}
}
func (s *Web) Create(ctx context.Context, raw string) (model.WebJob, error) {
	platform, err := video.Detect(raw)
	if err != nil {
		return model.WebJob{}, err
	}
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return model.WebJob{}, err
	}
	job := model.WebJob{ID: hex.EncodeToString(id[:]), URL: raw, Platform: platform, Status: "queued", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(s.ttl)}
	if err := s.jobs.Save(job); err != nil {
		return job, err
	}
	// Keep queued state on ambiguous publish failure: a confirmed job may arrive later.
	if err := publish(ctx, s.publisher, model.WebQueue, struct {
		ID string `json:"id"`
	}{job.ID}); err != nil {
		return job, err
	}
	return job, nil
}
func (s *Web) Get(id string) (model.WebJob, error) {
	job, err := s.jobs.Get(id)
	if err != nil {
		return job, err
	}
	if time.Now().After(job.ExpiresAt) {
		return job, ErrWebExpired
	}
	return job, nil
}
func (s *Web) Open(id string) (io.ReadSeekCloser, model.WebJob, error) {
	job, err := s.Get(id)
	if err != nil {
		return nil, job, err
	}
	if job.Status != "ready" {
		return nil, job, ErrWebNotReady
	}
	f, _, err := s.jobs.Open(id, job.File)
	return f, job, err
}
func (s *Web) Process(ctx context.Context, message struct {
	ID string `json:"id"`
}) error {
	release, err := s.jobs.Acquire(message.ID)
	if err != nil {
		return err
	}
	defer release()
	job, err := s.jobs.Get(message.ID)
	if errors.Is(err, os.ErrNotExist) {
		return apperrors.Permanent(err)
	}
	if err != nil {
		return err
	}
	if time.Now().After(job.ExpiresAt) || job.Status == "ready" || job.Status == "failed" {
		return nil
	}
	dir, err := s.jobs.Directory(job.ID)
	if err != nil {
		return err
	}
	job.Status = "downloading"
	if err := s.jobs.Save(job); err != nil {
		return err
	}
	file, downloadErr := s.downloader.Download(ctx, job.URL, dir)
	if downloadErr != nil {
		slog.Warn("web download failed", "job_id", job.ID, "platform", job.Platform, "reason", downloadFailureCode(downloadErr))
		job.Status = "failed"
		job.Error = "Не удалось скачать видео. Проверьте доступность ссылки и попробуйте ещё раз."
		if errors.Is(downloadErr, apperrors.ErrAuthentication) {
			job.Error = "YouTube требует авторизацию. Подключите cookies на сервере и повторите скачивание."
		}
		if errors.Is(downloadErr, context.DeadlineExceeded) {
			job.Error = "Превышено время скачивания. Попробуйте более короткое видео."
		}
		if errors.Is(downloadErr, context.Canceled) {
			job.Status = "queued"
			job.Error = ""
			if err := s.jobs.Save(job); err != nil {
				return err
			}
			return downloadErr
		}
		if err := s.jobs.Save(job); err != nil {
			return err
		}
		// A completed failed web job is visible to its owner; explicit UI retry creates a new job.
		return nil
	}
	f, size, err := s.jobs.Open(job.ID, file)
	if err != nil {
		return err
	}
	f.Close()
	if size == 0 {
		return fmt.Errorf("empty downloaded file")
	}
	job.Status = "ready"
	job.File = file
	job.Size = size
	job.Error = ""
	job.ExpiresAt = time.Now().UTC().Add(s.ttl)
	return s.jobs.Save(job)
}

func publish(ctx context.Context, p Publisher, queue string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return p.Publish(ctx, queue, body)
}

type Downloader interface {
	Download(ctx context.Context, rawURL, outputDir string) (model.File, error)
}

type Publisher interface {
	Publish(ctx context.Context, queue string, body []byte) error
}
type WebJobs interface {
	Acquire(string) (func(), error)
	Save(model.WebJob) error
	Get(string) (model.WebJob, error)
	Open(string, model.File) (io.ReadSeekCloser, int64, error)
	Directory(string) (string, error)
}
