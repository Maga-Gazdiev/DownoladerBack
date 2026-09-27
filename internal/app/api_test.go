package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"video-downloader/internal/handler/api"
	"video-downloader/internal/model"
	"video-downloader/internal/service/web"
)

type memorySource struct{}

func (memorySource) Download(context.Context, string) (*model.Media, error) {
	return model.NewMedia(model.File{Name: "video.mp4"}, []byte("0123456789"), nil), nil
}
func TestCreatePollAndRangeDownload(t *testing.T) {
	tasks := newTasks(context.Background(), 1)
	defer tasks.Close()
	s := web.New(tasks, memorySource{}, time.Second, time.Minute, 10)
	defer s.Close()
	handler := api.NewWebAPI(s, "secret")
	request := func(method, path, body, rangeHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Range", rangeHeader)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	response := request("POST", "/api/downloads", `{"url":"https://youtu.be/test"}`, "")
	if response.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}
	var job model.WebJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	path := "/api/downloads/" + job.ID
	deadline := time.Now().Add(time.Second)
	for {
		response = request("GET", path, "", "")
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Status == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not ready")
		}
		time.Sleep(time.Millisecond)
	}
	response = request("GET", path+"/file", "", "bytes=2-5")
	data, _ := io.ReadAll(response.Body)
	if response.Code != http.StatusPartialContent || string(data) != "2345" {
		t.Fatalf("range: %d %q", response.Code, data)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatal("missing authentication check")
	}
}
