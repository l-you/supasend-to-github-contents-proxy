package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitFilesCreatesOneGitCommit(t *testing.T) {
	var blobContents []string
	var updatedRef updateRefRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))

		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, commitResponseFixture("base", "tree-base"))
		case "POST /repos/owner/repo/git/blobs":
			var request blobRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			content, err := base64.StdEncoding.DecodeString(request.Content)
			require.NoError(t, err)
			blobContents = append(blobContents, string(content))
			writeTestJSON(t, w, map[string]string{"sha": "blob-" + string(rune('0'+len(blobContents)))})
		case "POST /repos/owner/repo/git/trees":
			var request treeRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "tree-base", request.BaseTree)
			require.Len(t, request.Tree, 2)
			writeTestJSON(t, w, map[string]string{"sha": "tree-new"})
		case "POST /repos/owner/repo/git/commits":
			var request createCommitRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "Add Supasend capture", request.Message)
			require.Equal(t, "tree-new", request.Tree)
			require.Equal(t, []string{"base"}, request.Parents)
			writeTestJSON(t, w, commitResponseFixture("new", "tree-new"))
		case "PATCH /repos/owner/repo/git/refs/heads/main":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updatedRef))
			writeTestJSON(t, w, map[string]string{"ref": "refs/heads/main"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	require.NoError(t, err)

	result, err := client.CommitFiles(t.Context(), "owner", "repo", "main", "Add Supasend capture", []File{
		{Path: "Attachments/Supasend/a.png", Content: []byte("attachment")},
		{Path: "Inbox/Quick Capture/a.md", Content: []byte("note")},
	}, CommitOptions{})

	require.NoError(t, err)
	require.Equal(t, "new", result.SHA)
	require.Equal(t, []string{"attachment", "note"}, blobContents)
	require.Equal(t, updateRefRequest{SHA: "new", Force: false}, updatedRef)
}

func TestUniquePathsIncrementsDuplicateFilenames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, commitResponseFixture("base", "tree-base"))
		case "GET /repos/owner/repo/git/trees/tree-base":
			require.Equal(t, "1", r.URL.Query().Get("recursive"))
			writeTestJSON(t, w, map[string]any{
				"sha": "tree-base",
				"tree": []map[string]string{
					{"path": "Inbox/Quick Capture/2026-05-26T10-00-00.md", "type": "blob"},
					{"path": "Inbox/Quick Capture/2026-05-26T10-00-00-1.md", "type": "blob"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	require.NoError(t, err)

	paths, err := client.UniquePaths(t.Context(), "owner", "repo", "main", []string{
		"Inbox/Quick Capture/2026-05-26T10-00-00.md",
		"Inbox/Quick Capture/2026-05-26T10-00-00.md",
	}, 5)

	require.NoError(t, err)
	require.Equal(t, []string{
		"Inbox/Quick Capture/2026-05-26T10-00-00-2.md",
		"Inbox/Quick Capture/2026-05-26T10-00-00-3.md",
	}, paths)
}

func TestUniquePathsReturnsErrorAfterMaxSuffix(t *testing.T) {
	occupied := map[string]struct{}{
		"Inbox/note.md":   {},
		"Inbox/note-1.md": {},
		"Inbox/note-2.md": {},
		"Inbox/note-3.md": {},
		"Inbox/note-4.md": {},
		"Inbox/note-5.md": {},
	}

	_, err := nextAvailableFilePath("Inbox/note.md", occupied, 5)

	require.Error(t, err)
	require.True(t, IsPathUnavailable(err))
}

func TestUniqueDirectoryIncrementsDuplicateFolders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, commitResponseFixture("base", "tree-base"))
		case "GET /repos/owner/repo/git/trees/tree-base":
			writeTestJSON(t, w, map[string]any{
				"sha": "tree-base",
				"tree": []map[string]string{
					{"path": "Inbox/receipt/receipt.md", "type": "blob"},
					{"path": "Inbox/receipt-1/receipt-1.md", "type": "blob"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	require.NoError(t, err)

	dir, err := client.UniqueDirectory(t.Context(), "owner", "repo", "main", "Inbox/receipt", 5)

	require.NoError(t, err)
	require.Equal(t, "Inbox/receipt-2", dir)
}

func TestCommitFilesRejectExistingReturnsErrorForExistingPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			writeTestJSON(t, w, map[string]any{"object": map[string]string{"sha": "base"}})
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, commitResponseFixture("base", "tree-base"))
		case "GET /repos/owner/repo/git/trees/tree-base":
			writeTestJSON(t, w, map[string]any{
				"sha": "tree-base",
				"tree": []map[string]string{
					{"path": "Inbox/run-1/note.md", "type": "blob"},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", server.Client())
	require.NoError(t, err)

	_, err = client.CommitFiles(
		t.Context(),
		"owner",
		"repo",
		"main",
		"Add file capture",
		[]File{{Path: "Inbox/run-1/note.md", Content: []byte("note")}},
		CommitOptions{RejectExisting: true},
	)

	require.Error(t, err)
	require.True(t, IsPathExists(err))
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
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
