package supasend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode"
)

type Attachment struct {
	Content  []byte
	FileName string
}

func DownloadAttachment(
	ctx context.Context,
	client *http.Client,
	rawURL string,
	maxBytes int64,
) (Attachment, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Attachment{}, fmt.Errorf("create attachment request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Attachment{}, fmt.Errorf("download attachment: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Attachment{}, fmt.Errorf("download attachment: unexpected status %s", resp.Status)
	}

	content, err := readLimited(resp.Body, maxBytes)
	if err != nil {
		return Attachment{}, err
	}

	filename := filenameFromResponse(rawURL, resp.Header)

	return Attachment{
		Content:  content,
		FileName: filename,
	}, nil
}

func readLimited(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("max attachment size must be greater than zero")
	}

	content, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read attachment: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("attachment exceeds max size of %d bytes", maxBytes)
	}

	return content, nil
}

func filenameFromResponse(rawURL string, header http.Header) string {
	if value := header.Get("Content-Disposition"); value != "" {
		_, params, err := mime.ParseMediaType(value)
		if err == nil && params["filename"] != "" {
			return sanitizeFilename(params["filename"])
		}
	}

	parsed, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(parsed.Path)
		if base != "." && base != "/" && base != "" {
			return sanitizeFilename(base)
		}
	}

	return "attachment"
}

func sanitizeFilename(value string) string {
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	var b strings.Builder
	b.Grow(len(value))

	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune('-')
		default:
			b.WriteRune('-')
		}
	}

	result := strings.Trim(b.String(), ".-")
	if result == "" {
		return "attachment"
	}

	return result
}
