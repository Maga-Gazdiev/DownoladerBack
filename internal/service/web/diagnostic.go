package web

import (
	"context"
	"errors"

	apperrors "video-downloader/internal/errors"
)

// The downloader may provide a fixed diagnostic code; never log its raw error.
func downloadFailureCode(err error) string {
	if errors.Is(err, apperrors.ErrAuthentication) {
		return "authentication"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var coded interface{ FailureCode() string }
	if errors.As(err, &coded) {
		return coded.FailureCode()
	}
	return "unknown"
}
