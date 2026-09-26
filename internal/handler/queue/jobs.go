package queue

import (
	"context"
	"encoding/json"
	"fmt"

	apperrors "video-downloader/internal/errors"
)

// JSONJob translates broker payloads without exposing AMQP to the service.
func JSONJob[T any](process func(context.Context, T) error) func(context.Context, []byte) error {
	return func(ctx context.Context, body []byte) error {
		var job T
		if err := json.Unmarshal(body, &job); err != nil {
			return apperrors.Permanent(fmt.Errorf("invalid job JSON: %w", err))
		}
		return process(ctx, job)
	}
}
