package supasend

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodePayloadUsesCreatedAt(t *testing.T) {
	payload := strings.NewReader(`{"text":"hello","created_at":"2026-05-26T10:00:00Z"}`)

	capture, err := DecodePayload(payload)

	require.NoError(t, err)
	require.Equal(t, "hello", capture.Text)
	require.Equal(t, time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC), capture.CreatedAt)
}

func TestDecodePayloadKeepsCreatedAtOffset(t *testing.T) {
	payload := strings.NewReader(`{"text":"hello","created_at":"2026-05-26T22:35:43+03:00"}`)

	capture, err := DecodePayload(payload)

	require.NoError(t, err)
	require.Equal(t, "2026-05-26T22:35:43+03:00", capture.CreatedAt.Format(time.RFC3339))
}

func TestDecodePayloadRequiresCreatedAt(t *testing.T) {
	payload := strings.NewReader(`{"text":"hello"}`)

	_, err := DecodePayload(payload)

	require.EqualError(t, err, "created_at is required")
}

func TestDecodePayloadRequiresText(t *testing.T) {
	payload := strings.NewReader(`{"created_at":"2026-05-26T10:00:00Z"}`)

	_, err := DecodePayload(payload)

	require.Error(t, err)
}
