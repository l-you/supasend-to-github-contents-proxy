package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/supasend"
)

func decodeSupasendCapture(r *http.Request, fallbackCreatedAt time.Time) (captureRequest, error) {
	capture, err := supasend.DecodePayload(r.Body, fallbackCreatedAt)
	if err != nil {
		return captureRequest{}, err
	}

	return captureRequest{
		Source:     "supasend",
		Text:       capture.Text,
		FileURL:    capture.SharedURL,
		DueDateUTC: capture.DueDateUTC,
		CreatedAt:  capture.CreatedAt,
	}, nil
}

func decodeFileCapture(r *http.Request, fallbackCreatedAt time.Time) (captureRequest, error) {
	var payload filePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return captureRequest{}, fmt.Errorf("decode payload: %w", err)
	}

	text := strings.TrimSpace(payload.Text)
	if text == "" {
		return captureRequest{}, errors.New("text is required")
	}

	fileName := sanitizeFilename(payload.FileName)
	if fileName == "" {
		return captureRequest{}, errors.New("file_name is required")
	}

	file := strings.TrimSpace(payload.File)
	if file == "" {
		return captureRequest{}, errors.New("file is required")
	}

	content, err := base64.StdEncoding.DecodeString(file)
	if err != nil {
		return captureRequest{}, fmt.Errorf("decode file: %w", err)
	}

	createdAt, err := parseCreatedAt(payload.CreatedAt, fallbackCreatedAt)
	if err != nil {
		return captureRequest{}, err
	}

	return captureRequest{
		Source:          "file",
		Text:            text,
		FileName:        fileName,
		FileContent:     content,
		FileContentType: strings.TrimSpace(payload.FileContentType),
		DueDateUTC:      strings.TrimSpace(payload.DueDateUTC),
		CreatedAt:       createdAt,
	}, nil
}

func parseCreatedAt(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback.UTC(), nil
	}

	createdAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse created_at: %w", err)
	}

	return createdAt.UTC(), nil
}

func sanitizeFilename(value string) string {
	value = path.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	var b strings.Builder

	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}

	return strings.Trim(b.String(), ".-")
}

type filePayload struct {
	Text            string `json:"text"`
	FileName        string `json:"file_name"`
	File            string `json:"file"`
	FileContentType string `json:"file_content_type"`
	DueDateUTC      string `json:"due_date_utc"`
	CreatedAt       string `json:"created_at"`
}
