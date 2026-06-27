package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is a linked git worktree: an isolated working directory that shares
// the parent repository's object store. It is the on-disk substrate for a Mesh
// workspace, and so honours the workspace invariant from CLAUDE.md — exactly one
// branch per worktree.
type Worktree struct {
	// Name is Mesh's handle for the worktree, unique within a repository.
	Name string
	// Branch is the branch checked out in the worktree.
	Branch string
	// Path is the absolute path of the worktree's working directory.
	Path string
}

// worktreeRoot returns the directory under which Mesh materialises worktree
// working directories. They live as a sibling of the main working tree so they
// are never themselves tracked by the repository.
func (r *Repo) worktreeRoot() string {
	return filepath.Join(filepath.Dir(r.dir), filepath.Base(r.dir)+"-worktrees")
}

// CreateWorktree creates a linked worktree named name with a new branch, both
// rooted at the current HEAD, and returns a handle to it.
//
// IMPLEMENTATION NOTE: go-git has no equivalent of `git worktree add`, so this
// single concern shells out to the system git binary. This is the one
// documented deviation from the go-git-only object layer (see package doc). The
// repository must have at least one commit, since a worktree needs a commit-ish
// to check out.
func (r *Repo) CreateWorktree(ctx context.Context, name, branch string) (*Worktree, error) {
	if r.dir == "" {
		return nil, fmt.Errorf("git.CreateWorktree: in-memory repositories cannot host worktrees")
	}
	if err := validateName(name); err != nil {
		return nil, fmt.Errorf("git.CreateWorktree: %w", err)
	}

	path := filepath.Join(r.worktreeRoot(), name)
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("git.CreateWorktree: worktree %q already exists", name)
	}
	if err := os.MkdirAll(r.worktreeRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("git.CreateWorktree: mkdir root: %w", err)
	}

	if _, err := r.runGit(ctx, "worktree", "add", "-b", branch, path, "HEAD"); err != nil {
		return nil, fmt.Errorf("git.CreateWorktree: %w", err)
	}
	return &Worktree{Name: name, Branch: branch, Path: path}, nil
}

// ListWorktrees returns every linked worktree Mesh manages for this repository.
// The main working tree is excluded — only linked worktrees are returned.
func (r *Repo) ListWorktrees(ctx context.Context) ([]*Worktree, error) {
	if r.dir == "" {
		return nil, nil
	}
	out, err := r.runGit(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git.ListWorktrees: %w", err)
	}

	root := r.worktreeRoot()
	var worktrees []*Worktree
	var cur *Worktree
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			path := strings.TrimPrefix(line, "worktree ")
			cur = &Worktree{Path: path, Name: filepath.Base(path)}
		case strings.HasPrefix(line, "branch "):
			if cur != nil {
				// Porcelain reports the full ref, e.g. refs/heads/feature.
				cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			}
		case line == "":
			// Blank line terminates a record. Keep only worktrees Mesh
			// manages (those under worktreeRoot), skipping the main tree.
			if cur != nil && isUnder(cur.Path, root) {
				worktrees = append(worktrees, cur)
			}
			cur = nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("git.ListWorktrees: scan: %w", err)
	}
	if cur != nil && isUnder(cur.Path, root) {
		worktrees = append(worktrees, cur)
	}
	return worktrees, nil
}

// RemoveWorktree deletes the linked worktree named name, removing both its
// working directory and the repository's bookkeeping for it. It does not delete
// the branch.
func (r *Repo) RemoveWorktree(ctx context.Context, name string) error {
	if r.dir == "" {
		return fmt.Errorf("git.RemoveWorktree: in-memory repositories cannot host worktrees")
	}
	path := filepath.Join(r.worktreeRoot(), name)
	if _, err := r.runGit(ctx, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("git.RemoveWorktree: %w", err)
	}
	return nil
}

// ChangedFiles returns the repository-relative paths that branch changes
// relative to base, using the merge-base (three-dot) diff GitHub shows for a
// pull request. The result is the file-level footprint the merge-train
// scheduler compares for overlap.
//
// Like worktree lifecycle, this shells out to system git: a three-dot diff is
// awkward to assemble correctly with go-git, and the call is read-only.
func (r *Repo) ChangedFiles(ctx context.Context, base, branch string) ([]string, error) {
	if r.dir == "" {
		return nil, fmt.Errorf("git.ChangedFiles: in-memory repositories are unsupported")
	}
	out, err := r.runGit(ctx, "diff", "--name-only", base+"..."+branch)
	if err != nil {
		return nil, fmt.Errorf("git.ChangedFiles: %w", err)
	}
	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			files = append(files, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("git.ChangedFiles: scan: %w", err)
	}
	return files, nil
}

