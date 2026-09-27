package backblaze

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/minio/minio-go/v7"
	"time"

	"video-downloader/internal/service/model"
)

// Run with B2_INTEGRATION=1 and the B2 env vars to verify real bucket access.
func TestB2Integration(t *testing.T) {
	if os.Getenv("B2_INTEGRATION") != "1" {
		t.Skip("set B2_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := NewWeb(t.TempDir(), os.Getenv("B2_ENDPOINT"), os.Getenv("B2_BUCKET"), os.Getenv("KEYID"), os.Getenv("APPLICATIONKEY"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Check(ctx); err != nil {
		t.Fatal(err)
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(random[:])
	job := model.WebJob{ID: id, Status: "queued", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		job.ExpiresAt = time.Now().Add(-time.Hour)
		if err := store.Save(cleanupCtx, job); err != nil {
			t.Errorf("mark test job expired: %v", err)
		}
		if err := store.Sweep(cleanupCtx, time.Now()); err != nil {
			t.Errorf("clean test job: %v", err)
		}
		for object := range store.client.ListObjects(cleanupCtx, store.bucket, minio.ListObjectsOptions{Prefix: prefix + id + "/", Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				t.Errorf("list test objects: %v", object.Err)
			} else {
				t.Errorf("test object version remained: %s", object.Key)
			}
		}
	}()
	reopened, err := NewWeb(t.TempDir(), os.Getenv("B2_ENDPOINT"), os.Getenv("B2_BUCKET"), os.Getenv("KEYID"), os.Getenv("APPLICATIONKEY"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, id)
	if err != nil || got.ID != id {
		t.Fatalf("Get = %v, %v", got, err)
	}
	dir, err := store.Directory(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := model.File{Hash: id, Name: id + ".mp4"}
	if err := os.WriteFile(filepath.Join(dir, file.Name), []byte("test-video"), 0600); err != nil {
		t.Fatal(err)
	}
	size, err := store.StoreFile(ctx, id, file)
	if err != nil || size != 10 {
		t.Fatalf("StoreFile = %d, %v", size, err)
	}
	job.File = file
	job.Status = "ready"
	if err := store.Save(ctx, job); err != nil {
		t.Fatal(err)
	}
	remote, size, err := reopened.Open(ctx, id, file)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if size != 10 {
		t.Fatalf("size = %d", size)
	}
	if _, err := remote.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(remote)
	if err != nil || string(data) != "test-video" {
		t.Fatalf("remote file = %q, %v", data, err)
	}
	ranged, _, err := reopened.Open(ctx, id, file)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/file", nil)
	req.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	http.ServeContent(response, req, file.Name, time.Time{}, ranged)
	if err := ranged.Close(); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusPartialContent || response.Body.String() != "st-v" {
		t.Fatalf("range response = %d %q", response.Code, response.Body.String())
	}
}
