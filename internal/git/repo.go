// Package git is the only package permitted to mutate the on-disk git object
// store. Per CLAUDE.md ("go-git side effects"), every other package reads
// through the git object store but must not write to it directly — it calls the
// functions in this package instead. The implementation is built on go-git so
// that Mesh carries no dependency on a system git version for object access.
//
// Worktree *lifecycle* (see worktree.go) is the one documented exception: go-git
// has no equivalent of `git worktree add`, so that single concern shells out to
// the system git binary. It is quarantined in worktree.go behind this package's
// API and can be replaced if go-git gains linked-worktree support.
package git

import (
	"fmt"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

// meshSignature is the author/committer used for commits Mesh creates on behalf
// of an agent. Per-agent attribution is layered on top by higher levels; at the
// git layer we record a stable identity so object hashes are reproducible.
func meshSignature(when time.Time) *object.Signature {
	return &object.Signature{
		Name:  "mesh",
		Email: "mesh@localhost",
		When:  when,
	}
}

// Repo wraps a go-git repository together with the absolute path of its working
// tree on disk. The zero value is not usable; construct it with Init, Open, or
// InitMemory.
type Repo struct {
	repo *gogit.Repository
	// dir is the absolute path of the main working tree. It is empty for
	// in-memory repositories, which therefore cannot host worktrees.
	dir string
}

// InitMemory creates an empty repository backed entirely by memory. It exists
// for tests that need a real git object store but no disk (see CLAUDE.md:
// "Integration tests that need a real git repo use t.TempDir() and
// git.InitMemory()"). Memory repos cannot create worktrees.
func InitMemory() (*Repo, error) {
	repo, err := gogit.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, fmt.Errorf("git.InitMemory: %w", err)
	}
	return &Repo{repo: repo}, nil
}

// Init creates an empty repository on disk at dir, creating dir if needed.
func Init(dir string) (*Repo, error) {
	repo, err := gogit.PlainInit(dir, false)
	if err != nil {
		return nil, fmt.Errorf("git.Init: %w", err)
	}
	return &Repo{repo: repo, dir: dir}, nil
}

// Open opens an existing repository on disk at dir.
func Open(dir string) (*Repo, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return nil, fmt.Errorf("git.Open: %w", err)
	}
	return &Repo{repo: repo, dir: dir}, nil
}

// Dir returns the absolute path of the main working tree, or "" for an
// in-memory repository.
func (r *Repo) Dir() string { return r.dir }

// Head returns the commit hash currently referenced by HEAD.
func (r *Repo) Head() (plumbing.Hash, error) {
	ref, err := r.repo.Head()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git.Head: %w", err)
	}
	return ref.Hash(), nil
}

// Commit writes the given files into the working tree (creating or overwriting
// them), stages every change, and records a single commit on the current
// branch. It returns the new commit hash. Paths are repository-relative and use
// forward slashes.
//
// This is a convenience used by tests and by higher layers that need to
// materialise agent output; it is intentionally the only write path exposed for
// commits so all commit metadata flows through meshSignature.
func (r *Repo) Commit(message string, files map[string][]byte) (plumbing.Hash, error) {
	wt, err := r.repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git.Commit: worktree: %w", err)
	}

	for path, content := range files {
		f, err := wt.Filesystem.Create(path)
		if err != nil {
			return plumbing.ZeroHash, fmt.Errorf("git.Commit: create %q: %w", path, err)
		}
		if _, err := f.Write(content); err != nil {
			_ = f.Close()
			return plumbing.ZeroHash, fmt.Errorf("git.Commit: write %q: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("git.Commit: close %q: %w", path, err)
		}
		if _, err := wt.Add(path); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("git.Commit: stage %q: %w", path, err)
		}
	}

	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author:    meshSignature(time.Now()),
		Committer: meshSignature(time.Now()),
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("git.Commit: %w", err)
	}
	return hash, nil
}

// CreateBranch creates a new branch named branch pointing at the current HEAD.
// It does not check the branch out.
func (r *Repo) CreateBranch(branch string) error {
	head, err := r.repo.Head()
	if err != nil {
		return fmt.Errorf("git.CreateBranch: head: %w", err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), head.Hash())
	if err := r.repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("git.CreateBranch: %w", err)
	}
	return nil
}

// Branches returns the names of all local branches, sorted by go-git's
// iteration order.
func (r *Repo) Branches() ([]string, error) {
	iter, err := r.repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("git.Branches: %w", err)
	}
	defer iter.Close()

	var names []string
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("git.Branches: iterate: %w", err)
	}
	return names, nil
}