// CommitWorktree stages every change in the worktree at worktreePath and
// records a commit. It returns the new commit's short hash.
//
// This is a convenience wrapper over raw git for agents driving the CLI: the
// underlying storage stays standard git (CLAUDE.md "not a git replacement"), we
// just run the commands in the right worktree so the caller need not cd around.
func (r *Repo) CommitWorktree(ctx context.Context, worktreePath, message string) (string, error) {
	if _, err := r.runGitIn(ctx, worktreePath, "add", "-A"); err != nil {
		return "", fmt.Errorf("git.CommitWorktree: %w", err)
	}
	commit := append(r.identityArgs(ctx, worktreePath), "commit", "-m", message)
	if _, err := r.runGitIn(ctx, worktreePath, commit...); err != nil {
		return "", fmt.Errorf("git.CommitWorktree: %w", err)
	}
	out, err := r.runGitIn(ctx, worktreePath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git.CommitWorktree: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// PushWorktree pushes branch from the worktree at worktreePath to remote,
// setting upstream. remote defaults to "origin" when empty.
func (r *Repo) PushWorktree(ctx context.Context, worktreePath, branch, remote string) error {
	if remote == "" {
		remote = "origin"
	}
	if _, err := r.runGitIn(ctx, worktreePath, "push", "--set-upstream", remote, branch); err != nil {
		return fmt.Errorf("git.PushWorktree: %w", err)
	}
	return nil
}

// RemoteURL returns the configured URL of the named remote (e.g. "origin"), or
// an error if it is not set.
func (r *Repo) RemoteURL(ctx context.Context, name string) (string, error) {
	out, err := r.runGit(ctx, "remote", "get-url", name)
	if err != nil {
		return "", fmt.Errorf("git.RemoteURL: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBranch merges branch into the currently checked-out branch of the main
// working tree with a merge commit, and returns the new commit's short hash. It
// is the local landing primitive for merge trains: continuous merge into the
// base without going through the PR gate.
func (r *Repo) MergeBranch(ctx context.Context, branch, message string) (string, error) {
	merge := append(r.identityArgs(ctx, r.dir), "merge", "--no-ff", "-m", message, branch)
	if _, err := r.runGit(ctx, merge...); err != nil {
		return "", fmt.Errorf("git.MergeBranch: %w", err)
	}
	out, err := r.runGit(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git.MergeBranch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ReadFileAtBranch returns the contents of a repository-relative path as it
// exists at the tip of branch. A path absent on that branch yields an error.
func (r *Repo) ReadFileAtBranch(ctx context.Context, branch, path string) ([]byte, error) {
	out, err := r.runGit(ctx, "show", branch+":"+path)
	if err != nil {
		return nil, fmt.Errorf("git.ReadFileAtBranch: %w", err)
	}
	return out, nil
}

// runGit runs a git subcommand in the repository's main working tree.
func (r *Repo) runGit(ctx context.Context, args ...string) ([]byte, error) {
	return r.runGitIn(ctx, r.dir, args...)
}

// identityArgs returns the `-c user.name=… -c user.email=…` flags needed to
// record a commit when the host git has no configured identity — the common
// case in fresh containers and CI, where agents driving Mesh often run. When an
// identity is already configured (repo or global), it returns nil so the user's
// real attribution is preserved. The fallback honours git's own
// GIT_{AUTHOR,COMMITTER}_{NAME,EMAIL} env vars before defaulting to Mesh.
func (r *Repo) identityArgs(ctx context.Context, dir string) []string {
	if out, err := r.runGitIn(ctx, dir, "config", "user.email"); err == nil && strings.TrimSpace(string(out)) != "" {
		return nil
	}
	name := firstNonEmpty(os.Getenv("GIT_AUTHOR_NAME"), os.Getenv("GIT_COMMITTER_NAME"), "Mesh")
	email := firstNonEmpty(os.Getenv("GIT_AUTHOR_EMAIL"), os.Getenv("GIT_COMMITTER_EMAIL"), "mesh@localhost")
	return []string{"-c", "user.name=" + name, "-c", "user.email=" + email}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// runGitIn runs a git subcommand in dir and returns its stdout. Stderr is
// folded into the returned error so failures are diagnosable.
func (r *Repo) runGitIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// validateName rejects worktree names that would escape the worktree root or
// collide with path separators.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("invalid worktree name %q", name)
	}
	return nil
}

// isUnder reports whether path is contained in dir. Both are resolved through
// any symlinks first, because `git worktree list` reports canonical paths (on
// macOS /var is a symlink to /private/var) which would otherwise not match a
// lexically-constructed root.
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(resolve(dir), resolve(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolve returns path with symlinks evaluated, falling back to the cleaned
// path if it cannot be resolved (e.g. it does not yet exist).
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}
