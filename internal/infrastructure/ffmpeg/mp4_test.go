package ffmpeg_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"video-downloader/internal/infrastructure/command"
	"video-downloader/internal/infrastructure/ffmpeg"
)

func TestConvertLimitsResolution(t *testing.T) {
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is not installed", binary)
		}
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	create := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=5",
		"-t", "1", "-c:v", "mpeg4", "-threads:v", "1", input)
	if body, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create test video: %v: %s", err, body)
	}
	if err := ffmpeg.NewMP4(command.Exec{}, 50_000_000).Convert(context.Background(), input, output); err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", output)
	dimensions, err := probe.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(dimensions)); got != "1280x720" {
		t.Fatalf("output dimensions = %s, want 1280x720", got)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output video missing or empty: %v", err)
	}
}
