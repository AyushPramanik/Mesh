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
