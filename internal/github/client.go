package github

import (
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

	gh "github.com/google/go-github/v88/github"
)

type Client struct {
	git          *gh.GitService
	repositories *gh.RepositoriesService
	httpClient   *http.Client
	baseURL      string
}

type File struct {
	Path          string
	Content       []byte
	Base64Content string
}

type CommitResult struct {
	SHA string
}

type CommitOptions struct {
	RejectExisting bool
}

type PathUnavailableError struct {
	Path     string
	MaxIndex int
}

func (e PathUnavailableError) Error() string {
	return fmt.Sprintf("no available path for %q after suffix -%d", e.Path, e.MaxIndex)
}

type PathExistsError struct {
	Path string
}

func (e PathExistsError) Error() string {
	return fmt.Sprintf("target path %q already exists", e.Path)
}

func NewClient(apiURL string, token string, httpClient *http.Client) (*Client, error) {
	options := []gh.ClientOptionsFunc{gh.WithAuthToken(token)}
	if httpClient != nil {
		options = append(options, gh.WithHTTPClient(httpClient))
	}
	baseAPIURL := "https://api.github.com/"
	if !isDefaultAPIURL(apiURL) {
		baseURL := ensureTrailingSlash(apiURL)
		baseAPIURL = baseURL
		options = append(options, gh.WithURLs(&baseURL, nil))
	}

	client, err := gh.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}

	return &Client{
		git:          client.Git,
		repositories: client.Repositories,
		httpClient:   client.Client(),
		baseURL:      baseAPIURL,
	}, nil
}

func (c *Client) CommitFiles(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	message string,
	files []File,
	options CommitOptions,
) (CommitResult, error) {
	if len(files) == 0 {
		return CommitResult{}, errors.New("at least one file is required")
	}

	ref, _, err := c.git.GetRef(ctx, owner, repo, headRef(branch))
	if err != nil {
		return CommitResult{}, err
	}

	baseCommit, _, err := c.git.GetCommit(ctx, owner, repo, ref.GetObject().GetSHA())
	if err != nil {
		return CommitResult{}, err
	}

	baseTreeSHA := baseCommit.GetTree().GetSHA()
	if options.RejectExisting {
		if err := c.rejectExistingPaths(ctx, owner, repo, branch, files); err != nil {
			return CommitResult{}, err
		}
	}

	treeEntries := make([]*gh.TreeEntry, 0, len(files))
	for _, file := range files {
		blobSHA, err := c.createBlob(ctx, owner, repo, file)
		if err != nil {
			return CommitResult{}, err
		}

		treeEntries = append(treeEntries, &gh.TreeEntry{
			Path: gh.Ptr(file.Path),
			Mode: gh.Ptr("100644"),
			Type: gh.Ptr("blob"),
			SHA:  gh.Ptr(blobSHA),
		})
	}

	tree, _, err := c.git.CreateTree(ctx, owner, repo, baseTreeSHA, treeEntries)
	if err != nil {
		return CommitResult{}, err
	}

	commit, _, err := c.git.CreateCommit(ctx, owner, repo, gh.Commit{
		Message: gh.Ptr(message),
		Tree:    &gh.Tree{SHA: gh.Ptr(tree.GetSHA())},
		Parents: []*gh.Commit{
			{SHA: gh.Ptr(baseCommit.GetSHA())},
		},
	}, nil)
	if err != nil {
		return CommitResult{}, err
	}
	if _, _, err := c.git.UpdateRef(ctx, owner, repo, headRef(branch), gh.UpdateRef{
		SHA:   commit.GetSHA(),
		Force: gh.Ptr(false),
	}); err != nil {
		return CommitResult{}, err
	}

	return CommitResult{SHA: commit.GetSHA()}, nil
}

func (f File) encodedContent() string {
	if f.Base64Content != "" {
		return f.Base64Content
	}

	return base64.StdEncoding.EncodeToString(f.Content)
}

