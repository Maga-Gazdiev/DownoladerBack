package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	apperrors "video-downloader/internal/errors"
)

type Submitter interface {
	Submit(context.Context, int64, int64, string) error
}
type Webhook struct {
	service Submitter
	secret  string
}

func NewWebhook(service Submitter, secret string) *Webhook {
	return &Webhook{service: service, secret: secret}
}

type entity struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}
type message struct {
	Text            string   `json:"text"`
	Caption         string   `json:"caption"`
	Entities        []entity `json:"entities"`
	CaptionEntities []entity `json:"caption_entities"`
	Chat            struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}
type update struct {
	ID      *int64   `json:"update_id"`
	Message *message `json:"message"`
}

func (h *Webhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(h.secret)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	defer r.Body.Close()
	u, err := decodeUpdate(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if u.Message == nil {
		w.WriteHeader(200)
		return
	}
	m := u.Message
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	err = h.service.Submit(ctx, *u.ID, m.Chat.ID, m.linkText())
	if errors.Is(err, apperrors.ErrUnsupportedURL) || apperrors.IsPermanent(err) {
		w.WriteHeader(200)
		return
	}
	if err != nil {
		http.Error(w, "service busy", 503)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func decodeUpdate(body io.Reader) (update, error) {
	var u update
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&u); err != nil {
		return u, errors.New("invalid JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF || u.ID == nil {
		return u, errors.New("invalid update")
	}
	return u, nil
}

func (m *message) linkText() string {
	return strings.Join([]string{
		m.Text, m.Caption,
		entityURLs(m.Text, m.Entities),
		entityURLs(m.Caption, m.CaptionEntities),
	}, " ")
}

func entityURLs(text string, entities []entity) string {
	var urls []string
	units := utf16.Encode([]rune(text))
	for _, e := range entities {
		if e.Type == "text_link" {
			urls = append(urls, e.URL)
		}
		if e.Type == "url" && e.Offset >= 0 && e.Length > 0 && e.Offset <= len(units) && e.Length <= len(units)-e.Offset {
			urls = append(urls, string(utf16.Decode(units[e.Offset:e.Offset+e.Length])))
		}
	}
	return strings.Join(urls, " ")
}
