package backblaze

import "testing"

func TestNewWebRejectsNonBackblazeEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"http://s3.us-east-005.backblazeb2.com",
		"https://example.com",
		"https://s3.us-east-005.backblazeb2.com/path",
		"https://s3.us-east-005.backblazeb2.com?token=x",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := NewWeb(t.TempDir(), endpoint, "bucket", "key", "secret"); err == nil {
				t.Fatal("expected endpoint validation error")
			}
		})
	}
}
