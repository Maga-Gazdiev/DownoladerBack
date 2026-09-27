package model

import "time"

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
