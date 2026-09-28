package command

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	apperrors "video-downloader/internal/errors"
)

func TestFailureCode(t *testing.T) {
	tests := []struct {
		name, stderr, want string
	}{
		{"instagram empty with login hint", "Instagram sent an empty media response. If it is not accessible, use --cookies", "instagram_empty_response"},
		{"forbidden", "HTTP Error 403: Forbidden", "http_403"},
		{"rate limit", "HTTP Error 429: Too Many Requests", "rate_limited"},
		{"disk", "No space left on device", "disk_full"},
		{"postprocessing", "ERROR: Postprocessing: Conversion failed!", "postprocessing"},
		{"network", "Connection reset by peer", "network"},
		{"format", "Requested format is not available", "format_unavailable"},
		{"unknown", "unexpected error", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const secret = "signed-url-secret"
			err := failure("yt-dlp", errors.New("exit status 1"), []byte(tt.stderr+" https://video.example/?sig="+secret))
			var coded interface{ FailureCode() string }
			if !errors.As(err, &coded) || coded.FailureCode() != tt.want {
				t.Fatalf("failure code = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("failure exposed command stderr")
			}
		})
	}
}

func TestFailureAuthentication(t *testing.T) {
	err := failure("yt-dlp", errors.New("exit status 1"), []byte("Sign in to confirm you're not a bot"))
	if !errors.Is(err, apperrors.ErrAuthentication) || !apperrors.IsPermanent(err) {
		t.Fatalf("authentication classification = %v", err)
	}
}

func TestFailureCommandNotFound(t *testing.T) {
	err := failure("/usr/local/bin/yt-dlp", &exec.Error{Name: "yt-dlp", Err: exec.ErrNotFound}, nil)
	var coded interface{ FailureCode() string }
	if !errors.As(err, &coded) || coded.FailureCode() != "command_not_found" {
		t.Fatalf("missing command classification = %v", err)
	}
}
