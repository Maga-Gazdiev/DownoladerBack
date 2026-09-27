package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"video-downloader/internal/model"
)

func TestSendVideoFromMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botsecret/sendVideo" {
			t.Errorf("path: %s", r.URL.Path)
		}
		multipart, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		fields := map[string]string{}
		for {
			part, err := multipart.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Error(err)
				return
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Error(err)
				return
			}
			fields[part.FormName()] = string(data)
		}
		if fields["chat_id"] != "42" || fields["video"] != "media" || fields["supports_streaming"] != "true" {
			t.Errorf("multipart: %v", fields)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	media := model.NewMedia(model.File{Name: "test.mp4"}, []byte("media"), nil)
	defer media.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := New("secret", server.URL, server.Client(), 100).SendVideo(ctx, 42, media); err != nil {
		t.Fatal(err)
	}
}
func TestEarlyRejectionReleasesUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"ok":false,"error_code":400}`)
	}))
	defer server.Close()
	media := model.NewMedia(model.File{Name: "test.mp4"}, make([]byte, 4<<20), nil)
	defer media.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := New("secret", server.URL, server.Client(), 5<<20).SendVideo(ctx, 42, media); err == nil {
		t.Fatal("accepted rejected upload")
	}
}
