//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerMemoryCaptureE2E(t *testing.T) {
	repoRoot := repoRoot(t)
	binaryPath := filepath.Join(t.TempDir(), "supasend-to-github")
	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/server")
	build.Dir = repoRoot
	buildOutput, err := build.CombinedOutput()
	require.NoErrorf(t, err, "build server: %s", buildOutput)

	github := newFakeGitHub(t)
	defer github.Close()

	listenAddr := freeTCPAddr(t)
	debugAddr := freeTCPAddr(t)
	logs := &lockedBuffer{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Dir = repoRoot
	cmd.Stdout = logs
	cmd.Stderr = logs
	cmd.Env = append(os.Environ(),
		"GITHUB_TOKEN=e2e-token",
		"GITHUB_OWNER=owner",
		"GITHUB_REPO=repo",
		"GITHUB_BRANCH=main",
		"GITHUB_API_URL="+github.URL,
		"WEBHOOK_TOKEN=secret",
		"LISTEN_ADDR="+listenAddr,
		"DEBUG_LISTEN_ADDR="+debugAddr,
		"NOTE_DIR=Inbox/Quick Capture",
		"MAX_ATTACHMENT_BYTES=10485760",
	)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		cancel()
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	waitForHTTP(t, "http://"+listenAddr+"/healthz", logs)
	waitForHTTP(t, "http://"+debugAddr+"/debug/vars", logs)

	client := &http.Client{Timeout: 10 * time.Second}
	requestCount := envInt("E2E_CAPTURE_COUNT", 50)
	attachmentBytes := envInt("E2E_ATTACHMENT_BYTES", 4096)

	forceGC(t, client, debugAddr)
	before := readMemStats(t, client, debugAddr)
	started := time.Now()
	postCaptures(t, client, listenAddr, requestCount, attachmentBytes)
	elapsed := time.Since(started)
	forceGC(t, client, debugAddr)
	after := readMemStats(t, client, debugAddr)

	totalAllocDelta := after.TotalAlloc - before.TotalAlloc
	mallocsDelta := after.Mallocs - before.Mallocs
	apiRequests := github.APIRequests()

	t.Logf(
		"memory: captures=%d attachment_bytes=%d elapsed=%s avg=%s "+
			"total_alloc_delta=%s total_alloc_per_capture=%s mallocs_delta=%d "+
			"heap_alloc_delta=%s heap_objects_delta=%d sys_delta=%s gc_delta=%d",
		requestCount,
		attachmentBytes,
		elapsed.Round(time.Millisecond),
		(elapsed / time.Duration(requestCount)).Round(time.Microsecond),
		formatBytes(totalAllocDelta),
		formatBytes(totalAllocDelta/uint64(requestCount)),
		mallocsDelta,
		formatBytesDelta(after.HeapAlloc, before.HeapAlloc),
		int64(after.HeapObjects)-int64(before.HeapObjects),
		formatBytesDelta(after.Sys, before.Sys),
		after.NumGC-before.NumGC,
	)
	t.Logf(
		"github_api: requests=%d per_capture=%.2f commits=%d files=%d",
		apiRequests,
		float64(apiRequests)/float64(requestCount),
		github.Commits(),
		github.Files(),
	)

	require.Equal(t, requestCount, github.Commits())
	require.Equal(t, requestCount*2, github.Files())
}

func postCaptures(t *testing.T, client *http.Client, addr string, count int, attachmentBytes int) {
	t.Helper()

	attachment := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), attachmentBytes))
	baseTime := time.Date(2026, 5, 26, 22, 35, 43, 0, time.FixedZone("EEST", 3*60*60))

	for i := range count {
		payload := map[string]string{
			"folder_name":     fmt.Sprintf("e2e-%03d", i),
			"created_at":      baseTime.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
			"text":            strings.Repeat("quick capture ", 16),
			"file_name":       "note.md",
			"attachment_name": "attachment.txt",
			"attachment":      attachment,
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/webhooks/file", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		require.NoError(t, err)
		responseBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		require.Equalf(t, http.StatusOK, resp.StatusCode, "response: %s", responseBody)
		require.JSONEq(t, `{"ok":true}`, string(responseBody))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, listener.Close())
	}()

	return listener.Addr().String()
}

func waitForHTTP(t *testing.T, url string, logs *lockedBuffer) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				return
			}
			lastErr = fmt.Errorf("unexpected status %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("wait for %s: %v\nserver logs:\n%s", url, lastErr, logs.String())
}

func forceGC(t *testing.T, client *http.Client, debugAddr string) {
	t.Helper()

	resp, err := client.Get("http://" + debugAddr + "/debug/pprof/heap?gc=1")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

type memStats struct {
	TotalAlloc  uint64 `json:"TotalAlloc"`
	Mallocs     uint64 `json:"Mallocs"`
	HeapAlloc   uint64 `json:"HeapAlloc"`
	HeapObjects uint64 `json:"HeapObjects"`
	Sys         uint64 `json:"Sys"`
	NumGC       uint32 `json:"NumGC"`
}

func readMemStats(t *testing.T, client *http.Client, debugAddr string) memStats {
	t.Helper()

	resp, err := client.Get("http://" + debugAddr + "/debug/vars")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, resp.Body.Close())
	}()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		MemStats memStats `json:"memstats"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	return payload.MemStats
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}

	divisor := uint64(unit)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		if value < divisor*unit {
			return fmt.Sprintf("%.2f %s", float64(value)/float64(divisor), suffix)
		}
		divisor *= unit
	}

	return fmt.Sprintf("%.2f TiB", float64(value)/float64(divisor))
}

func formatBytesDelta(after uint64, before uint64) string {
	if after >= before {
		return "+" + formatBytes(after-before)
	}

	return "-" + formatBytes(before-after)
}

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
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/trees/"):
		g.handleGetTree(w)
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

func (g *fakeGitHub) handleGetTree(w http.ResponseWriter) {
	entries := make([]map[string]string, 0, len(g.occupied))
	for occupiedPath := range g.occupied {
		entries = append(entries, map[string]string{"path": occupiedPath, "type": "blob"})
	}

	writeJSON(w, map[string]any{"sha": "tree-current", "tree": entries})
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
