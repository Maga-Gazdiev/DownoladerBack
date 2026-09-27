package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Token, Secret, WebToken, HTTPAddr, TelegramURL string
	YTDLP, CookiesBrowser, CookiesFile             string
	Concurrency, MaxJobs                           int
	MaxUploadBytes, MemoryBytes                    int64
	JobTimeout, SendTimeout, WebTTL                time.Duration
}

func Load() (Config, error) {
	c := Config{Token: os.Getenv("TELEGRAM_BOT_TOKEN"), Secret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"), WebToken: os.Getenv("WEB_API_TOKEN"), HTTPAddr: value("HTTP_ADDR", ":8085"), TelegramURL: value("TELEGRAM_API_URL", "https://api.telegram.org"), YTDLP: value("YT_DLP_BIN", "yt-dlp"), CookiesBrowser: os.Getenv("YOUTUBE_COOKIES_BROWSER"), CookiesFile: os.Getenv("YOUTUBE_COOKIES_FILE")}
	var err error
	n, err := positive("MAX_CONCURRENT_DOWNLOADS", "2")
	if err != nil {
		return c, err
	}
	c.Concurrency = int(n)
	n, err = positive("MAX_WEB_JOBS", "100")
	if err != nil {
		return c, err
	}
	c.MaxJobs = int(n)
	c.MaxUploadBytes, err = positive("MAX_UPLOAD_BYTES", "50000000")
	if err != nil {
		return c, err
	}
	c.MemoryBytes, err = positive("MAX_MEMORY_BYTES", "200000000")
	if err != nil {
		return c, err
	}
	if c.MemoryBytes < c.MaxUploadBytes {
		return c, fmt.Errorf("MAX_MEMORY_BYTES must be at least MAX_UPLOAD_BYTES")
	}
	c.JobTimeout, err = duration("DOWNLOAD_TIMEOUT", "20m")
	if err != nil {
		return c, err
	}
	c.SendTimeout, err = duration("SEND_TIMEOUT", "5m")
	if err != nil {
		return c, err
	}
	c.WebTTL, err = duration("WEB_FILE_TTL", "10m")
	if err != nil {
		return c, err
	}
	return c, nil
}
func value(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
func positive(key, fallback string) (int64, error) {
	n, err := strconv.ParseInt(value(key, fallback), 10, strconv.IntSize)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}
func duration(key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(value(key, fallback))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}
