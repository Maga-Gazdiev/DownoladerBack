package model

import (
	"net/url"
	"regexp"
	"strings"

	apperrors "video-downloader/internal/errors"
)

func DetectPlatform(raw string) (Platform, error) {
	u, err := url.ParseRequestURI(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return "", apperrors.ErrUnsupportedURL
	}
	host := strings.ToLower(u.Hostname())
	for _, candidate := range []struct {
		host     string
		platform Platform
	}{
		{"youtube.com", YouTube}, {"youtu.be", YouTube}, {"instagram.com", Instagram}, {"tiktok.com", TikTok},
	} {
		if host == candidate.host || strings.HasSuffix(host, "."+candidate.host) {
			return candidate.platform, nil
		}
	}
	return "", apperrors.ErrUnsupportedURL
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>]+`)

func ExtractURL(text string) (string, error) {
	for _, raw := range urlPattern.FindAllString(text, -1) {
		raw = strings.TrimRight(raw, ".,!?;:)]}\"'")
		if _, err := DetectPlatform(raw); err == nil {
			return raw, nil
		}
	}
	return "", apperrors.ErrUnsupportedURL
}
