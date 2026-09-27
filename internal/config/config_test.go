package config

import "testing"

func TestLimits(t *testing.T) {
	for _, key := range []string{"MAX_CONCURRENT_DOWNLOADS", "MAX_WEB_JOBS", "MAX_UPLOAD_BYTES", "MAX_MEMORY_BYTES", "DOWNLOAD_TIMEOUT", "SEND_TIMEOUT", "WEB_FILE_TTL"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "0")
			if _, err := Load(); err == nil {
				t.Fatal("accepted zero limit")
			}
		})
	}
}
func TestMemoryFitsVideo(t *testing.T) {
	t.Setenv("MAX_UPLOAD_BYTES", "100")
	t.Setenv("MAX_MEMORY_BYTES", "99")
	if _, err := Load(); err == nil {
		t.Fatal("accepted insufficient memory")
	}
}
