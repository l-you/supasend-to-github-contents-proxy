package filecapture

import (
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
	FolderName        string
	Text              string
	NoteFileName      string
	AttachmentName    string
	AttachmentContent []byte
	CreatedAt         string
}

func Decode(r io.Reader, maxAttachmentBytes int64) (Capture, error) {
	var payload Payload
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
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

	var attachmentContent []byte
	switch {
	case attachmentName == "" && attachment != "":
		return Capture{}, errors.New("attachment_name is required when attachment is provided")
	case attachmentName != "" && attachment == "":
		return Capture{}, errors.New("attachment is required when attachment_name is provided")
	case attachmentName != "" && attachment != "":
		if path.Ext(attachmentName) == "" {
			return Capture{}, errors.New("attachment_name must include a file extension")
		}
		content, err := base64.StdEncoding.DecodeString(attachment)
		if err != nil {
			return Capture{}, fmt.Errorf("decode attachment: %w", err)
		}
		if int64(len(content)) > maxAttachmentBytes {
			return Capture{}, fmt.Errorf("attachment exceeds max size of %d bytes", maxAttachmentBytes)
		}
		attachmentContent = content
	}

	return Capture{
		FolderName:        folderName,
		Text:              text,
		NoteFileName:      noteFileName,
		AttachmentName:    attachmentName,
		AttachmentContent: attachmentContent,
		CreatedAt:         createdAt,
	}, nil
}

func sanitizeFilename(value string) string {
	value = path.Base(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"))
	var b strings.Builder

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
