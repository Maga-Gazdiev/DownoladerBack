package config

import (
	"strings"
	"testing"
)

func TestLoadB2Configuration(t *testing.T) {
	t.Setenv("B2_ENDPOINT", "https://s3.us-east-005.backblazeb2.com")
	t.Setenv("B2_BUCKET", "DownloaderDFDFDF")
	t.Setenv("KEYID", "test-id")
	t.Setenv("APPLICATIONKEY", "test-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.B2Endpoint != "https://s3.us-east-005.backblazeb2.com" || cfg.B2Bucket != "DownloaderDFDFDF" {
		t.Fatalf("unexpected B2 location: %q %q", cfg.B2Endpoint, cfg.B2Bucket)
	}
	if cfg.B2KeyID != "test-id" || cfg.B2ApplicationKey != "test-secret" {
		t.Fatal("B2 credentials were not loaded")
	}
}

func TestLoadRejectsPartialB2Configuration(t *testing.T) {
	t.Setenv("B2_ENDPOINT", "https://s3.us-east-005.backblazeb2.com")
	t.Setenv("B2_BUCKET", "")
	t.Setenv("KEYID", "test-id")
	t.Setenv("APPLICATIONKEY", "test-secret")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "B2_BUCKET") {
		t.Fatalf("expected incomplete B2 configuration error, got %v", err)
	}
}
