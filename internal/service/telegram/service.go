package telegram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
)

type delivery struct {
	running bool
	expires time.Time
}

func (s *Service) Submit(ctx context.Context, updateID, chatID int64, text string) error {
	raw, err := model.ExtractURL(text)
	if err != nil {
		return err
	}
	if chatID == 0 {
		return apperrors.Permanent(errors.New("missing chat ID"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, d := range s.seen {
		if !d.running && now.After(d.expires) {
			delete(s.seen, id)
		}
	}
	if _, ok := s.seen[updateID]; ok {
		return nil
	}
	if len(s.seen) >= 10000 {
		return apperrors.ErrBusy
	}
	s.seen[updateID] = delivery{running: true}
	err = s.tasks.Start(ctx, func(ctx context.Context) {
		success := false
		defer func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if success {
				s.seen[updateID] = delivery{expires: time.Now().Add(time.Hour)}
			} else {
				delete(s.seen, updateID)
			}
		}()
		media, err := s.download(ctx, raw)
		if err != nil {
			slog.Warn("telegram download failed", "update_id", updateID)
			return
		}
		defer media.Close()
		sendCtx, cancel := context.WithTimeout(ctx, s.sendTimeout)
		defer cancel()
		if err := s.client.SendVideo(sendCtx, chatID, media); err != nil {
			slog.Warn("telegram send failed", "update_id", updateID)
			return
		}
		success = true
	})
	if err != nil {
		delete(s.seen, updateID)
	}
	return err
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

type Telegram interface {
	SendVideo(context.Context, int64, *model.Media) error
}
type Service struct {
	downloader                   Downloader
	client                       Telegram
	tasks                        Tasks
	downloadTimeout, sendTimeout time.Duration
	mu                           sync.Mutex
	seen                         map[int64]delivery
}

func New(tasks Tasks, d Downloader, client Telegram, downloadTimeout, sendTimeout time.Duration) *Service {
	return &Service{tasks: tasks, downloader: d, client: client, downloadTimeout: downloadTimeout, sendTimeout: sendTimeout, seen: make(map[int64]delivery)}
}
