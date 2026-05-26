package note

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Capture struct {
	Source                string
	Text                  string
	CreatedAt             time.Time
	DueDateUTC            string
	FileURL               string
	NoteName              string
	AttachmentName        string
	AttachmentPath        string
	AttachmentContentType string
}

func Render(c Capture) []byte {
	var b strings.Builder

	b.WriteString("---\n")
	source := c.Source
	if source == "" {
		source = "supasend"
	}
	writeField(&b, "source", source)
	writeField(&b, "created_at", c.CreatedAt.UTC().Format(time.RFC3339))
	if c.DueDateUTC != "" {
		writeField(&b, "due_date_utc", c.DueDateUTC)
	}
	if c.FileURL != "" {
		writeField(&b, "file_url", c.FileURL)
	}
	if c.NoteName != "" {
		writeField(&b, "file_name", c.NoteName)
	}
	if c.AttachmentName != "" {
		writeField(&b, "attachment_name", c.AttachmentName)
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