func (c *Client) createBlob(ctx context.Context, owner string, repo string, file File) (string, error) {
	req, err := c.newJSONRequest(
		ctx,
		http.MethodPost,
		repoEndpoint(owner, repo, "git/blobs"),
		base64BlobBody(file.encodedContent()),
	)
	if err != nil {
		return "", err
	}

	var response struct {
		SHA string `json:"sha"`
	}
	if err := c.do(req, &response); err != nil {
		return "", err
	}
	if response.SHA == "" {
		return "", errors.New("create blob: missing sha")
	}

	return response.SHA, nil
}

func (c *Client) newJSONRequest(
	ctx context.Context,
	method string,
	endpoint string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL, "/")+"/"+endpoint, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	return req, nil
}

func (c *Client) do(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if err := gh.CheckResponse(resp); err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode github response: %w", err)
	}

	return nil
}

func repoEndpoint(owner string, repo string, endpoint string) string {
	return "repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/" + endpoint
}

func base64BlobBody(content string) io.Reader {
	return &base64BlobReader{content: content}
}

type base64BlobReader struct {
	content string
	part    int
	offset  int
}

func (r *base64BlobReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	n := 0
	for n < len(p) && r.part < 3 {
		part := r.currentPart()
		if r.offset >= len(part) {
			r.part++
			r.offset = 0
			continue
		}

		copied := copy(p[n:], part[r.offset:])
		r.offset += copied
		n += copied
	}
	if n > 0 {
		return n, nil
	}

	return 0, io.EOF
}

func (r *base64BlobReader) currentPart() string {
	switch r.part {
	case 0:
		return `{"content":"`
	case 1:
		return r.content
	case 2:
		return `","encoding":"base64"}`
	default:
		return ""
	}
}

func (c *Client) UniquePaths(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	desiredPaths []string,
	maxIndex int,
) ([]string, error) {
	occupied, err := c.OccupiedPaths(ctx, owner, repo, branch)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(desiredPaths))
	for _, desiredPath := range desiredPaths {
		uniquePath, err := nextAvailableFilePath(desiredPath, occupied, maxIndex)
		if err != nil {
			return nil, err
		}
		occupied[uniquePath] = struct{}{}
		paths = append(paths, uniquePath)
	}

	return paths, nil
}

func (c *Client) UniqueDirectory(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	desiredPath string,
	maxIndex int,
) (string, error) {
	occupied, err := c.OccupiedPaths(ctx, owner, repo, branch)
	if err != nil {
		return "", err
	}

	return nextAvailableDirectoryPath(desiredPath, occupied, maxIndex)
}

func (c *Client) OccupiedPaths(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
) (map[string]struct{}, error) {
	ref, _, err := c.git.GetRef(ctx, owner, repo, headRef(branch))
	if err != nil {
		return nil, err
	}

	baseCommit, _, err := c.git.GetCommit(ctx, owner, repo, ref.GetObject().GetSHA())
	if err != nil {
		return nil, err
	}

	return c.occupiedPathsForTree(ctx, owner, repo, baseCommit.GetTree().GetSHA())
}

func (c *Client) rejectExistingPaths(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	files []File,
) error {
	var fixed [4]string
	paths := fixed[:0]
	if len(files) > len(fixed) {
		paths = make([]string, 0, len(files))
	}

	for i, file := range files {
		cleanPath := path.Clean(file.Path)
		for _, previous := range files[:i] {
			if path.Clean(previous.Path) == cleanPath {
				return PathExistsError{Path: cleanPath}
			}
		}
		paths = append(paths, cleanPath)
	}

	if len(paths) == 0 {
		return nil
	}

	for _, cleanPath := range paths {
		exists, err := c.pathExists(ctx, owner, repo, branch, cleanPath)
		if err != nil {
			return err
		}
		if exists {
			return PathExistsError{Path: cleanPath}
		}
	}

	return nil
}

