package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
	"video-downloader/internal/storage/media"
)

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Store struct{ root string }

func New(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("download directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, ".sent"), 0700); err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}
func (s *Store) Directory(id string) (string, error) {
	if !hashPattern.MatchString(id) {
		return "", apperrors.Permanent(errors.New("invalid job ID"))
	}
	return filepath.Join(s.root, id), nil
}
func (s *Store) Path(id string, file model.File) (string, error) {
	dir, err := s.Directory(id)
	if err != nil {
		return "", err
	}
	ext := filepath.Ext(file.Name)
	if !hashPattern.MatchString(file.Hash) || file.Name != file.Hash+ext || !media.VideoExtension(ext) {
		return "", apperrors.Permanent(errors.New("invalid file name"))
	}
	return filepath.Join(dir, file.Name), nil
}
func (s *Store) Cached(id string) (model.File, bool, error) {
	dir, err := s.Directory(id)
	if err != nil {
		return model.File{}, false, err
	}
	body, err := os.ReadFile(filepath.Join(dir, "ready.json"))
	if errors.Is(err, os.ErrNotExist) {
		return model.File{}, false, nil
	}
	if err != nil {
		return model.File{}, false, err
	}
	var file model.File
	if err := json.Unmarshal(body, &file); err != nil {
		return file, false, fmt.Errorf("read cached result: %w", err)
	}
	path, err := s.Path(id, file)
	if err != nil {
		return file, false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.File{}, false, nil
	}
	if err != nil {
		return file, false, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return file, false, errors.New("cached video is not a nonempty regular file")
	}
	return file, true, nil
}
func (s *Store) Remember(id string, file model.File) error {
	path, err := s.Path(id, file)
	if err != nil {
		return err
	}
	body, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(filepath.Dir(path), "ready.json"), body)
}
func (s *Store) Sent(id string) (bool, error) {
	if _, err := s.Directory(id); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(s.root, ".sent", id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (s *Store) MarkSent(id string) error {
	if _, err := s.Directory(id); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.root, ".sent", id), []byte("sent\n"))
}
func (s *Store) Cleanup(id string) error {
	dir, err := s.Directory(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
func atomicWrite(path string, body []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
