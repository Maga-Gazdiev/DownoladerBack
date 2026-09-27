package command

import (
	"context"
	"io"
	"os/exec"
	"time"
)

type Exec struct{}

func (Exec) Stream(ctx context.Context, output io.Writer, binary string, args ...string) error {
	return execute(ctx, output, binary, args...)
}

func execute(ctx context.Context, stdout io.Writer, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = stdout
	configure(cmd)
	cmd.WaitDelay = 3 * time.Second
	var diagnostic tailBuffer
	cmd.Stderr = &diagnostic
	// Downloader output can contain cookies and signed URLs. Do not log it.
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return failure(binary, err, diagnostic.data)
	}
	return nil
}

// Retain only a bounded tail for classification; never expose raw stderr or cookies.
type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n >= 16384 {
		b.data = append(b.data[:0], p[n-16384:]...)
		return n, nil
	}
	if len(b.data)+n > 16384 {
		b.data = append(b.data[:0], b.data[len(b.data)+n-16384:]...)
	}
	b.data = append(b.data, p...)
	return n, nil
}
