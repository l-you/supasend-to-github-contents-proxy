package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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

func TestUnknownPathReturnsErrorReason(t *testing.T) {
	handler := New(config.Config{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"not found"}`, rec.Body.String())
}

func TestSupasendWebhookReturnsOKTrueOnSuccess(t *testing.T) {
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, map[string]any{"sha": "base", "tree": map[string]string{"sha": "tree-base"}})
		case "GET /repos/owner/repo/git/trees/tree-base":
			writeTestJSON(t, w, map[string]any{"sha": "tree-base", "tree": []map[string]string{}})
		case "POST /repos/owner/repo/git/blobs":
			writeTestJSON(t, w, map[string]string{"sha": "blob-note"})
		case "POST /repos/owner/repo/git/trees":
			writeTestJSON(t, w, map[string]string{"sha": "tree-new"})
		case "POST /repos/owner/repo/git/commits":
			writeTestJSON(t, w, map[string]any{"sha": "new", "tree": map[string]string{"sha": "tree-new"}})
		case "PATCH /repos/owner/repo/git/refs/heads/main":
			writeTestJSON(t, w, map[string]string{"ref": "refs/heads/main"})
		default:
			t.Fatalf("unexpected github request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())
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
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, map[string]any{"sha": "base", "tree": map[string]string{"sha": "tree-base"}})
		case "GET /repos/owner/repo/git/trees/tree-base":
			writeTestJSON(t, w, map[string]any{"sha": "tree-base", "tree": []map[string]string{}})
		case "POST /repos/owner/repo/git/blobs":
			var request struct {
				Content string `json:"content"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			content, err := base64.StdEncoding.DecodeString(request.Content)
			require.NoError(t, err)
			noteContent = string(content)
			writeTestJSON(t, w, map[string]string{"sha": "blob-note"})
		case "POST /repos/owner/repo/git/trees":
			writeTestJSON(t, w, map[string]string{"sha": "tree-new"})
		case "POST /repos/owner/repo/git/commits":
			writeTestJSON(t, w, map[string]any{"sha": "new", "tree": map[string]string{"sha": "tree-new"}})
		case "PATCH /repos/owner/repo/git/refs/heads/main":
			writeTestJSON(t, w, map[string]string{"ref": "refs/heads/main"})
		default:
			t.Fatalf("unexpected github request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())
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
		nil,
		func(t *testing.T, paths []string) {
			t.Helper()
			require.Equal(t, []string{"Inbox/Quick Capture/run-1/receipt.txt"}, paths)
		},
	)
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())

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
		[]string{"Inbox/Quick Capture/run-1/receipt.md"},
		func(t *testing.T, paths []string) {
			t.Helper()
			require.Equal(t, []string{"Inbox/Quick Capture/run-1/receipt.txt"}, paths)
		},
	)
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())

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
		nil,
		func(t *testing.T, paths []string) {
			t.Helper()
			require.Equal(t, []string{
				"Inbox/Quick Capture/run-1/receipt.txt",
				"Inbox/Quick Capture/run-1/receipt.md",
			}, paths)
		},
	)
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())

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
		[]string{
			"Inbox/Quick Capture/run-1/custom.md",
		},
		func(t *testing.T, paths []string) {
			t.Helper()
			t.Fatalf("commit should not be created: %v", paths)
		},
	)
	defer githubServer.Close()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	handler := New(config.Config{
		GitHubOwner:  "owner",
		GitHubRepo:   "repo",
		GitHubBranch: "main",
		WebhookToken: "secret",
		NoteDir:      "Inbox/Quick Capture",
	}, githubClient, githubServer.Client())

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

func newCaptureGitHubServer(
	t *testing.T,
	occupied []string,
	onTree func(t *testing.T, paths []string),
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, map[string]any{"sha": "base", "tree": map[string]string{"sha": "tree-base"}})
		case "GET /repos/owner/repo/git/trees/tree-base":
			entries := make([]map[string]string, 0, len(occupied))
			for _, occupiedPath := range occupied {
				entries = append(entries, map[string]string{"path": occupiedPath, "type": "blob"})
			}
			writeTestJSON(t, w, map[string]any{"sha": "tree-base", "tree": entries})
		case "POST /repos/owner/repo/git/blobs":
			writeTestJSON(t, w, map[string]string{"sha": "blob"})
		case "POST /repos/owner/repo/git/trees":
			var request struct {
				Tree []struct {
					Path string `json:"path"`
				} `json:"tree"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))

			paths := make([]string, 0, len(request.Tree))
			for _, entry := range request.Tree {
				paths = append(paths, entry.Path)
			}
			if onTree != nil {
				onTree(t, paths)
			}

			writeTestJSON(t, w, map[string]string{"sha": "tree-new"})
		case "POST /repos/owner/repo/git/commits":
			writeTestJSON(t, w, map[string]any{"sha": "new", "tree": map[string]string{"sha": "tree-new"}})
		case "PATCH /repos/owner/repo/git/refs/heads/main":
			writeTestJSON(t, w, map[string]string{"ref": "refs/heads/main"})
		default:
			t.Fatalf("unexpected github request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
