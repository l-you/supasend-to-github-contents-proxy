package github

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitFilesCreatesOneGitCommit(t *testing.T) {
	var blobContents []string
	var updatedRef updateRefRequest

	server := newGitHubTestServer(t, baseGitHubRoutes(githubTestRoutes{
		"POST /repos/owner/repo/git/blobs": func(w http.ResponseWriter, r *http.Request) {
			var request blobRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			content, err := base64.StdEncoding.DecodeString(request.Content)
			require.NoError(t, err)
			blobContents = append(blobContents, string(content))
			writeTestJSON(w, map[string]string{"sha": "blob-" + string(rune('0'+len(blobContents)))})
		},
		"POST /repos/owner/repo/git/trees": func(w http.ResponseWriter, r *http.Request) {
			var request treeRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "tree-base", request.BaseTree)
			require.Len(t, request.Tree, 2)
			writeTestJSON(w, map[string]string{"sha": "tree-new"})
		},
		"POST /repos/owner/repo/git/commits": func(w http.ResponseWriter, r *http.Request) {
			var request createCommitRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "Add Supasend capture", request.Message)
			require.Equal(t, "tree-new", request.Tree)
			require.Equal(t, []string{"base"}, request.Parents)
			writeTestJSON(w, commitResponseFixture("new", "tree-new"))
		},
		"PATCH /repos/owner/repo/git/refs/heads/main": func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updatedRef))
			writeTestJSON(w, map[string]string{"ref": "refs/heads/main"})
		},
	}))
	client := newGitHubTestClient(t, server)

	result, err := client.CommitFiles(t.Context(), "owner", "repo", "main", "Add Supasend capture", []File{
		{Path: "Attachments/Supasend/a.png", Base64Content: base64.StdEncoding.EncodeToString([]byte("attachment"))},
		{Path: "Inbox/Quick Capture/a.md", Content: []byte("note")},
	}, CommitOptions{})

	require.NoError(t, err)
	require.Equal(t, "new", result.SHA)
	require.Equal(t, []string{"attachment", "note"}, blobContents)
	require.Equal(t, updateRefRequest{SHA: "new", Force: false}, updatedRef)
}

func TestUniquePathsIncrementsDuplicateFilenames(t *testing.T) {
	server := newGitHubTestServer(t, baseGitHubRoutes(githubTestRoutes{
		"GET /repos/owner/repo/git/trees/tree-base": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "1", r.URL.Query().Get("recursive"))
			writeTestJSON(w, map[string]any{
				"sha": "tree-base",
				"tree": []map[string]string{
					{"path": "Inbox/Quick Capture/2026-05-26T10-00-00.md", "type": "blob"},
					{"path": "Inbox/Quick Capture/2026-05-26T10-00-00-1.md", "type": "blob"},
				},
			})
		},
	}))
	client := newGitHubTestClient(t, server)

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
	server := newGitHubTestServer(t, baseGitHubRoutes(githubTestRoutes{
		"GET /repos/owner/repo/git/trees/tree-base": func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(w, map[string]any{
				"sha": "tree-base",
				"tree": []map[string]string{
					{"path": "Inbox/receipt/receipt.md", "type": "blob"},
					{"path": "Inbox/receipt-1/receipt-1.md", "type": "blob"},
				},
			})
		},
	}))
	client := newGitHubTestClient(t, server)

	dir, err := client.UniqueDirectory(t.Context(), "owner", "repo", "main", "Inbox/receipt", 5)

	require.NoError(t, err)
	require.Equal(t, "Inbox/receipt-2", dir)
}

func TestCommitFilesRejectExistingReturnsErrorForExistingPath(t *testing.T) {
	server := newGitHubTestServer(t, baseGitHubRoutes(githubTestRoutes{
		"GET /repos/owner/repo/contents/Inbox/run-1/note.md": func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "main", r.URL.Query().Get("ref"))
			writeTestJSON(w, map[string]string{
				"path": "Inbox/run-1/note.md",
				"type": "file",
			})
		},
	}))
	client := newGitHubTestClient(t, server)

	_, err := client.CommitFiles(
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
