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
	"os"
	"os/exec"
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
	postCaptures(t, client, listenAddr, "e2e", requestCount, attachmentBytes)
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

	if profileDir := strings.TrimSpace(os.Getenv("E2E_PROFILE_DIR")); profileDir != "" {
		runProfiledLoad(t, client, listenAddr, debugAddr, profileDir, attachmentBytes)
	}
}

func postCaptures(
	t *testing.T,
	client *http.Client,
	addr string,
	folderPrefix string,
	count int,
	attachmentBytes int,
) {
	t.Helper()

	attachment := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), attachmentBytes))
	baseTime := time.Date(2026, 5, 26, 22, 35, 43, 0, time.FixedZone("EEST", 3*60*60))

	for i := range count {
		postCapture(t, client, addr, folderPrefix, i, baseTime, attachment)
	}
}

func runProfiledLoad(
	t *testing.T,
	client *http.Client,
	listenAddr string,
	debugAddr string,
	profileDir string,
	attachmentBytes int,
) {
	t.Helper()

	require.NoError(t, os.MkdirAll(profileDir, 0o755))

	seconds := envInt("E2E_CPU_PROFILE_SECONDS", 10)
	profileClient := &http.Client{Timeout: time.Duration(seconds+10) * time.Second}
	cpuPath := filepath.Join(profileDir, "cpu.pb.gz")
	cpuDone := make(chan error, 1)
	go func() {
		url := fmt.Sprintf("http://%s/debug/pprof/profile?seconds=%d", debugAddr, seconds)
		cpuDone <- downloadProfile(profileClient, url, cpuPath)
	}()

	attachment := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), attachmentBytes))
	baseTime := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	timeout := time.After(time.Duration(seconds+5) * time.Second)
	captures := 0

	for {
		select {
		case err := <-cpuDone:
			require.NoError(t, err)
			writeRuntimeProfiles(t, profileClient, debugAddr, profileDir)
			t.Logf("profiles: dir=%s cpu=%s captures=%d", profileDir, cpuPath, captures)
			return
		case <-timeout:
			t.Fatalf("cpu profile did not finish after %d seconds", seconds)
		default:
			postCapture(t, client, listenAddr, "profile", captures, baseTime, attachment)
			captures++
		}
	}
}

func postCapture(
	t *testing.T,
	client *http.Client,
	addr string,
	folderPrefix string,
	index int,
	baseTime time.Time,
	attachment string,
) {
	t.Helper()

	payload := map[string]string{
		"folder_name":     fmt.Sprintf("%s-%06d", folderPrefix, index),
		"created_at":      baseTime.Add(time.Duration(index) * time.Second).Format(time.RFC3339),
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

func writeRuntimeProfiles(t *testing.T, client *http.Client, debugAddr string, profileDir string) {
	t.Helper()

	for _, profile := range []string{"allocs", "heap"} {
		url := fmt.Sprintf("http://%s/debug/pprof/%s?gc=1", debugAddr, profile)
		path := filepath.Join(profileDir, profile+".pb.gz")
		require.NoError(t, downloadProfile(client, url, path))
	}
}

func downloadProfile(client *http.Client, url string, path string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download profile %s: %s", url, resp.Status)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	_, err = io.Copy(file, resp.Body)
	return err
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
