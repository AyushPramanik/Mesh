package conflict

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSymbolGraph_DefinesAndRefs(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"a.go": []byte(`package p

type Widget struct{}

func Build() Widget { return helper() }
`),
	})
	assert.Contains(t, g.Defines, "Widget")
	assert.Contains(t, g.Defines, "Build")
	assert.Contains(t, g.Refs, "helper") // referenced, not defined here
	assert.NotContains(t, g.Defines, "helper")
}

func TestBuildSymbolGraph_SkipsNonGoAndUnparseable(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"readme.md": []byte("# not go"),
		"broken.go": []byte("package p\nfunc ("), // does not parse
		"ok.go":     []byte("package p\nfunc Good() {}"),
	})
	assert.Contains(t, g.Defines, "Good")
	assert.Len(t, g.Defines, 1)
}

func TestSemanticConflicts_DependencyAcrossFiles(t *testing.T) {
	// Branch A changes the definition of Authenticate; branch B (a different
	// file) calls Authenticate. File overlap is empty, but they are coupled.
	a := BuildSymbolGraph(map[string][]byte{
		"auth/auth.go": []byte(`package auth
func Authenticate(token string) bool { return token != "" }`),
	})
	b := BuildSymbolGraph(map[string][]byte{
		"api/handler.go": []byte(`package api
import "x/auth"
func Handle() { _ = auth.Authenticate("t") }`),
	})

	conflicts := SemanticConflicts(a, b)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "Authenticate", conflicts[0].Symbol)
	assert.Equal(t, kindDependency, conflicts[0].Kind)
}

func TestSemanticConflicts_DirectSameSymbol(t *testing.T) {
	a := BuildSymbolGraph(map[string][]byte{"a.go": []byte("package p\nfunc Foo() int { return 1 }")})
	b := BuildSymbolGraph(map[string][]byte{"b.go": []byte("package p\nfunc Foo() int { return 2 }")})

	conflicts := SemanticConflicts(a, b)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "Foo", conflicts[0].Symbol)
	assert.Equal(t, kindDirect, conflicts[0].Kind)
}

func TestSemanticConflicts_DisjointSymbolsAreClear(t *testing.T) {
	a := BuildSymbolGraph(map[string][]byte{"a.go": []byte("package p\nfunc Alpha() {}")})
	b := BuildSymbolGraph(map[string][]byte{"b.go": []byte("package p\nfunc Beta() {}")})
	assert.Empty(t, SemanticConflicts(a, b))
}

// fakeBranches is a BranchSource backed by in-memory branch -> file -> content.
type fakeBranches map[string]map[string][]byte

func (f fakeBranches) ChangedFiles(_ context.Context, branch string) ([]string, error) {
	var files []string
	for p := range f[branch] {
		files = append(files, p)
	}
	return files, nil
}

func (f fakeBranches) ReadFile(_ context.Context, branch, path string) ([]byte, error) {
	return f[branch][path], nil
}

func TestAnalyzer_ConflictsBetweenBranches(t *testing.T) {
	src := fakeBranches{
		"feat-a": {"auth/auth.go": []byte("package auth\nfunc Login() {}")},
		"feat-b": {"api/h.go": []byte("package api\nfunc H() { Login() }")},
		"feat-c": {"ui/ui.go": []byte("package ui\nfunc Render() {}")},
	}
	a := NewAnalyzer(src)
	ctx := context.Background()

	// a and b are semantically coupled through Login.
	ab, err := a.Conflicts(ctx, "feat-a", "feat-b")
	require.NoError(t, err)
	require.Len(t, ab, 1)
	assert.Equal(t, "Login", ab[0].Symbol)

	// a and c are unrelated.
	ac, err := a.Conflicts(ctx, "feat-a", "feat-c")
	require.NoError(t, err)
	assert.Empty(t, ac)
}
