package backblaze

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"video-downloader/internal/service/model"
	"video-downloader/internal/storage/files"
)

const prefix = "web/"

// WebStore keeps only temporary download files locally. Job state and completed
// videos live in B2, so another process can read them after a restart.
type WebStore struct {
	local  *files.WebStore
	client *minio.Client
	bucket string
}

func NewWeb(root, endpoint, bucket, keyID, applicationKey string) (*WebStore, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("B2_ENDPOINT must be an HTTPS S3 endpoint without a path")
	}
	if !strings.HasPrefix(parsed.Host, "s3.") || !strings.HasSuffix(parsed.Host, ".backblazeb2.com") {
		return nil, errors.New("B2_ENDPOINT must be a Backblaze S3 endpoint")
	}
	region := strings.TrimSuffix(strings.TrimPrefix(parsed.Host, "s3."), ".backblazeb2.com")
	client, err := minio.New(parsed.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(keyID, applicationKey, ""),
		Secure:       true,
		Region:       region,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, err
	}
	local, err := files.NewWeb(root)
	if err != nil {
		return nil, err
	}
	return &WebStore{local: local, client: client, bucket: bucket}, nil
}

func (s *WebStore) Check(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check B2 bucket: %w", err)
	}
	if !exists {
		return fmt.Errorf("B2 bucket %q does not exist or is inaccessible", s.bucket)
	}
	return nil
}

func (s *WebStore) Acquire(id string) (func(), error)   { return s.local.Acquire(id) }
func (s *WebStore) Directory(id string) (string, error) { return s.local.Directory(id) }

func (s *WebStore) jobKey(id string) (string, error) {
	if _, err := s.local.Directory(id); err != nil {
		return "", err
	}
	return prefix + id + "/job.json", nil
}

func (s *WebStore) fileKey(id string, file model.File) (string, error) {
	if _, err := s.local.Path(id, file); err != nil {
		return "", err
	}
	return prefix + id + "/" + file.Name, nil
}

func (s *WebStore) Save(ctx context.Context, job model.WebJob) error {
	key, err := s.jobKey(job.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return fmt.Errorf("save B2 job: %w", err)
	}
	if job.Status == "ready" || job.Status == "failed" {
		if err := s.local.Cleanup(job.ID); err != nil {
			slog.Warn("remove temporary web download", "job_id", job.ID, "error", err)
		}
	}
	return nil
}

func (s *WebStore) Get(ctx context.Context, id string) (model.WebJob, error) {
	var job model.WebJob
	key, err := s.jobKey(id)
	if err != nil {
		return job, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return job, objectError(err)
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, 1<<20))
	if err != nil {
		return job, objectError(err)
	}
	if err := json.Unmarshal(data, &job); err != nil {
		return job, err
	}
	if job.ID != id {
		return job, errors.New("B2 job ID mismatch")
	}
	return job, nil
}

func (s *WebStore) StoreFile(ctx context.Context, id string, file model.File) (int64, error) {
	key, err := s.fileKey(id, file)
	if err != nil {
		return 0, err
	}
	path, err := s.local.Path(id, file)
	if err != nil {
		return 0, err
	}
	source, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, errors.New("downloaded file is empty or not regular")
	}
	_, err = s.client.PutObject(ctx, s.bucket, key, source, info.Size(), minio.PutObjectOptions{ContentType: "application/octet-stream", PartSize: 16 << 20, NumThreads: 1})
	if err != nil {
		return 0, fmt.Errorf("upload B2 video: %w", err)
	}
	return info.Size(), nil
}

func (s *WebStore) Open(ctx context.Context, id string, file model.File) (io.ReadSeekCloser, int64, error) {
	key, err := s.fileKey(id, file)
	if err != nil {
		return nil, 0, err
	}
	release, err := s.local.Acquire(id)
	if err != nil {
		return nil, 0, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		release()
		return nil, 0, objectError(err)
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		release()
		return nil, 0, objectError(err)
	}
	return &leasedObject{Object: object, release: release}, info.Size, nil
}

type leasedObject struct {
	*minio.Object
	release func()
}

func (o *leasedObject) Close() error {
	defer o.release()
	return o.Object.Close()
}

// Sweep deletes every version: B2's default "Keep all versions" policy would
// otherwise retain old metadata and video even after deleting the visible key.
func (s *WebStore) Sweep(ctx context.Context, now time.Time) error {
	groups := make(map[string][]minio.ObjectInfo)
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			return object.Err
		}
		parts := strings.Split(object.Key, "/")
		if len(parts) != 3 || parts[0] != "web" || len(parts[1]) != 64 {
			continue
		}
		groups[parts[1]] = append(groups[parts[1]], object)
	}
	var result error
	for id, versions := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.local.Active(id) {
			continue
		}
		job, err := s.Get(ctx, id)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("read B2 job %s: %w", id, err))
			continue
		}
		if err == nil && now.Before(job.ExpiresAt) {
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			newest := time.Time{}
			for _, version := range versions {
				if version.LastModified.After(newest) {
					newest = version.LastModified
				}
			}
			if now.Sub(newest) < 24*time.Hour {
				continue
			}
		}
		for _, version := range versions {
			if err := s.client.RemoveObject(ctx, s.bucket, version.Key, minio.RemoveObjectOptions{VersionID: version.VersionID}); err != nil {
				result = errors.Join(result, fmt.Errorf("remove B2 object %s: %w", version.Key, err))
			}
		}
	}
	// Abandoned partial downloads are local only; remove those older than a day.
	localDir, err := s.local.Directory(strings.Repeat("0", 64))
	if err == nil {
		entries, readErr := os.ReadDir(filepath.Dir(localDir))
		if readErr != nil {
			return errors.Join(result, readErr)
		}
		for _, entry := range entries {
			if !entry.IsDir() || len(entry.Name()) != 64 {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				result = errors.Join(result, infoErr)
				continue
			}
			if now.Sub(info.ModTime()) > 24*time.Hour && !s.local.Active(entry.Name()) {
				if err := s.local.Cleanup(entry.Name()); err != nil {
					result = errors.Join(result, err)
				}
			}
		}
	}
	return result
}

func objectError(err error) error {
	code := minio.ToErrorResponse(err).Code
	if code == "NoSuchKey" || code == "NoSuchBucket" || code == "NotFound" {
		return fmt.Errorf("%w: %v", os.ErrNotExist, err)
	}
	return err
}
