package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"video-downloader/internal/service/model"
)

// WebStore lives under a separate root; Telegram cleanup cannot touch web jobs.
type WebStore struct {
	*Store
	mu     sync.Mutex
	active map[string]int
}

func NewWeb(root string) (*WebStore, error) {
	s, err := New(filepath.Join(root, "web"))
	return &WebStore{Store: s, active: make(map[string]int)}, err
}

// Acquire protects a job from retention cleanup while a worker or reader uses it.
// One WebStore instance is shared by all components in the single app process.
func (s *WebStore) Acquire(id string) (func(), error) {
	if _, err := s.Directory(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.active[id]++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.active[id]--
			if s.active[id] == 0 {
				delete(s.active, id)
			}
		})
	}, nil
}

func (s *WebStore) Active(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[id] != 0
}

func (s *WebStore) Save(_ context.Context, job model.WebJob) error {
	dir, err := s.Directory(job.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	body, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "job.json"), body)
}
func (s *WebStore) Get(_ context.Context, id string) (model.WebJob, error) {
	var job model.WebJob
	dir, err := s.Directory(id)
	if err != nil {
		return job, err
	}
	body, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		return job, err
	}
	err = json.Unmarshal(body, &job)
	return job, err
}
func (s *WebStore) Open(_ context.Context, id string, file model.File) (io.ReadSeekCloser, int64, error) {
	release, err := s.Acquire(id)
	if err != nil {
		return nil, 0, err
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()
	path, err := s.Path(id, file)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, 0, fmt.Errorf("not a regular file")
	}
	keep = true
	return &leasedFile{File: f, release: release}, info.Size(), nil
}

// StoreFile verifies a locally downloaded video; the disk-backed store needs no upload.
func (s *WebStore) StoreFile(_ context.Context, id string, file model.File) (int64, error) {
	path, err := s.Path(id, file)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("not a regular file")
	}
	return info.Size(), nil
}

type leasedFile struct {
	*os.File
	release func()
}

func (f *leasedFile) Close() error {
	defer f.release()
	return f.File.Close()
}

func (s *WebStore) Sweep(ctx context.Context, now time.Time) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !hashPattern.MatchString(entry.Name()) {
			continue
		}
		if err := s.sweepJob(entry.Name(), now); err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup %s: %w", entry.Name(), err))
		}
	}
	return result
}

func (s *WebStore) sweepJob(id string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[id] != 0 {
		return nil
	}
	job, err := s.Get(context.Background(), id)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if job.ID != id {
		return fmt.Errorf("job directory and metadata ID differ")
	}
	if now.Before(job.ExpiresAt) {
		return nil
	}
	return s.Cleanup(id)
}
