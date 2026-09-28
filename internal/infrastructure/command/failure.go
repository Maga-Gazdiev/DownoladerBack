package command

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	apperrors "video-downloader/internal/errors"
)

// commandError contains a fixed code instead of stderr, which may include signed URLs.
type commandError struct {
	binary string
	code   string
	cause  error
}

func (e *commandError) Error() string {
	return fmt.Sprintf("%s failed (%s): %v", e.binary, e.code, e.cause)
}

func (e *commandError) Unwrap() error       { return e.cause }
func (e *commandError) FailureCode() string { return e.code }

func failure(binary string, cause error, stderr []byte) error {
	code := failureCode(stderr)
	if code == "authentication" {
		return apperrors.Permanent(apperrors.ErrAuthentication)
	}
	if errors.Is(cause, exec.ErrNotFound) {
		code = "command_not_found"
	}
	return &commandError{binary: filepath.Base(binary), code: code, cause: cause}
}

func failureCode(stderr []byte) string {
	message := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(message, "instagram sent an empty media response"):
		return "instagram_empty_response"
	case strings.Contains(message, "sign in to confirm"),
		strings.Contains(message, "login required"),
		strings.Contains(message, "use --cookies"):
		return "authentication"
	case strings.Contains(message, "http error 403"),
		strings.Contains(message, "403 forbidden"),
		strings.Contains(message, "403: forbidden"):
		return "http_403"
	case strings.Contains(message, "http error 429"),
		strings.Contains(message, "too many requests"),
		strings.Contains(message, "rate limit"):
		return "rate_limited"
	case strings.Contains(message, "no space left on device"):
		return "disk_full"
	case strings.Contains(message, "postprocessing:"),
		strings.Contains(message, "conversion failed"):
		return "postprocessing"
	case strings.Contains(message, "timed out"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "name or service not known"):
		return "network"
	case strings.Contains(message, "requested format is not available"),
		strings.Contains(message, "no video formats found"):
		return "format_unavailable"
	default:
		return "unknown"
	}
}
