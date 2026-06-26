package github

import (
	"net/http"
	"testing"

	gh "github.com/google/go-github/v74/github"
	"github.com/stretchr/testify/assert"

	"github.com/AyushPramanik/mesh/internal/queue"
)

func resp(status int) *gh.Response {
	return &gh.Response{Response: &http.Response{StatusCode: status}}
}

func TestClassify_RateLimitIsTransient(t *testing.T) {
	err := classify(resp(http.StatusForbidden), &gh.RateLimitError{Message: "rate limited"})
	assert.True(t, queue.IsTransient(err))
}

func TestClassify_ServerErrorIsTransient(t *testing.T) {
	err := classify(resp(http.StatusBadGateway), &gh.ErrorResponse{Message: "bad gateway"})
	assert.True(t, queue.IsTransient(err))
}

func TestClassify_AlreadyExistsIsSuccess(t *testing.T) {
	er := &gh.ErrorResponse{
		Message: "Validation Failed",
		Errors:  []gh.Error{{Message: "A pull request already exists for owner:branch."}},
	}
	err := classify(resp(http.StatusUnprocessableEntity), er)
	assert.NoError(t, err, "an existing PR for the branch is treated as success")
}

func TestClassify_OtherValidationIsPermanent(t *testing.T) {
	er := &gh.ErrorResponse{
		Message: "Validation Failed",
		Errors:  []gh.Error{{Message: "No commits between main and branch"}},
	}
	err := classify(resp(http.StatusUnprocessableEntity), er)
	assert.Error(t, err)
	assert.False(t, queue.IsTransient(err), "a real validation failure must not be retried")
}

func TestClassify_NotFoundIsPermanent(t *testing.T) {
	err := classify(resp(http.StatusNotFound), &gh.ErrorResponse{Message: "Not Found"})
	assert.Error(t, err)
	assert.False(t, queue.IsTransient(err))
}

func TestNew_Validation(t *testing.T) {
	_, err := New(Config{Owner: "o", Repo: "r"})
	assert.Error(t, err, "missing token should error")

	c, err := New(Config{Token: "t", Owner: "o", Repo: "r"})
	assert.NoError(t, err)
	assert.Equal(t, "main", c.base, "base defaults to main")
}
