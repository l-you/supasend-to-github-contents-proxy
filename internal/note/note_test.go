package note

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPathUsesCreatedAt(t *testing.T) {
	createdAt := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	require.Equal(t, "Inbox/Quick Capture/2026-05-26T10-00-00.md", Path("Inbox/Quick Capture", createdAt))
}

func TestRenderIncludesCreatedAtAndAttachment(t *testing.T) {
	createdAt := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	content := string(Render(Capture{
		Text:                  "hello",
		CreatedAt:             createdAt,
		DueDateUTC:            "2026-05-27T10:00:00Z",
		SharedURL:             "https://example.com/a.png",
		AttachmentPath:        "Attachments/Supasend/a.png",
		AttachmentContentType: "image/png",
	}))

	require.Contains(t, content, "created_at: \"2026-05-26T10:00:00Z\"")
	require.Contains(t, content, "due_date_utc: \"2026-05-27T10:00:00Z\"")
	require.Contains(t, content, "attachment: \"Attachments/Supasend/a.png\"")
	require.Contains(t, content, "hello\n\n![[Attachments/Supasend/a.png]]\n")
}
