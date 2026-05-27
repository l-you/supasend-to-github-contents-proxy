package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/filecapture"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/supasend"
)

func decodeSupasendCapture(r *http.Request) (captureRequest, error) {
	capture, err := supasend.DecodePayload(r.Body)
	if err != nil {
		return captureRequest{}, err
	}

	return captureRequest{
		Source:    "supasend",
		Text:      capture.Text,
		FileURL:   capture.SharedURL,
		CreatedAt: capture.CreatedAt,
	}, nil
}

func decodeFileCapture(
	r *http.Request,
	maxAttachmentBytes int64,
) (captureRequest, error) {
	capture, err := filecapture.Decode(r.Body, maxAttachmentBytes)
	if err != nil {
		return captureRequest{}, err
	}
	createdAt, err := parseCreatedAt(capture.CreatedAt)
	if err != nil {
		return captureRequest{}, err
	}

	return captureRequest{
		Source:            "file",
		Text:              capture.Text,
		FolderName:        capture.FolderName,
		NoteFileName:      capture.NoteFileName,
		AttachmentName:    capture.AttachmentName,
		AttachmentContent: capture.AttachmentContent,
		CreatedAt:         createdAt,
	}, nil
}

func parseCreatedAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("created_at is required")
	}

	createdAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse created_at: %w", err)
	}

	return createdAt, nil
}
