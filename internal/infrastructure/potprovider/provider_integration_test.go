package potprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"video-downloader/internal/infrastructure/command"
)

func TestGenerateAnonymousToken(t *testing.T) {
	home := os.Getenv("TEST_PO_TOKEN_HOME")
	if home == "" {
		t.Skip("set TEST_PO_TOKEN_HOME to built bgutil server directory; requires internet")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p, err := Start(ctx, command.Exec{}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL+"/get_pot", strings.NewReader(`{"content_binding":"dQw4w9WgXcQ"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token generation HTTP %d", resp.StatusCode)
	}
	var token struct {
		Token string `json:"poToken"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		t.Fatal(err)
	}
	if token.Token == "" {
		t.Fatal("empty PO token")
	}
	t.Log("anonymous PO token generated; value omitted")
}
