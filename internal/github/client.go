// Package github submits pull requests to GitHub's REST API. It provides the
// production implementation of queue.Submitter; the queue owns retry policy, so
// this package's only job is to perform one submission and classify the outcome
// as transient (worth retrying) or permanent.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v74/github"

	"github.com/AyushPramanik/mesh/internal/queue"
)

// Client opens pull requests in a single repository.
type Client struct {
	api   *gh.Client
	owner string
	repo  string
	base  string // base branch PRs target, e.g. "main"
}

// Config configures a Client.
type Config struct {
	Token string // GitHub token with repo scope
	Owner string
	Repo  string
	Base  string // base branch; defaults to "main" if empty
}

// New constructs a Client from cfg.
func New(cfg Config) (*Client, error) {
	if cfg.Token == "" {
		return nil, errors.New("github.New: token is required")
	}
	if cfg.Owner == "" || cfg.Repo == "" {
		return nil, errors.New("github.New: owner and repo are required")
	}
	base := cfg.Base
	if base == "" {
		base = "main"
	}
	return &Client{
		api:   gh.NewClient(nil).WithAuthToken(cfg.Token),
		owner: cfg.Owner,
		repo:  cfg.Repo,
		base:  base,
	}, nil
}

// Verify checks the credentials before the daemon relies on them, turning the
// common failures (no/expired token, missing repo scope, wrong repo) into clear
// messages instead of surfacing a stack trace at submission time.
func (c *Client) Verify(ctx context.Context) error {
	_, resp, err := c.api.Repositories.Get(ctx, c.owner, c.repo)
	if err == nil {
		// Token works and can see the repo; confirm it can also write.
		if scopes := resp.Header.Get("X-OAuth-Scopes"); scopes != "" && !hasRepoScope(scopes) {
			return fmt.Errorf("github: token lacks the 'repo' scope (has: %s) — create a token with repo access", scopes)
		}
		return nil
	}

	if resp != nil {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return fmt.Errorf("github: token is invalid or expired (401) — generate a new token with 'repo' scope")
		case http.StatusForbidden:
			return fmt.Errorf("github: token is forbidden (403) — it likely lacks the 'repo' scope or hit a rate limit")
		case http.StatusNotFound:
			return fmt.Errorf("github: %s/%s not found, or the token cannot access it (404) — check GITHUB_OWNER/GITHUB_REPO and that the token has 'repo' scope", c.owner, c.repo)
		}
	}
	return fmt.Errorf("github: could not verify credentials: %w", err)
}

func hasRepoScope(scopes string) bool {
	for _, s := range strings.Split(scopes, ",") {
		if strings.TrimSpace(s) == "repo" {
			return true
		}
	}
	return false
}

// ParseRepoURL extracts the owner and repository name from a GitHub remote URL
// in either SSH (git@github.com:owner/repo.git) or HTTPS
// (https://github.com/owner/repo) form. ok is false when the URL is not a
// recognisable GitHub remote.
func ParseRepoURL(remote string) (owner, repo string, ok bool) {
	s := strings.TrimSpace(remote)
	switch {
	case strings.HasPrefix(s, "git@"):
		// git@github.com:owner/repo.git
		if i := strings.Index(s, ":"); i >= 0 {
			s = s[i+1:]
		}
	case strings.Contains(s, "://"):
		// scheme://[user@]host/owner/repo
		if i := strings.Index(s, "://"); i >= 0 {
			s = s[i+3:]
		}
		if i := strings.Index(s, "/"); i >= 0 {
			s = s[i+1:] // strip host
		}
	default:
		return "", "", false
	}
	s = strings.TrimSuffix(s, ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

// Submit opens a pull request for pr.Branch against the client's base branch.
// A pull request that already exists for the branch is treated as success
// (submission is idempotent from the queue's perspective). Rate-limit and
// server errors are returned via queue.Transient so the queue retries them.
func (c *Client) Submit(ctx context.Context, pr queue.PR) error {
	head := pr.Branch
	_, resp, err := c.api.PullRequests.Create(ctx, c.owner, c.repo, &gh.NewPullRequest{
		Title: gh.Ptr(pr.Title),
		Head:  gh.Ptr(head),
		Base:  gh.Ptr(c.base),
	})
	if err == nil {
		return nil
	}
	return classify(resp, err)
}

// classify turns a go-github error into either nil (already-exists, treated as
// success), a queue.Transient error (retry), or a permanent error.
func classify(resp *gh.Response, err error) error {
	// Rate limiting is always transient.
	var rle *gh.RateLimitError
	var arle *gh.AbuseRateLimitError
	if errors.As(err, &rle) || errors.As(err, &arle) {
		return queue.Transient(fmt.Errorf("github: rate limited: %w", err))
	}

	if resp != nil {
		switch {
		case resp.StatusCode == http.StatusUnprocessableEntity && alreadyExists(err):
			// A PR already open for this head: nothing to do.
			return nil
		case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
			return queue.Transient(fmt.Errorf("github: server error %d: %w", resp.StatusCode, err))
		}
	}
	return fmt.Errorf("github: submit failed: %w", err)
}

// alreadyExists reports whether a 422 error is GitHub's "a pull request already
// exists" validation error rather than some other unprocessable-entity case.
func alreadyExists(err error) bool {
	var er *gh.ErrorResponse
	if !errors.As(err, &er) {
		return false
	}
	for _, e := range er.Errors {
		if containsFold(e.Message, "already exists") {
			return true
		}
	}
	return containsFold(er.Message, "already exists")
}

// containsFold reports whether needle appears in haystack, case-insensitively.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// Ensure Client satisfies the queue's Submitter interface at compile time.
var _ queue.Submitter = (*Client)(nil)
