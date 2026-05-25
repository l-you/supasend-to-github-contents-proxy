package note

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

type Capture struct {
	Text                  string
	CreatedAt             time.Time
	DueDateUTC            string
	SharedURL             string
	AttachmentPath        string
	AttachmentContentType string
}

func Path(dir string, createdAt time.Time) string {
	return path.Join(dir, createdAt.UTC().Format("2006-01-02T15-04-05")+".md")
}

func Render(c Capture) []byte {
	var b strings.Builder

	b.WriteString("---\n")
	writeField(&b, "source", "supasend")
	writeField(&b, "created_at", c.CreatedAt.UTC().Format(time.RFC3339))
	if c.DueDateUTC != "" {
		writeField(&b, "due_date_utc", c.DueDateUTC)
	}
	if c.SharedURL != "" {
		writeField(&b, "shared_url", c.SharedURL)
	}
	if c.AttachmentPath != "" {
		writeField(&b, "attachment", c.AttachmentPath)
	}
	b.WriteString("---\n\n")

	b.WriteString(strings.TrimRight(c.Text, "\n"))
	b.WriteString("\n")

	if c.AttachmentPath != "" {
		b.WriteString("\n")
		if strings.HasPrefix(c.AttachmentContentType, "image/") {
			fmt.Fprintf(&b, "![[%s]]\n", c.AttachmentPath)
		} else {
			fmt.Fprintf(&b, "[[%s]]\n", c.AttachmentPath)
		}
	}

	return []byte(b.String())
}

func writeField(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "%s: %s\n", key, strconv.Quote(value))
}
