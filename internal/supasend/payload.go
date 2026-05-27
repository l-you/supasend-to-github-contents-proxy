package supasend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type Payload struct {
	Text      string `json:"text"`
	SharedURL string `json:"shared_url"`
	CreatedAt string `json:"created_at"`
}

type Capture struct {
	Text      string
	SharedURL string
	CreatedAt time.Time
}

func DecodePayload(r io.Reader) (Capture, error) {
	var payload Payload
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return Capture{}, fmt.Errorf("decode payload: %w", err)
	}

	text := strings.TrimSpace(payload.Text)
	if text == "" {
		return Capture{}, errors.New("text is required")
	}

	createdAt, err := parseCreatedAt(payload.CreatedAt)
	if err != nil {
		return Capture{}, err
	}

	return Capture{
		Text:      text,
		SharedURL: strings.TrimSpace(payload.SharedURL),
		CreatedAt: createdAt,
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
