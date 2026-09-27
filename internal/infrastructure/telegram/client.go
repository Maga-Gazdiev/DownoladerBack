package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"strconv"
	"strings"

	apperrors "video-downloader/internal/errors"
	"video-downloader/internal/model"
)

type Client struct {
	token, baseURL string
	http           *http.Client
	maxBytes       int64
}

func New(token, baseURL string, client *http.Client, maxBytes int64) *Client {
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: client, maxBytes: maxBytes}
}
func (c *Client) SendVideo(ctx context.Context, chatID int64, media *model.Media) error {
	if media.Size() == 0 || media.Size() > c.maxBytes {
		return apperrors.Permanent(errors.New("invalid video size"))
	}
	f, err := media.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	// Stream multipart framing around the existing buffer without a second copy.
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	defer reader.Close()
	method, field := "sendVideo", "video"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, reader)
	if err != nil {
		return errors.New("invalid Telegram API URL")
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	done := make(chan error, 1)
	go func() {
		err := multipartWriter.WriteField("chat_id", strconv.FormatInt(chatID, 10))
		if err == nil && method == "sendVideo" {
			err = multipartWriter.WriteField("supports_streaming", "true")
		}
		if err == nil {
			var part io.Writer
			part, err = multipartWriter.CreateFormFile(field, media.File.Name)
			if err == nil {
				_, err = io.Copy(part, f)
			}
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = writer.CloseWithError(err)
		done <- err
	}()
	resp, requestErr := c.http.Do(req)
	// Early responses/cancellation must release a blocked upload goroutine.
	_ = reader.Close()
	uploadErr := <-done
	if requestErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// net/http errors contain the request URL and therefore the bot token.
		return errors.New("Telegram transport failed")
	}
	defer resp.Body.Close()
	var result struct {
		OK   bool `json:"ok"`
		Code int  `json:"error_code"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
	if decodeErr == nil && resp.StatusCode == http.StatusOK && result.OK {
		return nil
	}
	code := result.Code
	if code == 0 {
		code = resp.StatusCode
	}
	err = fmt.Errorf("Telegram API failed (status %d)", code)
	if code >= 400 && code < 500 && code != 429 && code != 408 {
		return apperrors.Permanent(err)
	}
	return errors.Join(err, uploadErr)
}
