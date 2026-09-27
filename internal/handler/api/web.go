package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
	service "video-downloader/internal/service/web"
)

type WebService interface {
	Create(context.Context, string) (model.WebJob, error)
	Get(context.Context, string) (model.WebJob, error)
	Open(context.Context, string) (io.ReadSeekCloser, model.WebJob, error)
}

func NewWebAPI(s WebService, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/downloads", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			URL string `json:"url"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		if err := decoder.Decode(&request); err != nil {
			webError(w, 400, "Некорректный запрос")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			webError(w, 400, "Некорректный запрос")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		job, err := s.Create(ctx, request.URL)
		if errors.Is(err, apperrors.ErrUnsupportedURL) {
			webError(w, 400, "Нужна ссылка на YouTube, Instagram или TikTok")
			return
		}
		if err != nil {
			webError(w, 503, "Сервис занят. Попробуйте позже.")
			return
		}
		webJSON(w, 202, job)
	})
	mux.HandleFunc("GET /api/downloads/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, err := s.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			webJobError(w, err)
			return
		}
		webJSON(w, 200, job)
	})
	mux.HandleFunc("GET /api/downloads/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		f, job, err := s.Open(r.Context(), r.PathValue("id"))
		if err != nil {
			webJobError(w, err)
			return
		}
		defer f.Close()
		// Large downloads must not inherit the short JSON response deadline.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": job.File.Name}))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, job.File.Name, job.CreatedAt, f)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			webError(w, 401, "Требуется ключ доступа")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func webJobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrWebExpired):
		webError(w, 410, "Срок доступности видео истёк. Скачайте видео заново.")
	case errors.Is(err, service.ErrWebNotReady):
		webError(w, 409, "Файл ещё не готов")
	case errors.Is(err, os.ErrNotExist), apperrors.IsPermanent(err):
		webError(w, 404, "Задача не найдена")
	default:
		webError(w, 500, "Не удалось прочитать задачу")
	}
}
func webError(w http.ResponseWriter, status int, message string) {
	webJSON(w, status, map[string]string{"error": message})
}
func webJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
