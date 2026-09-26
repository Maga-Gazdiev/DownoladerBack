package video

import (
	"net/url"
	"regexp"
	"strings"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/service/model"
)

func Detect(raw string) (model.Platform, error) {
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return "", apperrors.ErrUnsupportedURL
	}
	host := strings.ToLower(u.Hostname())
	for _, candidate := range []struct {
		host     string
		platform model.Platform
	}{
		{"youtube.com", model.YouTube}, {"youtu.be", model.YouTube}, {"instagram.com", model.Instagram}, {"tiktok.com", model.TikTok},
	} {
		if host == candidate.host || strings.HasSuffix(host, "."+candidate.host) {
			return candidate.platform, nil
		}
	}
	return "", apperrors.ErrUnsupportedURL
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>]+`)

func Extract(text string) (string, error) {
	for _, raw := range urlPattern.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,!?;:)]}\"'")
		if _, err := Detect(raw); err == nil {
			return raw, nil
		}
	}
	return "", apperrors.ErrUnsupportedURL
}
