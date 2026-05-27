package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	gh "github.com/google/go-github/v88/github"
)

type Client struct {
	git *gh.GitService
}

type File struct {
	Path    string
	Content []byte
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
	if !isDefaultAPIURL(apiURL) {
		baseURL := ensureTrailingSlash(apiURL)
		options = append(options, gh.WithURLs(&baseURL, nil))
	}

	client, err := gh.NewClient(options...)
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}

	return &Client{git: client.Git}, nil
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
		if err := c.rejectExistingPaths(ctx, owner, repo, baseTreeSHA, files); err != nil {
			return CommitResult{}, err
		}
	}

	treeEntries := make([]*gh.TreeEntry, 0, len(files))
	for _, file := range files {
		blob, _, err := c.git.CreateBlob(ctx, owner, repo, gh.Blob{
			Content:  gh.Ptr(base64.StdEncoding.EncodeToString(file.Content)),
			Encoding: gh.Ptr("base64"),
		})
		if err != nil {
			return CommitResult{}, err
		}

		treeEntries = append(treeEntries, &gh.TreeEntry{
			Path: gh.Ptr(file.Path),
			Mode: gh.Ptr("100644"),
			Type: gh.Ptr("blob"),
			SHA:  gh.Ptr(blob.GetSHA()),
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
	treeSHA string,
	files []File,
) error {
	occupied, err := c.occupiedPathsForTree(ctx, owner, repo, treeSHA)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		cleanPath := path.Clean(file.Path)
		if _, exists := occupied[cleanPath]; exists {
			return PathExistsError{Path: cleanPath}
		}
		if _, exists := seen[cleanPath]; exists {
			return PathExistsError{Path: cleanPath}
		}
		seen[cleanPath] = struct{}{}
	}

	return nil
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
