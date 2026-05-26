package note

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderIncludesCreatedAtAndAttachment(t *testing.T) {
	createdAt := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	content := string(Render(Capture{
		Text:                  "hello",
		CreatedAt:             createdAt,
		DueDateUTC:            "2026-05-27T10:00:00Z",
		FileURL:               "https://example.com/a.png",
		NoteName:              "receipt",
		AttachmentName:        "receipt.png",
		AttachmentPath:        "Inbox/Quick Capture/receipt/receipt.png",
		AttachmentContentType: "image/png",
	}))

	require.Contains(t, content, "created_at: \"2026-05-26T10:00:00Z\"")
	require.Contains(t, content, "due_date_utc: \"2026-05-27T10:00:00Z\"")
	require.Contains(t, content, "file_url: \"https://example.com/a.png\"")
	require.Contains(t, content, "file_name: \"receipt\"")
	require.Contains(t, content, "attachment_name: \"receipt.png\"")
	require.Contains(t, content, "attachment: \"Inbox/Quick Capture/receipt/receipt.png\"")
	require.Contains(t, content, "hello\n\n![[Inbox/Quick Capture/receipt/receipt.png]]\n")
}
