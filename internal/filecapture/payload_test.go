package filecapture

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeRequiresFolderName(t *testing.T) {
	_, err := Decode(
		strings.NewReader(`{"created_at":"2026-05-26T10:00:00Z","text":"hello","file_name":"note.md"}`),
		1024,
	)

	require.EqualError(t, err, MissingFolderNameReason)
}

func TestDecodeRequiresCreatedAt(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"folder_name":"capture","text":"hello","file_name":"note.md"}`), 1024)

	require.EqualError(t, err, "created_at is required")
}

func TestDecodeAcceptsNotePayload(t *testing.T) {
	capture, err := Decode(
		strings.NewReader(
			`{"folder_name":"2026-05-27T10-00-00","created_at":"2026-05-26T10:00:00Z",`+
				`"text":"hello","file_name":"note.md"}`,
		),
		1024,
	)

	require.NoError(t, err)
	require.Equal(t, "2026-05-27T10-00-00", capture.FolderName)
	require.Equal(t, "hello", capture.Text)
	require.Equal(t, "note.md", capture.NoteFileName)
}

func TestDecodeAcceptsAttachmentPayload(t *testing.T) {
	capture, err := Decode(
		strings.NewReader(
			`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"receipt.jpg","attachment":"aGk="}`,
		),
		1024,
	)

	require.NoError(t, err)
	require.Equal(t, "capture", capture.FolderName)
	require.Equal(t, "receipt.jpg", capture.AttachmentName)
	require.Equal(t, []byte("hi"), capture.AttachmentContent)
}

func TestDecodeRejectsMissingContent(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z"}`), 1024)

	require.EqualError(t, err, "text or attachment is required")
}

func TestDecodeRejectsFileNameWithoutText(t *testing.T) {
	_, err := Decode(
		strings.NewReader(`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z","file_name":"note.md"}`),
		1024,
	)

	require.EqualError(t, err, "text is required when file_name is provided")
}

func TestDecodeRequiresMarkdownNoteFilename(t *testing.T) {
	_, err := Decode(
		strings.NewReader(
			`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z","text":"hello","file_name":"note"}`,
		),
		1024,
	)

	require.EqualError(t, err, "file_name must end with .md when text is provided")
}

func TestDecodeRequiresAttachmentFilenameExtension(t *testing.T) {
	_, err := Decode(
		strings.NewReader(
			`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"receipt","attachment":"aGk="}`,
		),
		1024,
	)

	require.EqualError(t, err, "attachment_name must include a file extension")
}
