package web

import (
	"context"
	"errors"
	"fmt"
	"testing"

	apperrors "video-downloader/internal/errors"
)

type codedFailure struct{}

func (codedFailure) Error() string       { return "raw diagnostic" }
func (codedFailure) FailureCode() string { return "http_403" }

func TestDownloadFailureCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"coded", fmt.Errorf("download: %w", codedFailure{}), "http_403"},
		{"authentication", apperrors.Permanent(apperrors.ErrAuthentication), "authentication"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"unknown", errors.New("raw diagnostic"), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloadFailureCode(tt.err); got != tt.want {
				t.Fatalf("downloadFailureCode() = %q, want %q", got, tt.want)
			}
		})
	}
}