func (c *Client) pathExists(
	ctx context.Context,
	owner string,
	repo string,
	branch string,
	cleanPath string,
) (bool, error) {
	_, _, _, err := c.repositories.GetContents(ctx, owner, repo, cleanPath, &gh.RepositoryContentGetOptions{
		Ref: branch,
	})
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}

	return false, err
}

func (c *Client) occupiedPathsForTree(
	ctx context.Context,
	owner string,
	repo string,
	treeSHA string,
) (map[string]struct{}, error) {
	tree, _, err := c.git.GetTree(ctx, owner, repo, treeSHA, true)
	if err != nil {
		return nil, err
	}
	occupied := make(map[string]struct{}, len(tree.Entries))
	for _, entry := range tree.Entries {
		if entry.GetPath() != "" {
			occupied[entry.GetPath()] = struct{}{}
		}
	}

	return occupied, nil
}

func isRetryable(err error) bool {
	var responseErr *gh.ErrorResponse
	if !errors.As(err, &responseErr) || responseErr.Response == nil {
		return false
	}

	return responseErr.Response.StatusCode == http.StatusConflict
}

func isNotFound(err error) bool {
	var responseErr *gh.ErrorResponse
	if !errors.As(err, &responseErr) || responseErr.Response == nil {
		return false
	}

	return responseErr.Response.StatusCode == http.StatusNotFound
}

func IsRetryable(err error) bool {
	return isRetryable(err)
}

func IsPathUnavailable(err error) bool {
	var pathErr PathUnavailableError
	return errors.As(err, &pathErr)
}

func IsPathExists(err error) bool {
	var pathErr PathExistsError
	return errors.As(err, &pathErr)
}

func isDefaultAPIURL(apiURL string) bool {
	switch strings.TrimRight(apiURL, "/") {
	case "", "https://api.github.com":
		return true
	default:
		return false
	}
}

func ensureTrailingSlash(apiURL string) string {
	return strings.TrimRight(apiURL, "/") + "/"
}

func headRef(branch string) string {
	branch = strings.TrimPrefix(branch, "refs/heads/")
	branch = strings.TrimPrefix(branch, "heads/")
	return "heads/" + branch
}

func nextAvailableFilePath(desiredPath string, occupied map[string]struct{}, maxIndex int) (string, error) {
	desiredPath = path.Clean(desiredPath)
	if _, exists := occupied[desiredPath]; !exists {
		return desiredPath, nil
	}

	dir, file := path.Split(desiredPath)
	extension := path.Ext(file)
	name := strings.TrimSuffix(file, extension)

	for suffix := 1; suffix <= maxIndex; suffix++ {
		candidate := path.Join(dir, fmt.Sprintf("%s-%d%s", name, suffix, extension))
		if _, exists := occupied[candidate]; !exists {
			return candidate, nil
		}
	}

	return "", PathUnavailableError{Path: desiredPath, MaxIndex: maxIndex}
}

func nextAvailableDirectoryPath(desiredPath string, occupied map[string]struct{}, maxIndex int) (string, error) {
	desiredPath = path.Clean(desiredPath)
	if !directoryOccupied(desiredPath, occupied) {
		return desiredPath, nil
	}

	for suffix := 1; suffix <= maxIndex; suffix++ {
		candidate := fmt.Sprintf("%s-%d", desiredPath, suffix)
		if !directoryOccupied(candidate, occupied) {
			return candidate, nil
		}
	}

	return "", PathUnavailableError{Path: desiredPath, MaxIndex: maxIndex}
}

func directoryOccupied(dir string, occupied map[string]struct{}) bool {
	dir = strings.TrimSuffix(path.Clean(dir), "/")
	for occupiedPath := range occupied {
		if occupiedPath == dir || strings.HasPrefix(occupiedPath, dir+"/") {
			return true
		}
	}

	return false
}
