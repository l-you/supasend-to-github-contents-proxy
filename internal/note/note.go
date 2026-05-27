package note

import (
	"bytes"
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
	var b bytes.Buffer
	text := strings.TrimRight(c.Text, "\n")

	b.Grow(estimatedSize(c, len(text)))
	writeTextWithFrontmatter(&b, c, text)
	b.WriteString("\n")

	if c.AttachmentPath != "" {
		b.WriteString("\n")
		if isImageAttachment(c.AttachmentPath) {
			b.WriteString("![[")
		} else {
			b.WriteString("[[")
		}
		b.WriteString(c.AttachmentPath)
		b.WriteString("]]\n")
	}

	return b.Bytes()
}

func writeTextWithFrontmatter(b *bytes.Buffer, c Capture, text string) {
	frontmatterStart := frontmatterStartLen(text)
	if frontmatterStart == 0 {
		writeNewFrontmatter(b, c)
		b.WriteString(text)
		return
	}

	endStart, _, ok := findFrontmatterEnd(text, frontmatterStart)
	if !ok {
		writeNewFrontmatter(b, c)
		b.WriteString(text)
		return
	}

	b.WriteString(text[:endStart])
	writeMissingFrontmatterFields(b, c, text[frontmatterStart:endStart])
	b.WriteString(text[endStart:])
}

func writeNewFrontmatter(b *bytes.Buffer, c Capture) {
	b.WriteString("---\n")
	writeFrontmatterFields(b, c)
	b.WriteString("---\n\n")
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

func writeFrontmatterFields(b *bytes.Buffer, c Capture) {
	writeField(b, "source", sourceName(c.Source))
	writeField(b, "created_at", c.CreatedAt.Format(createdAtLayout))
	writeOptionalField(b, "file_url", c.FileURL)
	writeOptionalField(b, "file_name", c.NoteName)
	writeOptionalField(b, "attachment_name", c.AttachmentName)
	writeOptionalField(b, "attachment", c.AttachmentPath)
}

func writeMissingFrontmatterFields(b *bytes.Buffer, c Capture, frontmatter string) {
	writeFieldIfMissing(b, frontmatter, "source", sourceName(c.Source))
	writeFieldIfMissing(b, frontmatter, "created_at", c.CreatedAt.Format(createdAtLayout))
	writeOptionalFieldIfMissing(b, frontmatter, "file_url", c.FileURL)
	writeOptionalFieldIfMissing(b, frontmatter, "file_name", c.NoteName)
	writeOptionalFieldIfMissing(b, frontmatter, "attachment_name", c.AttachmentName)
	writeOptionalFieldIfMissing(b, frontmatter, "attachment", c.AttachmentPath)
}

func sourceName(source string) string {
	if source == "" {
		return "supasend"
	}

	return source
}

func writeField(b *bytes.Buffer, key string, value string) {
	var quoted [256]byte

	b.WriteString(key)
	b.WriteString(": ")
	_, _ = b.Write(strconv.AppendQuote(quoted[:0], value))
	b.WriteByte('\n')
}

func writeOptionalField(b *bytes.Buffer, key string, value string) {
	if value != "" {
		writeField(b, key, value)
	}
}

func writeFieldIfMissing(b *bytes.Buffer, frontmatter string, key string, value string) {
	if !frontmatterHasKey(frontmatter, key) {
		writeField(b, key, value)
	}
}

func writeOptionalFieldIfMissing(b *bytes.Buffer, frontmatter string, key string, value string) {
	if value != "" {
		writeFieldIfMissing(b, frontmatter, key, value)
	}
}

func frontmatterHasKey(frontmatter string, key string) bool {
	for offset := 0; offset < len(frontmatter); {
		lineStart := offset
		newline := strings.IndexByte(frontmatter[offset:], '\n')
		lineEnd := len(frontmatter)
		next := len(frontmatter)
		if newline >= 0 {
			lineEnd = offset + newline
			next = lineEnd + 1
		}

		line := strings.TrimSuffix(frontmatter[lineStart:lineEnd], "\r")
		if lineHasKey(line, key) {
			return true
		}
		offset = next
	}

	return false
}

func lineHasKey(line string, expected string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "-") {
		return false
	}
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
		return false
	}

	key, _, ok := strings.Cut(line, ":")
	if !ok {
		return false
	}
	key = strings.Trim(strings.TrimSpace(key), `"'`)

	return key == expected
}

func estimatedSize(c Capture, textSize int) int {
	size := textSize + 80 + len(c.Source) + len(c.FileURL) + len(c.NoteName) +
		len(c.AttachmentName) + len(c.AttachmentPath)
	if c.AttachmentPath != "" {
		size += len(c.AttachmentPath) + 8
	}

	return size
}

func isImageAttachment(attachmentPath string) bool {
	switch strings.ToLower(path.Ext(attachmentPath)) {
	case ".avif", ".bmp", ".gif", ".heic", ".heif", ".jpeg", ".jpg", ".png", ".svg", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
}
