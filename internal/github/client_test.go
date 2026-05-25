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
			writeTestJSON(t, w, refResponseFixture("base"))
		case "GET /repos/owner/repo/git/commits/base":
			writeTestJSON(t, w, commitResponseFixture("base", "tree-base"))
		case "POST /repos/owner/repo/git/blobs":
			var request blobRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			content, err := base64.StdEncoding.DecodeString(request.Content)
			require.NoError(t, err)
			blobContents = append(blobContents, string(content))
			writeTestJSON(t, w, blobResponse{SHA: "blob-" + string(rune('0'+len(blobContents)))})
		case "POST /repos/owner/repo/git/trees":
			var request treeRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			require.Equal(t, "tree-base", request.BaseTree)
			require.Len(t, request.Tree, 2)
			writeTestJSON(t, w, treeResponse{SHA: "tree-new"})
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

	client := NewClient(server.URL, "token", server.Client())

	result, err := client.CommitFiles(t.Context(), "owner", "repo", "main", "Add Supasend capture", []File{
		{Path: "Attachments/Supasend/a.png", Content: []byte("attachment")},
		{Path: "Inbox/Quick Capture/a.md", Content: []byte("note")},
	})

	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "new", result.SHA)
	require.Equal(t, []string{"attachment", "note"}, blobContents)
	require.Equal(t, updateRefRequest{SHA: "new", Force: false}, updatedRef)
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func refResponseFixture(sha string) refResponse {
	var response refResponse
	response.Object.SHA = sha
	return response
}

func commitResponseFixture(sha string, treeSHA string) commitResponse {
	var response commitResponse
	response.SHA = sha
	response.Tree.SHA = treeSHA
	return response
}
