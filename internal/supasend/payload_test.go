package supasend

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodePayloadUsesCreatedAt(t *testing.T) {
	payload := strings.NewReader(`{"text":"hello","created_at":"2026-05-26T10:00:00Z"}`)
	fallback := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)

	capture, err := DecodePayload(payload, fallback)

	require.NoError(t, err)
	require.Equal(t, "hello", capture.Text)
	require.Equal(t, time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC), capture.CreatedAt)
}

func TestDecodePayloadFallsBackToReceiveTime(t *testing.T) {
	payload := strings.NewReader(`{"text":"hello"}`)
	fallback := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)

	capture, err := DecodePayload(payload, fallback)

	require.NoError(t, err)
	require.Equal(t, fallback, capture.CreatedAt)
}

func TestDecodePayloadRequiresText(t *testing.T) {
	payload := strings.NewReader(`{"created_at":"2026-05-26T10:00:00Z"}`)

	_, err := DecodePayload(payload, time.Now())

	require.Error(t, err)
}
