package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
)

var ErrWebNotReady = errors.New("file is not ready")
var ErrWebExpired = errors.New("download expired")

type entry struct {
	job   model.WebJob
	media *model.Media
	timer *time.Timer
}

func (s *Service) Create(ctx context.Context, raw string) (model.WebJob, error) {
	platform, err := model.DetectPlatform(raw)
	if err != nil {
		return model.WebJob{}, err
	}
	var id [32]byte
	if _, err := rand.Read(id[:]); err != nil {
		return model.WebJob{}, err
	}
	now := time.Now().UTC()
	job := model.WebJob{ID: hex.EncodeToString(id[:]), URL: raw, Platform: platform, Status: "downloading", CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.WebJob{}, context.Canceled
	}
	if len(s.jobs) >= s.maxJobs {
		return model.WebJob{}, apperrors.ErrBusy
	}
	e := &entry{job: job}
	s.jobs[job.ID] = e
	if err := s.tasks.Start(ctx, func(ctx context.Context) { s.process(ctx, job.ID, raw) }); err != nil {
		delete(s.jobs, job.ID)
		return model.WebJob{}, err
	}
	return job, nil
}
func (s *Service) process(ctx context.Context, id, raw string) {
	media, err := s.download(ctx, raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.jobs[id]
	if !ok || s.closed {
		if media != nil {
			media.Close()
		}
		return
	}
	if err != nil {
		if media != nil {
			media.Close()
		}
		slog.Warn("web download failed", "job_id", id, "reason", downloadFailureCode(err))
		e.job.Status = "failed"
		e.job.Error = "Не удалось скачать видео. Проверьте доступность ссылки и попробуйте ещё раз."
		if errors.Is(err, apperrors.ErrAuthentication) {
			e.job.Error = "YouTube отклонил анонимный запрос или видео требует входа в аккаунт."
		}
		if errors.Is(err, context.DeadlineExceeded) {
			e.job.Error = "Превышено время скачивания. Попробуйте более короткое видео."
		}
		if errors.Is(err, apperrors.ErrBusy) {
			e.job.Error = "Недостаточно свободной памяти. Попробуйте позже."
		}
	} else {
		e.media = media
		e.job.File = media.File
		e.job.Size = media.Size()
		e.job.Status = "ready"
	}
	e.job.ExpiresAt = time.Now().UTC().Add(s.ttl)
	e.timer = time.AfterFunc(s.ttl, func() { s.expire(id) })
}
func (s *Service) expire(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.jobs[id]; ok {
		delete(s.jobs, id)
		if e.media != nil {
			e.media.Close()
		}
	}
}
func (s *Service) get(id string) (*entry, error) {
	e, ok := s.jobs[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if e.job.Status != "downloading" && time.Now().After(e.job.ExpiresAt) {
		return nil, ErrWebExpired
	}
	return e, nil
}
func (s *Service) Get(ctx context.Context, id string) (model.WebJob, error) {
	if err := ctx.Err(); err != nil {
		return model.WebJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.get(id)
	if err != nil {
		return model.WebJob{}, err
	}
	return e.job, nil
}
func (s *Service) Open(ctx context.Context, id string) (io.ReadSeekCloser, model.WebJob, error) {
	if err := ctx.Err(); err != nil {
		return nil, model.WebJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.get(id)
	if err != nil {
		return nil, model.WebJob{}, err
	}
	if e.job.Status != "ready" {
		return nil, e.job, ErrWebNotReady
	}
	r, err := e.media.Open()
	return r, e.job, err
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for id, e := range s.jobs {
		if e.timer != nil {
			e.timer.Stop()
		}
		if e.media != nil {
			e.media.Close()
		}
		delete(s.jobs, id)
	}
}

type Downloader interface {
	Download(context.Context, string) (*model.Media, error)
}
type Tasks interface {
	Start(context.Context, func(context.Context)) error
}

func (s *Service) download(ctx context.Context, raw string) (*model.Media, error) {
	ctx, cancel := context.WithTimeout(ctx, s.downloadTimeout)
	defer cancel()
	return s.downloader.Download(ctx, raw)
}

type Service struct {
	downloader           Downloader
	tasks                Tasks
	downloadTimeout, ttl time.Duration
	maxJobs              int
	mu                   sync.Mutex
	jobs                 map[string]*entry
	closed               bool
}

func New(tasks Tasks, d Downloader, timeout, ttl time.Duration, maxJobs int) *Service {
	return &Service{tasks: tasks, downloader: d, downloadTimeout: timeout, ttl: ttl, maxJobs: maxJobs, jobs: make(map[string]*entry)}
}
