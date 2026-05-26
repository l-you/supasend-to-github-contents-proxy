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

	baseTreeSHA := baseCommit.GetTree().GetSHA()
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
) ([]string, error) {
	ref, _, err := c.git.GetRef(ctx, owner, repo, headRef(branch))
	if err != nil {
		return nil, err
	}

	baseCommit, _, err := c.git.GetCommit(ctx, owner, repo, ref.GetObject().GetSHA())
	if err != nil {
		return nil, err
	}

	tree, _, err := c.git.GetTree(ctx, owner, repo, baseCommit.GetTree().GetSHA(), true)
	if err != nil {
		return nil, err
	}

	occupied := make(map[string]struct{}, len(tree.Entries)+len(desiredPaths))
	for _, entry := range tree.Entries {
		if entry.GetPath() != "" {
			occupied[entry.GetPath()] = struct{}{}
		}
	}

	paths := make([]string, 0, len(desiredPaths))
	for _, desiredPath := range desiredPaths {
		uniquePath := nextAvailablePath(desiredPath, occupied)
		occupied[uniquePath] = struct{}{}
		paths = append(paths, uniquePath)
	}

	return paths, nil
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

func nextAvailablePath(desiredPath string, occupied map[string]struct{}) string {
	desiredPath = path.Clean(desiredPath)
	if _, exists := occupied[desiredPath]; !exists {
		return desiredPath
	}

	dir, file := path.Split(desiredPath)
	extension := path.Ext(file)
	name := strings.TrimSuffix(file, extension)

	for suffix := 1; ; suffix++ {
		candidate := path.Join(dir, fmt.Sprintf("%s-%d%s", name, suffix, extension))
		if _, exists := occupied[candidate]; !exists {
			return candidate
		}
	}
}
