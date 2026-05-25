package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	apiURL     string
	token      string
	httpClient *http.Client
}

type File struct {
	Path    string
	Content []byte
}

type CommitResult struct {
	SHA     string
	TreeSHA string
	Created bool
}

type StatusError struct {
	StatusCode int
	Message    string
}

func (e StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("github api returned status %d", e.StatusCode)
	}

	return fmt.Sprintf("github api returned status %d: %s", e.StatusCode, e.Message)
}

func NewClient(apiURL string, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		apiURL:     strings.TrimRight(apiURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

func (c *Client) CommitFiles(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	message string,
	files []File,
) (CommitResult, error) {
	if len(files) == 0 {
		return CommitResult{}, errors.New("at least one file is required")
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := c.commitFilesOnce(ctx, owner, repo, branch, message, files)
		if err == nil {
			return result, nil
		}
		if !isRetryable(err) {
			return CommitResult{}, err
		}

		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}

	return CommitResult{}, lastErr
}

func (c *Client) commitFilesOnce(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	message string,
	files []File,
) (CommitResult, error) {
	repositoryPath := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)
	ref, err := c.getRef(ctx, repositoryPath, branch)
	if err != nil {
		return CommitResult{}, err
	}

	baseCommit, err := c.getCommit(ctx, repositoryPath, ref.Object.SHA)
	if err != nil {
		return CommitResult{}, err
	}

	treeItems := make([]treeItem, 0, len(files))
	for _, file := range files {
		blob, err := c.createBlob(ctx, repositoryPath, file.Content)
		if err != nil {
			return CommitResult{}, err
		}

		treeItems = append(treeItems, treeItem{
			Path: file.Path,
			Mode: "100644",
			Type: "blob",
			SHA:  blob.SHA,
		})
	}

	tree, err := c.createTree(ctx, repositoryPath, baseCommit.Tree.SHA, treeItems)
	if err != nil {
		return CommitResult{}, err
	}
	if tree.SHA == baseCommit.Tree.SHA {
		return CommitResult{SHA: baseCommit.SHA, TreeSHA: baseCommit.Tree.SHA, Created: false}, nil
	}

	commit, err := c.createCommit(ctx, repositoryPath, message, tree.SHA, baseCommit.SHA)
	if err != nil {
		return CommitResult{}, err
	}
	if err := c.updateRef(ctx, repositoryPath, branch, commit.SHA); err != nil {
		return CommitResult{}, err
	}

	return CommitResult{SHA: commit.SHA, TreeSHA: tree.SHA, Created: true}, nil
}

func (c *Client) getRef(ctx context.Context, repositoryPath string, branch string) (refResponse, error) {
	var out refResponse
	endpoint := repositoryPath + "/git/ref/heads/" + escapePathSegments(branch)
	err := c.doJSON(ctx, http.MethodGet, endpoint, nil, &out)
	return out, err
}

func (c *Client) getCommit(ctx context.Context, repositoryPath string, sha string) (commitResponse, error) {
	var out commitResponse
	err := c.doJSON(ctx, http.MethodGet, repositoryPath+"/git/commits/"+url.PathEscape(sha), nil, &out)
	return out, err
}

func (c *Client) createBlob(ctx context.Context, repositoryPath string, content []byte) (blobResponse, error) {
	var out blobResponse
	request := blobRequest{
		Content:  base64.StdEncoding.EncodeToString(content),
		Encoding: "base64",
	}
	err := c.doJSON(ctx, http.MethodPost, repositoryPath+"/git/blobs", request, &out)
	return out, err
}

func (c *Client) createTree(
	ctx context.Context,
	repositoryPath string,
	baseTree string,
	items []treeItem,
) (treeResponse, error) {
	var out treeResponse
	request := treeRequest{
		BaseTree: baseTree,
		Tree:     items,
	}
	err := c.doJSON(ctx, http.MethodPost, repositoryPath+"/git/trees", request, &out)
	return out, err
}

func (c *Client) createCommit(
	ctx context.Context,
	repositoryPath string,
	message string,
	treeSHA string,
	parentSHA string,
) (commitResponse, error) {
	var out commitResponse
	request := createCommitRequest{
		Message: message,
		Tree:    treeSHA,
		Parents: []string{parentSHA},
	}
	err := c.doJSON(ctx, http.MethodPost, repositoryPath+"/git/commits", request, &out)
	return out, err
}

func (c *Client) updateRef(ctx context.Context, repositoryPath string, branch string, sha string) error {
	request := updateRefRequest{
		SHA:   sha,
		Force: false,
	}
	endpoint := repositoryPath + "/git/refs/heads/" + escapePathSegments(branch)
	return c.doJSON(ctx, http.MethodPatch, endpoint, request, nil)
}

func (c *Client) doJSON(ctx context.Context, method string, endpoint string, in any, out any) error {
	body, err := encodeBody(in)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+endpoint, body)
	if err != nil {
		return fmt.Errorf("create github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call github api: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return decodeStatusError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}

	return nil
}

func encodeBody(in any) (io.Reader, error) {
	if in == nil {
		return nil, nil
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		return nil, fmt.Errorf("encode github request: %w", err)
	}

	return &body, nil
}

func decodeStatusError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return StatusError{StatusCode: resp.StatusCode, Message: resp.Status}
	}

	var response struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return StatusError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}

	return StatusError{StatusCode: resp.StatusCode, Message: response.Message}
}

func isRetryable(err error) bool {
	var statusErr StatusError
	if !errors.As(err, &statusErr) {
		return false
	}

	return statusErr.StatusCode == http.StatusConflict
}

func escapePathSegments(value string) string {
	value = strings.TrimPrefix(value, "refs/heads/")
	segments := strings.Split(path.Clean(value), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}

	return strings.Join(segments, "/")
}

type refResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type commitResponse struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

type blobRequest struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type blobResponse struct {
	SHA string `json:"sha"`
}

type treeRequest struct {
	BaseTree string     `json:"base_tree"`
	Tree     []treeItem `json:"tree"`
}

type treeItem struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type treeResponse struct {
	SHA string `json:"sha"`
}

type createCommitRequest struct {
	Message string   `json:"message"`
	Tree    string   `json:"tree"`
	Parents []string `json:"parents"`
}

type updateRefRequest struct {
	SHA   string `json:"sha"`
	Force bool   `json:"force"`
}
