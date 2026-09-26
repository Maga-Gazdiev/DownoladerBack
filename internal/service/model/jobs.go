package model

import "time"

const (
	DownloadQueue = "video.download"
	SendQueue     = "telegram.send"
	WebQueue      = "web.download"
)

type DownloadJob struct {
	ID     string `json:"id"`
	ChatID int64  `json:"chat_id"`
	URL    string `json:"url"`
}

type SendJob struct {
	ID     string `json:"id"`
	ChatID int64  `json:"chat_id"`
	File   File   `json:"file"`
}

type WebJob struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Platform  Platform  `json:"platform"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	File      File      `json:"file"`
	Size      int64     `json:"size,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
