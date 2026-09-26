package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
	"video-downloader/internal/service/video"
)

type Downloader interface {
	Download(ctx context.Context, rawURL, outputDir string) (model.File, error)
}

type Publisher interface {
	Publish(ctx context.Context, queue string, body []byte) error
}
type Telegram interface {
	SendVideo(ctx context.Context, chatID int64, path string) error
}

type DownloadFiles interface {
	Directory(id string) (string, error)
	Cached(id string) (model.File, bool, error)
	Remember(id string, file model.File) error
	Sent(id string) (bool, error)
	Cleanup(id string) error
}

type SendFiles interface {
	Path(id string, file model.File) (string, error)
	Sent(id string) (bool, error)
	MarkSent(id string) error
	Cleanup(id string) error
}
type Webhook struct{ publisher Publisher }

func NewWebhook(p Publisher) *Webhook { return &Webhook{publisher: p} }
func (s *Webhook) Enqueue(ctx context.Context, updateID, chatID int64, text string) error {
	url, err := video.Extract(text)
	if err != nil {
		return err
	}
	if chatID == 0 {
		return apperrors.Permanent(errors.New("missing chat ID"))
	}
	id := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", chatID, updateID)))
	return publish(ctx, s.publisher, model.DownloadQueue, model.DownloadJob{ID: hex.EncodeToString(id[:]), ChatID: chatID, URL: url})
}

type Download struct {
	downloader Downloader
	publisher  Publisher
	files      DownloadFiles
}

func NewDownload(d Downloader, p Publisher, f DownloadFiles) *Download {
	return &Download{downloader: d, publisher: p, files: f}
}
func (s *Download) Process(ctx context.Context, job model.DownloadJob) error {
	if job.ChatID == 0 {
		return apperrors.Permanent(errors.New("missing chat ID"))
	}
	if _, err := video.Detect(job.URL); err != nil {
		return apperrors.Permanent(err)
	}
	dir, err := s.files.Directory(job.ID)
	if err != nil {
		return err
	}
	sent, err := s.files.Sent(job.ID)
	if err != nil {
		return err
	}
	if sent {
		return s.files.Cleanup(job.ID)
	}
	file, found, err := s.files.Cached(job.ID)
	if err != nil {
		return err
	}
	if !found {
		file, err = s.downloader.Download(ctx, job.URL, dir)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		if err = s.files.Remember(job.ID, file); err != nil {
			return err
		}
	}
	// Retain the file on ambiguous publish failure; the next attempt reuses it.
	return publish(ctx, s.publisher, model.SendQueue, model.SendJob{ID: job.ID, ChatID: job.ChatID, File: file})
}

type Send struct {
	client Telegram
	files  SendFiles
}

func NewSend(c Telegram, f SendFiles) *Send { return &Send{client: c, files: f} }
func (s *Send) Process(ctx context.Context, job model.SendJob) error {
	if job.ChatID == 0 {
		return apperrors.Permanent(errors.New("missing chat ID"))
	}
	sent, err := s.files.Sent(job.ID)
	if err != nil {
		return err
	}
	if !sent {
		path, err := s.files.Path(job.ID, job.File)
		if err != nil {
			return err
		}
		if err = s.client.SendVideo(ctx, job.ChatID, path); err != nil {
			return err
		}
		if err = s.files.MarkSent(job.ID); err != nil {
			return fmt.Errorf("record delivery: %w", err)
		}
	}
	if err := s.files.Cleanup(job.ID); err != nil {
		slog.Warn("sent file cleanup failed", "job_id", job.ID, "error", err)
		return err
	}
	return nil
}

func publish(ctx context.Context, p Publisher, queue string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return p.Publish(ctx, queue, body)
}
