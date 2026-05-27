package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/l-you/supasend-to-github-contents-proxy/internal/config"
	githubapi "github.com/l-you/supasend-to-github-contents-proxy/internal/github"
	"github.com/stretchr/testify/require"
)

type captureGitHubServerOptions struct {
	occupied []string
	onTree   func(t *testing.T, paths []string)
	onBlob   func(t *testing.T, content []byte)
}

func newTestCaptureHandler(t *testing.T, githubServer *httptest.Server) *Server {
	t.Helper()

	githubClient, err := githubapi.NewClient(githubServer.URL, "gh-token", githubServer.Client())
	require.NoError(t, err)

	return New(config.Config{
		GitHubOwner:       "owner",
		GitHubRepo:        "repo",
		GitHubBranch:      "main",
		WebhookToken:      "secret",
		NoteDir:           "Inbox/Quick Capture",
		MaxAttachmentSize: 25 * 1024 * 1024,
	}, githubClient, githubServer.Client())
}

func newCaptureGitHubServer(t *testing.T, options captureGitHubServerOptions) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestKey := r.Method + " " + r.URL.Path
		switch {
		case requestKey == "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(w, map[string]any{"object": map[string]string{"sha": "base"}})
		case requestKey == "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(w, map[string]any{"sha": "base", "tree": map[string]string{"sha": "tree-base"}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/trees/"):
			treeSHA := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/git/trees/")
			writeTestJSON(w, map[string]any{
				"sha":  treeSHA,
				"tree": testTreeEntries(options.occupied, r.URL.Query().Get("recursive") == "1"),
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
			contentPath := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
			unescapedPath, err := url.PathUnescape(contentPath)
			require.NoError(t, err)
			require.Equal(t, "main", r.URL.Query().Get("ref"))
			if pathIsOccupied(unescapedPath, options.occupied) {
				writeTestJSON(w, map[string]string{
					"path": unescapedPath,
					"type": "file",
				})
				return
			}

			writeTestJSONStatus(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		case requestKey == "POST /repos/owner/repo/git/blobs":
			handleCreateBlob(t, w, r, options.onBlob)
		case requestKey == "POST /repos/owner/repo/git/trees":
			handleCreateTree(t, w, r, options.onTree)
		case requestKey == "POST /repos/owner/repo/git/commits":
			writeTestJSON(w, map[string]any{"sha": "new", "tree": map[string]string{"sha": "tree-new"}})
		case requestKey == "PATCH /repos/owner/repo/git/refs/heads/main":
			writeTestJSON(w, map[string]string{"ref": "refs/heads/main"})
		default:
			t.Errorf("unexpected github request: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "unexpected github request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func handleCreateBlob(
	t *testing.T,
	w http.ResponseWriter,
	r *http.Request,
	onBlob func(t *testing.T, content []byte),
) {
	t.Helper()

	var request struct {
		Content string `json:"content"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
	content, err := base64.StdEncoding.DecodeString(request.Content)
	require.NoError(t, err)
	if onBlob != nil {
		onBlob(t, content)
	}

	writeTestJSON(w, map[string]string{"sha": "blob"})
}

func handleCreateTree(
	t *testing.T,
	w http.ResponseWriter,
	r *http.Request,
	onTree func(t *testing.T, paths []string),
) {
	t.Helper()

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

	writeTestJSON(w, map[string]string{"sha": "tree-new"})
}

func pathIsOccupied(path string, occupied []string) bool {
	for _, occupiedPath := range occupied {
		if occupiedPath == path {
			return true
		}
	}

	return false
}

func testTreeEntries(occupied []string, recursive bool) []map[string]string {
	if recursive {
		entries := make([]map[string]string, 0, len(occupied))
		for _, occupiedPath := range occupied {
			entries = append(entries, map[string]string{"path": occupiedPath, "type": "blob"})
		}

		return entries
	}

	return nil
}

func writeTestJSON(w http.ResponseWriter, value any) {
	writeTestJSONStatus(w, http.StatusOK, value)
}

func writeTestJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
