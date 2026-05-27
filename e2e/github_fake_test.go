//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	pathpkg "path"
	"strings"
	"sync"
	"testing"
)

type fakeGitHub struct {
	*httptest.Server

	mu          sync.Mutex
	refSHA      string
	blobCount   int
	treeCount   int
	commitCount int
	apiRequests int
	occupied    map[string]struct{}
	commits     map[string]string
	pending     map[string][]string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()

	github := &fakeGitHub{
		refSHA:   "commit-0",
		occupied: make(map[string]struct{}),
		commits: map[string]string{
			"commit-0": "tree-0",
		},
		pending: make(map[string][]string),
	}
	github.Server = httptest.NewServer(http.HandlerFunc(github.handle))

	return github
}

func (g *fakeGitHub) APIRequests() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.apiRequests
}

func (g *fakeGitHub) Commits() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.commitCount
}

func (g *fakeGitHub) Files() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return len(g.occupied)
}

func (g *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.apiRequests++

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/git/ref/heads/main":
		writeJSON(w, map[string]any{"object": map[string]string{"sha": g.refSHA}})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/commits/"):
		g.handleGetCommit(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
		g.handleGetContent(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/trees/"):
		g.handleGetTree(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/blobs":
		g.blobCount++
		writeJSON(w, map[string]string{"sha": fmt.Sprintf("blob-%d", g.blobCount)})
	case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/trees":
		g.handleCreateTree(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/git/commits":
		g.handleCreateCommit(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/git/refs/heads/main":
		g.handleUpdateRef(w, r)
	default:
		http.Error(w, "unexpected GitHub request: "+r.Method+" "+r.URL.RequestURI(), http.StatusNotFound)
	}
}

func (g *fakeGitHub) handleGetCommit(w http.ResponseWriter, r *http.Request) {
	sha := pathpkg.Base(r.URL.Path)
	treeSHA, ok := g.commits[sha]
	if !ok {
		http.Error(w, "commit not found", http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]any{"sha": sha, "tree": map[string]string{"sha": treeSHA}})
}

func (g *fakeGitHub) handleGetContent(w http.ResponseWriter, r *http.Request) {
	contentPath := strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/contents/")
	contentPath, err := url.PathUnescape(contentPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, ok := g.occupied[contentPath]; !ok {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}

	writeJSON(w, map[string]string{
		"path": contentPath,
		"type": "file",
	})
}

func (g *fakeGitHub) handleGetTree(w http.ResponseWriter, r *http.Request) {
	treeSHA := pathpkg.Base(r.URL.Path)
	entries := g.treeEntries(treeDir(treeSHA), r.URL.Query().Get("recursive") == "1")

	writeJSON(w, map[string]any{"sha": treeSHA, "tree": entries})
}

func (g *fakeGitHub) treeEntries(dir string, recursive bool) []map[string]string {
	if recursive {
		entries := make([]map[string]string, 0, len(g.occupied))
		for occupiedPath := range g.occupied {
			entries = append(entries, map[string]string{"path": occupiedPath, "type": "blob"})
		}

		return entries
	}

	seen := make(map[string]struct{})
	entries := make([]map[string]string, 0)
	prefix := ""
	if dir != "" {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}

	for occupiedPath := range g.occupied {
		if prefix != "" && !strings.HasPrefix(occupiedPath, prefix) {
			continue
		}

		rest := strings.TrimPrefix(occupiedPath, prefix)
		name, _, hasChild := strings.Cut(rest, "/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		if hasChild {
			childDir := name
			if dir != "" {
				childDir = dir + "/" + name
			}
			entries = append(entries, map[string]string{
				"path": name,
				"type": "tree",
				"sha":  treeSHA(childDir),
			})
			continue
		}

		entries = append(entries, map[string]string{"path": name, "type": "blob"})
	}

	return entries
}

func treeDir(treeSHA string) string {
	if !strings.HasPrefix(treeSHA, "tree:") {
		return ""
	}

	dir := strings.TrimPrefix(treeSHA, "tree:")
	dir = strings.ReplaceAll(dir, "^", " ")
	return strings.ReplaceAll(dir, "|", "/")
}

func treeSHA(dir string) string {
	dir = strings.ReplaceAll(dir, "/", "|")
	dir = strings.ReplaceAll(dir, " ", "^")
	return "tree:" + dir
}

func (g *fakeGitHub) handleCreateTree(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Tree []struct {
			Path string `json:"path"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	paths := make([]string, 0, len(request.Tree))
	for _, entry := range request.Tree {
		paths = append(paths, entry.Path)
	}

	g.treeCount++
	treeSHA := fmt.Sprintf("tree-%d", g.treeCount)
	g.pending[treeSHA] = paths

	writeJSON(w, map[string]string{"sha": treeSHA})
}

func (g *fakeGitHub) handleCreateCommit(w http.ResponseWriter, r *http.Request) {
	var request map[string]any
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	treeSHA, ok := commitTreeSHA(request["tree"])
	if !ok {
		http.Error(w, "missing commit tree", http.StatusBadRequest)
		return
	}

	g.commitCount++
	commitSHA := fmt.Sprintf("commit-%d", g.commitCount)
	g.commits[commitSHA] = treeSHA
	for _, committedPath := range g.pending[treeSHA] {
		g.occupied[committedPath] = struct{}{}
	}

	writeJSON(w, map[string]any{
		"sha":  commitSHA,
		"tree": map[string]string{"sha": treeSHA},
	})
}

func commitTreeSHA(value any) (string, bool) {
	switch tree := value.(type) {
	case string:
		return tree, tree != ""
	case map[string]any:
		sha, ok := tree["sha"].(string)
		return sha, ok && sha != ""
	default:
		return "", false
	}
}

func (g *fakeGitHub) handleUpdateRef(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g.refSHA = request.SHA

	writeJSON(w, map[string]any{
		"ref":    "refs/heads/main",
		"object": map[string]string{"sha": request.SHA},
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
