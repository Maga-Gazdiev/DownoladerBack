// Package potprovider manages the anonymous bgutil token generator subprocess.
package potprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

const Version = "2.0.0"

type Runner interface {
	Stream(context.Context, io.Writer, string, ...string) error
}
type Provider struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	err    error // published by closing done
	URL    string
}

func Start(ctx context.Context, runner Runner, home string) (*Provider, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	address := listener.Addr().(*net.TCPAddr)
	port := fmt.Sprint(address.Port)
	listener.Close()
	ctx, cancel := context.WithCancel(ctx)
	p := &Provider{ctx: ctx, cancel: cancel, done: make(chan struct{}), URL: "http://127.0.0.1:" + port}
	go func() {
		defer close(p.done)
		defer cancel()
		p.err = runner.Stream(ctx, io.Discard, "node", filepath.Join(home, "build", "main.js"), "--host", "127.0.0.1", "--port", port)
		if p.err == nil {
			p.err = errors.New("PO token provider exited unexpectedly")
		}
	}()
	readyCtx, readyCancel := context.WithTimeout(ctx, 30*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for {
		select {
		case <-readyCtx.Done():
			p.Close()
			if err := p.Err(); err != nil {
				return nil, fmt.Errorf("start PO token provider: %w", err)
			}
			return nil, fmt.Errorf("PO token provider not ready: %w", readyCtx.Err())
		case <-ticker.C:
			if ping(readyCtx, client, p.URL) == nil {
				return p, nil
			}
		}
	}
}
func ping(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/ping", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || result.Version != Version {
		return errors.New("unexpected PO token provider version")
	}
	return nil
}
func (p *Provider) Context() context.Context { return p.ctx }
func (p *Provider) Close()                   { p.cancel(); <-p.done }
func (p *Provider) Err() error {
	select {
	case <-p.done:
		if !errors.Is(p.err, context.Canceled) {
			return p.err
		}
	default:
	}
	return nil
}
