package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
	"github.com/l-you/supasend-to-github-contents-proxy/internal/filecapture"
	githubapi "github.com/l-you/supasend-to-github-contents-proxy/internal/github"
	"github.com/stretchr/testify/require"
)

func TestSupasendWebhookRejectsInvalidBearer(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/supasend", strings.NewReader(`{"text":"hello"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"unauthorized"}`, rec.Body.String())
}

func TestSupasendWebhookReturnsBadRequestWithOkFalse(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/supasend",
		strings.NewReader(`{"created_at":"2026-05-26T10:00:00Z"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"text is required"}`, rec.Body.String())
}

func TestSupasendWebhookRequiresCreatedAt(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/supasend", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"created_at is required"}`, rec.Body.String())
}

func TestFileWebhookRequiresFolderName(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"created_at":"2026-05-26T10:00:00Z","text":"hello","file_name":"note.md"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(
		t,
		`{"ok":false,"error":`+strconv.Quote(filecapture.MissingFolderNameReason)+`}`,
		rec.Body.String(),
	)
}

func TestFileWebhookRequiresCreatedAt(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"folder_name":"capture","text":"hello","file_name":"note.md"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"created_at is required"}`, rec.Body.String())
}

func TestFileWebhookRejectsInvalidBase64Attachment(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"hello.txt","attachment":"not-base64"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"ok":false`)
	require.Contains(t, rec.Body.String(), `"error":`)
}

func TestFileWebhookRequiresAttachmentNameWhenAttachmentProvided(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"folder_name":"capture","created_at":"2026-05-26T10:00:00Z","attachment":"aGk="}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(
		t,
		`{"ok":false,"error":"attachment_name is required when attachment is provided"}`,
		rec.Body.String(),
	)
}

func TestClientErrorLoggingCanBeEnabled(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousLogOutput)
	})

	handler := New(config.Config{WebhookToken: "secret", LogClientErrors: true}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"folder_name":"capture","text":"hello","file_name":"note.md"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, logs.String(), "request error: status=400 method=POST path=/webhooks/file")
	require.Contains(t, logs.String(), `reason="created_at is required"`)
	require.NotContains(t, logs.String(), "secret")
}

func TestClientErrorLoggingIsDisabledByDefault(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousLogOutput)
	})

	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"folder_name":"capture","text":"hello","file_name":"note.md"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Empty(t, logs.String())
}

func TestCaptureTimeoutIsLogged(t *testing.T) {
	var logs bytes.Buffer
	previousLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() {
		log.SetOutput(previousLogOutput)
	})

	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(githubServer.Close)

	httpClient := &http.Client{Timeout: 10 * time.Millisecond}
	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", httpClient)
	require.NoError(t, err)
	handler := New(config.Config{
		GitHubOwner:     "owner",
		GitHubRepo:      "repo",
		GitHubBranch:    "main",
		WebhookToken:    "secret",
		NoteDir:         "Inbox/Quick Capture",
		LogClientErrors: true,
	}, githubClient, httpClient)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/supasend",
		strings.NewReader(`{"text":"hello","created_at":"2026-05-26T10:00:00Z"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusGatewayTimeout, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"request timed out"}`, rec.Body.String())
	require.Contains(t, logs.String(), "request error: status=504 method=POST path=/webhooks/supasend")
	require.Contains(t, logs.String(), `reason="request timed out"`)
}

func TestUnknownPathReturnsErrorReason(t *testing.T) {
	handler := New(config.Config{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"not found"}`, rec.Body.String())
}

func TestSupasendWebhookReturnsOKTrueOnSuccess(t *testing.T) {
	githubServer := newCaptureGitHubServer(t, captureGitHubServerOptions{})
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/supasend",
		strings.NewReader(`{"text":"hello","created_at":"2026-05-26T10:00:00Z"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
}

func TestFileWebhookWritesNoteIntoRequiredFolder(t *testing.T) {
	var noteContent string
	githubServer := newCaptureGitHubServer(
		t,
		captureGitHubServerOptions{
			onBlob: func(t *testing.T, content []byte) {
				t.Helper()

				noteContent = string(content)
			},
		},
	)
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"run-1","created_at":"2026-05-26T10:00:00Z",`+
				`"text":"hello","file_name":"custom.md"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
	require.Contains(t, noteContent, "file_name: \"custom.md\"")
	require.NotContains(t, noteContent, "attachment_name:")
}

func TestFileWebhookWritesAttachmentOnlyIntoRequiredFolder(t *testing.T) {
	githubServer := newCaptureGitHubServer(
		t,
		captureGitHubServerOptions{
			onTree: func(t *testing.T, paths []string) {
				t.Helper()
				require.Equal(t, []string{"Inbox/Quick Capture/run-1/receipt.txt"}, paths)
			},
		},
	)
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"run-1","created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"receipt.txt","attachment":"aGVsbG8K"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
}

func TestFileWebhookAllowsAttachmentWhenFolderAlreadyHasNote(t *testing.T) {
	githubServer := newCaptureGitHubServer(
		t,
		captureGitHubServerOptions{
			occupied: []string{"Inbox/Quick Capture/run-1/receipt.md"},
			onTree: func(t *testing.T, paths []string) {
				t.Helper()
				require.Equal(t, []string{"Inbox/Quick Capture/run-1/receipt.txt"}, paths)
			},
		},
	)
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"run-1","created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"receipt.txt","attachment":"aGVsbG8K"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
}

func TestFileWebhookWritesNoteAndAttachmentIntoRequiredFolder(t *testing.T) {
	githubServer := newCaptureGitHubServer(
		t,
		captureGitHubServerOptions{
			onTree: func(t *testing.T, paths []string) {
				t.Helper()
				require.Equal(t, []string{
					"Inbox/Quick Capture/run-1/receipt.txt",
					"Inbox/Quick Capture/run-1/receipt.md",
				}, paths)
			},
		},
	)
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"run-1","text":"receipt","file_name":"receipt.md",`+
				`"created_at":"2026-05-26T10:00:00Z",`+
				`"attachment_name":"receipt.txt","attachment":"aGVsbG8K"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
}

func TestFileWebhookReturnsConflictWhenTargetExists(t *testing.T) {
	githubServer := newCaptureGitHubServer(
		t,
		captureGitHubServerOptions{
			occupied: []string{
				"Inbox/Quick Capture/run-1/custom.md",
			},
			onTree: func(t *testing.T, paths []string) {
				t.Helper()
				t.Fatalf("commit should not be created: %v", paths)
			},
		},
	)
	handler := newTestCaptureHandler(t, githubServer)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(
			`{"folder_name":"run-1","created_at":"2026-05-26T10:00:00Z",`+
				`"text":"hello","file_name":"custom.md"}`,
		),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), `"ok":false`)
	require.Contains(t, rec.Body.String(), `"error":`)
}
