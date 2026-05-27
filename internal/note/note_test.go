package note

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderIncludesCreatedAtAndAttachment(t *testing.T) {
	createdAt := time.Date(2026, 5, 26, 22, 35, 43, 0, time.UTC)

	content := string(Render(Capture{
		Text:           "hello",
		CreatedAt:      createdAt,
		FileURL:        "https://example.com/a.png",
		NoteName:       "receipt",
		AttachmentName: "receipt.png",
		AttachmentPath: "Inbox/Quick Capture/receipt/receipt.png",
	}))

	require.Contains(t, content, "created_at: \"2026-05-26 22:35:43\"")
	require.Contains(t, content, "file_url: \"https://example.com/a.png\"")
	require.Contains(t, content, "file_name: \"receipt\"")
	require.Contains(t, content, "attachment_name: \"receipt.png\"")
	require.Contains(t, content, "attachment: \"Inbox/Quick Capture/receipt/receipt.png\"")
	require.Contains(t, content, "hello\n\n![[Inbox/Quick Capture/receipt/receipt.png]]\n")
}

func TestRenderAddsCreatedAtToExistingFrontmatter(t *testing.T) {
	content := string(Render(Capture{
		Text:      "---\ntitle: Existing\n---\nbody",
		CreatedAt: time.Date(2026, 5, 26, 22, 35, 43, 0, time.UTC),
	}))

	require.Contains(t, content, "title: Existing\nsource: \"supasend\"\ncreated_at: \"2026-05-26 22:35:43\"\n---")
	require.Contains(t, content, "\nbody\n")
	require.Equal(t, 2, strings.Count(content, "---\n"))
}

func TestRenderKeepsExistingCreatedAt(t *testing.T) {
	content := string(Render(Capture{
		Text:      "---\ncreated_at: \"already\"\n---\nbody",
		CreatedAt: time.Date(2026, 5, 26, 22, 35, 43, 0, time.UTC),
	}))

	require.Contains(t, content, "created_at: \"already\"")
	require.NotContains(t, content, "created_at: \"2026-05-26 22:35:43\"")
	require.Equal(t, 1, strings.Count(content, "created_at:"))
}

func TestRenderKeepsCreatedAtWallClock(t *testing.T) {
	content := string(Render(Capture{
		Text:      "hello",
		CreatedAt: time.Date(2026, 5, 26, 22, 35, 43, 0, time.FixedZone("EEST", 3*60*60)),
	}))

	require.Contains(t, content, "created_at: \"2026-05-26 22:35:43\"")
}

func TestRenderLinksNonImageAttachment(t *testing.T) {
	content := string(Render(Capture{
		Text:           "hello",
		CreatedAt:      time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
		AttachmentName: "receipt.txt",
		AttachmentPath: "Inbox/Quick Capture/receipt/receipt.txt",
	}))

	require.Contains(t, content, "hello\n\n[[Inbox/Quick Capture/receipt/receipt.txt]]\n")
}
