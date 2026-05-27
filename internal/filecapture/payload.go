package filecapture

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
)

const MissingFolderNameReason = "folder_name is required because Obsidian Quick Capture can create " +
	"a note and attachment with the same file name in one folder; use the iOS automation start time " +
	"as folder_name so separate note and attachment uploads can be matched to the same capture"

type Capture struct {
	FolderName       string
	Text             string
	NoteFileName     string
	AttachmentName   string
	AttachmentBase64 string
	CreatedAt        string
}

func Decode(r io.Reader, maxAttachmentBytes int64) (Capture, error) {
	return DecodeWithContentLength(r, maxAttachmentBytes, -1)
}

func DecodeWithContentLength(r io.Reader, maxAttachmentBytes int64, contentLength int64) (Capture, error) {
	body, err := readPayload(r, boundedContentLength(contentLength, maxAttachmentBytes))
	if err != nil {
		return Capture{}, fmt.Errorf("read payload: %w", err)
	}

	var payload Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return Capture{}, fmt.Errorf("decode payload: %w", err)
	}

	folderName := sanitizeFilename(payload.FolderName)
	if folderName == "" {
		return Capture{}, errors.New(MissingFolderNameReason)
	}
	createdAt := strings.TrimSpace(payload.CreatedAt)
	if createdAt == "" {
		return Capture{}, errors.New("created_at is required")
	}

	text := strings.TrimSpace(payload.Text)
	noteFileName := sanitizeFilename(payload.FileName)
	attachmentName := sanitizeFilename(payload.AttachmentName)
	attachment := strings.TrimSpace(payload.Attachment)

	if text == "" && noteFileName != "" {
		return Capture{}, errors.New("text is required when file_name is provided")
	}
	if text == "" && attachment == "" && attachmentName == "" {
		return Capture{}, errors.New("text or attachment is required")
	}
	if text != "" {
		if noteFileName == "" {
			return Capture{}, errors.New("file_name is required when text is provided")
		}
		if !strings.EqualFold(path.Ext(noteFileName), ".md") {
			return Capture{}, errors.New("file_name must end with .md when text is provided")
		}
	}

	switch {
	case attachmentName == "" && attachment != "":
		return Capture{}, errors.New("attachment_name is required when attachment is provided")
	case attachmentName != "" && attachment == "":
		return Capture{}, errors.New("attachment is required when attachment_name is provided")
	case attachmentName != "" && attachment != "":
		if path.Ext(attachmentName) == "" {
			return Capture{}, errors.New("attachment_name must include a file extension")
		}
		if err := validateBase64Attachment(attachment, maxAttachmentBytes); err != nil {
			return Capture{}, err
		}
	}

	return Capture{
		FolderName:       folderName,
		Text:             text,
		NoteFileName:     noteFileName,
		AttachmentName:   attachmentName,
		AttachmentBase64: attachment,
		CreatedAt:        createdAt,
	}, nil
}

func readPayload(r io.Reader, contentLength int64) ([]byte, error) {
	if contentLength <= 0 {
		return io.ReadAll(r)
	}
	if contentLength > int64(int(^uint(0)>>1)) {
		return nil, errors.New("payload is too large")
	}

	var b bytes.Buffer
	b.Grow(int(contentLength))
	_, err := b.ReadFrom(r)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func boundedContentLength(contentLength int64, maxAttachmentBytes int64) int64 {
	if contentLength <= 0 {
		return -1
	}

	maxPayloadBytes := maxPayloadPreallocBytes(maxAttachmentBytes)
	if maxPayloadBytes <= 0 || contentLength > maxPayloadBytes {
		return -1
	}

	return contentLength
}

func maxPayloadPreallocBytes(maxAttachmentBytes int64) int64 {
	const metadataBudget = 1024 * 1024
	if maxAttachmentBytes <= 0 {
		return metadataBudget
	}

	const maxInt64 = int64(^uint64(0) >> 1)
	if maxAttachmentBytes > maxInt64-2 {
		return -1
	}

	encodedBytes := ((maxAttachmentBytes + 2) / 3) * 4
	if encodedBytes > maxInt64-metadataBudget {
		return -1
	}

	return encodedBytes + metadataBudget
}

func validateBase64Attachment(value string, maxBytes int64) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("attachment must be standard base64 without newlines")
	}

	var scratch [32 * 1024]byte
	var decoded int64
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(value))
	for {
		n, err := decoder.Read(scratch[:])
		decoded += int64(n)
		if decoded > maxBytes {
			return fmt.Errorf("attachment exceeds max size of %d bytes", maxBytes)
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}

		return fmt.Errorf("decode attachment: %w", err)
	}
}

func sanitizeFilename(value string) string {
	value = path.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
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

	return strings.Trim(b.String(), ".-")
}

type Payload struct {
	FolderName     string `json:"folder_name"`
	Text           string `json:"text"`
	FileName       string `json:"file_name"`
	AttachmentName string `json:"attachment_name"`
	Attachment     string `json:"attachment"`
	CreatedAt      string `json:"created_at"`
}
