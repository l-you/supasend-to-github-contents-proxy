package note

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

const createdAtLayout = "2006-01-02 15:04:05"

type Capture struct {
	Source         string
	Text           string
	CreatedAt      time.Time
	FileURL        string
	NoteName       string
	AttachmentName string
	AttachmentPath string
}

func Render(c Capture) []byte {
	var b strings.Builder

	fields := frontmatterFields(c)
	b.WriteString(renderWithFrontmatter(strings.TrimRight(c.Text, "\n"), fields))
	b.WriteString("\n")

	if c.AttachmentPath != "" {
		b.WriteString("\n")
		if isImageAttachment(c.AttachmentPath) {
			fmt.Fprintf(&b, "![[%s]]\n", c.AttachmentPath)
		} else {
			fmt.Fprintf(&b, "[[%s]]\n", c.AttachmentPath)
		}
	}

	return []byte(b.String())
}

type frontmatterField struct {
	key   string
	value string
}

func frontmatterFields(c Capture) []frontmatterField {
	source := c.Source
	if source == "" {
		source = "supasend"
	}

	fields := []frontmatterField{
		{key: "source", value: source},
		{key: "created_at", value: c.CreatedAt.Format(createdAtLayout)},
	}
	if c.FileURL != "" {
		fields = append(fields, frontmatterField{key: "file_url", value: c.FileURL})
	}
	if c.NoteName != "" {
		fields = append(fields, frontmatterField{key: "file_name", value: c.NoteName})
	}
	if c.AttachmentName != "" {
		fields = append(fields, frontmatterField{key: "attachment_name", value: c.AttachmentName})
	}
	if c.AttachmentPath != "" {
		fields = append(fields, frontmatterField{key: "attachment", value: c.AttachmentPath})
	}

	return fields
}

func renderWithFrontmatter(text string, fields []frontmatterField) string {
	frontmatterStart := frontmatterStartLen(text)
	if frontmatterStart == 0 {
		return newFrontmatter(fields) + text
	}

	endStart, _, ok := findFrontmatterEnd(text, frontmatterStart)
	if !ok {
		return newFrontmatter(fields) + text
	}

	existing := frontmatterKeys(text[frontmatterStart:endStart])
	missing := missingFields(fields, existing)
	if len(missing) == 0 {
		return text
	}

	var b strings.Builder
	b.WriteString(text[:endStart])
	writeFields(&b, missing)
	b.WriteString(text[endStart:])

	return b.String()
}

func newFrontmatter(fields []frontmatterField) string {
	var b strings.Builder
	b.WriteString("---\n")
	writeFields(&b, fields)
	b.WriteString("---\n\n")

	return b.String()
}

func frontmatterStartLen(text string) int {
	switch {
	case strings.HasPrefix(text, "---\r\n"):
		return len("---\r\n")
	case strings.HasPrefix(text, "---\n"):
		return len("---\n")
	default:
		return 0
	}
}

func findFrontmatterEnd(text string, start int) (int, int, bool) {
	for offset := start; offset < len(text); {
		lineStart := offset
		newline := strings.IndexByte(text[offset:], '\n')
		lineEnd := len(text)
		next := len(text)
		if newline >= 0 {
			lineEnd = offset + newline
			next = lineEnd + 1
		}

		line := strings.TrimSuffix(text[lineStart:lineEnd], "\r")
		if strings.TrimSpace(line) == "---" {
			return lineStart, next, true
		}
		offset = next
	}

	return 0, 0, false
}

func frontmatterKeys(frontmatter string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		key, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		if key != "" {
			keys[key] = struct{}{}
		}
	}

	return keys
}

func missingFields(fields []frontmatterField, existing map[string]struct{}) []frontmatterField {
	missing := make([]frontmatterField, 0, len(fields))
	for _, field := range fields {
		if _, ok := existing[field.key]; !ok {
			missing = append(missing, field)
		}
	}

	return missing
}

func writeFields(b *strings.Builder, fields []frontmatterField) {
	for _, field := range fields {
		writeField(b, field.key, field.value)
	}
}

func writeField(b *strings.Builder, key string, value string) {
	fmt.Fprintf(b, "%s: %s\n", key, strconv.Quote(value))
}

func isImageAttachment(attachmentPath string) bool {
	switch strings.ToLower(path.Ext(attachmentPath)) {
	case ".avif", ".bmp", ".gif", ".heic", ".heif", ".jpeg", ".jpg", ".png", ".svg", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
}
