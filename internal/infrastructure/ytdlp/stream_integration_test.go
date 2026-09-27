package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"video-downloader/internal/infrastructure/command"
)

// RUN_MEDIA_INTEGRATION=1 go test ./internal/infrastructure/ytdlp -run TestStreamIntegration
// All media is generated and served in RAM; only extractor metadata goes to a temp file.
func TestStreamIntegration(t *testing.T) {
	if os.Getenv("RUN_MEDIA_INTEGRATION") != "1" {
		t.Skip("requires yt-dlp, ffmpeg and ffprobe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	generate := func(args ...string) []byte {
		t.Helper()
		args = append([]string{"-hide_banner", "-loglevel", "error"}, args...)
		args = append(args, "-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof", "pipe:1")
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		var diagnostic bytes.Buffer
		cmd.Stderr = &diagnostic
		data, err := cmd.Output()
		if err != nil {
			t.Fatalf("fixture: %v %s", err, diagnostic.String())
		}
		return data
	}
	video := generate("-f", "lavfi", "-i", "color=c=black:s=32x32:r=10", "-t", "0.3", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	audio := generate("-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "0.3", "-vn", "-c:a", "aac")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := video
		if r.URL.Path == "/audio.m4a" {
			data = audio
		}
		http.ServeContent(w, r, "media", time.Time{}, bytes.NewReader(data))
	}))
	defer source.Close()
	info := map[string]any{"id": "test", "title": "test", "extractor": "generic", "webpage_url": "https://youtu.be/test", "formats": []map[string]any{
		{"format_id": "video", "url": source.URL + "/video.mp4", "ext": "mp4", "protocol": "http", "vcodec": "avc1.42E01E", "acodec": "none", "width": 32, "height": 32},
		{"format_id": "audio", "url": source.URL + "/audio.m4a", "ext": "m4a", "protocol": "http", "vcodec": "none", "acodec": "mp4a.40.2"},
	}}
	dir := t.TempDir()
	metadata := filepath.Join(dir, "info.json")
	body, _ := json.Marshal(info)
	if err := os.WriteFile(metadata, body, 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Fatal(err)
	}
	runner := runnerFunc(func(ctx context.Context, w io.Writer, binary string, args ...string) error {
		args = append(args[:len(args)-2], "--load-info-json", metadata)
		shellArgs := []string{"-c", `cd "$1" && shift && exec "$@"`, "integration", dir, binary}
		return (command.Exec{}).Stream(ctx, w, "sh", append(shellArgs, args...)...)
	})
	d := New(runner, binary, "", 1<<20, 1<<20)
	media, err := d.Download(ctx, "https://youtu.be/test")
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	reader, err := media.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	probe := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "json", "pipe:0")
	probe.Stdin = reader
	result, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	var streams struct {
		Streams []struct {
			Type string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(result, &streams); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, s := range streams.Streams {
		found[s.Type] = true
	}
	if !found["audio"] || !found["video"] {
		t.Fatalf("missing track: %s", result)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "info.json" {
		t.Fatalf("downloader wrote files: %v", files)
	}
}
