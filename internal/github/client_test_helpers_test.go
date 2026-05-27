package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type githubTestRoutes map[string]http.HandlerFunc

func newGitHubTestServer(t *testing.T, routes githubTestRoutes) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer token")
			http.Error(w, "unexpected authorization", http.StatusUnauthorized)
			return
		}

		key := r.Method + " " + r.URL.Path
		handler, ok := routes[key]
		if !ok {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}

		handler(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func newGitHubTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()

	client, err := NewClient(server.URL, "token", server.Client())
	require.NoError(t, err)

	return client
}

func baseGitHubRoutes(routes githubTestRoutes) githubTestRoutes {
	merged := githubTestRoutes{
		"GET /repos/owner/repo/git/ref/heads/main": func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{"object": map[string]string{"sha": "base"}})
		},
		"GET /repos/owner/repo/git/commits/base": func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, commitResponseFixture("base", "tree-base"))
		},
	}
	for key, handler := range routes {
		merged[key] = handler
	}

	return merged
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func commitResponseFixture(sha string, treeSHA string) map[string]any {
	return map[string]any{
		"sha":  sha,
		"tree": map[string]string{"sha": treeSHA},
	}
}

type blobRequest struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type treeRequest struct {
	BaseTree string          `json:"base_tree"`
	Tree     []treeEntryItem `json:"tree"`
}

type treeEntryItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type updateRefRequest struct {
	SHA   string `json:"sha"`
	Force bool   `json:"force"`
}

type createCommitRequest struct {
	Message string   `json:"message"`
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}
