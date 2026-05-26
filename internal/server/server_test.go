package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
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

func TestFileWebhookRequiresFile(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/file", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `{"ok":false,"error":"file_name is required"}`, rec.Body.String())
}

func TestFileWebhookRejectsInvalidBase64File(t *testing.T) {
	handler := New(config.Config{WebhookToken: "secret"}, nil, nil)

	req := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/file",
		strings.NewReader(`{"text":"hello","file_name":"hello.txt","file":"not-base64"}`),
	)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), `"ok":false`)
	require.Contains(t, rec.Body.String(), `"error":`)
}

func TestSupasendWebhookReturnsOKTrueOnSuccess(t *testing.T) {
	createdAt := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
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
		GitHubOwner:  "owner",
		GitHubRepo:   "repo",
		GitHubBranch: "main",
		WebhookToken: "secret",
		NoteDir:      "Inbox/Quick Capture",
	}, githubClient, githubServer.Client())
	handler.now = func() time.Time {
		return createdAt
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/supasend", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok": true}`, rec.Body.String())
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
