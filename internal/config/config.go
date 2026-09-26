package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	RabbitURL, QueuePrefix, Token, Secret, HTTPAddr, DownloadDir string
	YTDLP, GalleryDL, CookiesBrowser, Backend, TelegramURL       string
	VideoFormat, VideoResolution, VideoCodec                     string
	CookiesFile, WebToken                                        string
	WebTTL                                                       time.Duration
	CleanupInterval                                              time.Duration
	MaxRetries                                                   int
	JobTimeout, SendTimeout                                      time.Duration
	MaxUploadBytes                                               int64
}

func Load() (Config, error) {
	c := Config{
		RabbitURL: os.Getenv("RABBITMQ_URL"), QueuePrefix: os.Getenv("QUEUE_PREFIX"),
		Token: os.Getenv("TELEGRAM_BOT_TOKEN"), Secret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
		HTTPAddr: value("HTTP_ADDR", ":8085"), DownloadDir: value("DOWNLOAD_DIR", "downloads"),
		YTDLP: value("YT_DLP_BIN", "yt-dlp"), GalleryDL: value("GALLERY_DL_BIN", "gallery-dl"),
		CookiesBrowser: value("YOUTUBE_COOKIES_BROWSER", "chrome"), Backend: value("VIDEO_BACKEND", "yt-dlp"),
		TelegramURL: value("TELEGRAM_API_URL", "https://api.telegram.org"),
		VideoFormat: value("VIDEO_FORMAT", "mp4"), VideoResolution: value("VIDEO_RESOLUTION", "720"), VideoCodec: value("VIDEO_CODEC", "avc1"),
		CookiesFile: os.Getenv("YOUTUBE_COOKIES_FILE"), WebToken: os.Getenv("WEB_API_TOKEN"),
	}
	if c.RabbitURL == "" && os.Getenv("RABBIT_HOST") != "" {
		port := value("RABBIT_PORT", "5672")
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return c, errors.New("RABBIT_PORT must be between 1 and 65535")
		}
		vhost := value("RABBIT_VHOST", "/")
		u := url.URL{Scheme: "amqp", Host: net.JoinHostPort(os.Getenv("RABBIT_HOST"), port),
			User: url.UserPassword(os.Getenv("RABBIT_USER"), os.Getenv("RABBIT_PASSWORD")),
			Path: "/" + vhost, RawPath: "/" + url.PathEscape(vhost)}
		c.RabbitURL = u.String()
	}
	var err error
	c.MaxRetries, err = strconv.Atoi(value("MAX_RETRIES", "3"))
	if err != nil || c.MaxRetries < 0 || c.MaxRetries > 100 {
		return c, errors.New("MAX_RETRIES must be between 0 and 100")
	}
	c.MaxUploadBytes, err = strconv.ParseInt(value("MAX_UPLOAD_BYTES", "50000000"), 10, 64)
	if err != nil || c.MaxUploadBytes <= 0 {
		return c, errors.New("MAX_UPLOAD_BYTES must be positive")
	}
	c.JobTimeout, err = duration("DOWNLOAD_TIMEOUT", "20m")
	if err != nil {
		return c, err
	}
	c.SendTimeout, err = duration("SEND_TIMEOUT", "5m")
	if err != nil {
		return c, err
	}
	c.WebTTL, err = duration("WEB_FILE_TTL", "24h")
	if err != nil {
		return c, err
	}
	if c.WebTTL <= c.JobTimeout {
		return c, errors.New("WEB_FILE_TTL must exceed DOWNLOAD_TIMEOUT")
	}
	c.CleanupInterval, err = duration("CLEANUP_INTERVAL", "1m")
	if err != nil {
		return c, err
	}
	if c.Backend != "yt-dlp" && c.Backend != "gostreampuller" {
		return c, errors.New("VIDEO_BACKEND must be yt-dlp or gostreampuller")
	}
	return c, nil
}
func value(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
func duration(key, fallback string) (time.Duration, error) {
	d, err := time.ParseDuration(value(key, fallback))
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return d, nil
}
