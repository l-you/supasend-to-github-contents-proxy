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

	noteName := sanitizeNoteName(payload.FileName)
	attachmentName := sanitizeFilename(payload.AttachmentName)
	attachment := strings.TrimSpace(payload.Attachment)
	var attachmentContent []byte

	switch {
	case attachmentName == "" && attachment != "":
		return captureRequest{}, errors.New("attachment_name is required when attachment is provided")
	case attachmentName != "" && attachment == "":
		return captureRequest{}, errors.New("attachment is required when attachment_name is provided")
	case attachmentName != "" && attachment != "":
		content, err := base64.StdEncoding.DecodeString(attachment)
		if err != nil {
			return captureRequest{}, fmt.Errorf("decode attachment: %w", err)
		}
		attachmentContent = content
	}

	createdAt, err := parseCreatedAt(payload.CreatedAt, fallbackCreatedAt)
	if err != nil {
		return captureRequest{}, err
	}

	return captureRequest{
		Source:                "file",
		Text:                  text,
		NoteName:              noteName,
		AttachmentName:        attachmentName,
		AttachmentContent:     attachmentContent,
		AttachmentContentType: strings.TrimSpace(payload.AttachmentContentType),
		DueDateUTC:            strings.TrimSpace(payload.DueDateUTC),
		CreatedAt:             createdAt,
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

func sanitizeNoteName(value string) string {
	name := sanitizeFilename(value)
	extension := path.Ext(name)
	if strings.EqualFold(extension, ".md") {
		name = strings.TrimSuffix(name, extension)
	}

	return strings.Trim(name, ".-")
}

type filePayload struct {
	Text                  string `json:"text"`
	FileName              string `json:"file_name"`
	AttachmentName        string `json:"attachment_name"`
	Attachment            string `json:"attachment"`
	AttachmentContentType string `json:"attachment_content_type"`
	DueDateUTC            string `json:"due_date_utc"`
	CreatedAt             string `json:"created_at"`
}
