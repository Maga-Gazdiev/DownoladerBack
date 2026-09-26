package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
)

// Save isolates partial downloads, accepts exactly one video, and hashes its bytes.
func Save(ctx context.Context, dir string, download func(string) error) (model.File, error) {
	if err := ctx.Err(); err != nil {
		return model.File{}, err
	}
	if dir == "" {
		return model.File{}, errors.New("output directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return model.File{}, err
	}
	stage, err := os.MkdirTemp(dir, ".download-")
	if err != nil {
		return model.File{}, err
	}
	defer os.RemoveAll(stage)
	if err := download(stage); err != nil {
		return model.File{}, err
	}
	path, err := findVideo(ctx, stage)
	if err != nil {
		return model.File{}, err
	}
	file, err := identifyVideo(ctx, path)
	if err != nil {
		return model.File{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.File{}, err
	}
	if err := os.Rename(path, filepath.Join(dir, file.Name)); err != nil {
		return model.File{}, fmt.Errorf("save video: %w", err)
	}
	return file, nil
}

func findVideo(ctx context.Context, stage string) (string, error) {
	var path string
	err := filepath.WalkDir(stage, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("unexpected symlink in download")
		}
		if entry.IsDir() {
			return nil
		}
		if !VideoExtension(filepath.Ext(p)) {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("downloaded video is not a regular file")
		}
		if path != "" {
			return apperrors.Permanent(errors.New("URL must contain exactly one video"))
		}
		path = p
		return nil
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("downloader produced no video")
	}
	return path, nil
}

func identifyVideo(ctx context.Context, path string) (model.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.File{}, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(h, &contextReader{ctx: ctx, reader: f})
	closeErr := f.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return model.File{}, err
	}
	if n == 0 {
		return model.File{}, errors.New("downloaded video is empty")
	}
	hash := hex.EncodeToString(h.Sum(nil))
	return model.File{Hash: hash, Name: hash + strings.ToLower(filepath.Ext(path))}, nil
}

func VideoExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".webm", ".mkv", ".mov", ".avi", ".flv", ".m4v":
		return true
	}
	return false
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
