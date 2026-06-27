package conflict

import (
	"context"
	"fmt"
	"sort"
)

// SymbolGraph summarises a branch's changes at the symbol level: the top-level
// symbols whose definitions the branch touches, and the symbols the branch
// references. Comparing two branches' graphs surfaces the conflicts file overlap
// misses — the "silent semantic conflict" where two agents edit different files
// but one defines a symbol the other depends on (CLAUDE.md "Conflict graph":
// dependency and structural conflicts).
//
// The graph shape is language-agnostic: each file is parsed by the parser
// registered for its extension (see parser.go). Go is parsed precisely with the
// standard library AST; other languages use pure-Go heuristic parsers. In every
// case references are collected without full type resolution, so the signal is a
// strong hint, not a proof — consistent with intents being best-effort
// predictions.
type SymbolGraph struct {
	// Defines holds top-level symbol names the branch declares (and so may be
	// changing): funcs, methods, types, vars, consts.
	Defines map[string]struct{}
	// Refs holds identifiers the branch uses outside their declaration site.
	Refs map[string]struct{}
}

// SemanticConflict is one symbol-level collision between two branches.
type SemanticConflict struct {
	// Kind is "direct" (both branches change the same symbol) or "dependency"
	// (one branch changes a symbol the other references).
	Kind   string
	Symbol string
}

const (
	kindDirect     = "direct"
	kindDependency = "dependency"
)

// BuildSymbolGraph parses the files in files (keyed by path) and merges them
// into one graph. Each file is dispatched to the parser registered for its
// extension; files in unsupported languages and files that fail to parse are
// skipped, so a partial or mid-edit branch still yields a usable graph.
func BuildSymbolGraph(files map[string][]byte) SymbolGraph {
	graph := SymbolGraph{Defines: map[string]struct{}{}, Refs: map[string]struct{}{}}
	for path, src := range files {
		parser, ok := parserForPath(path)
		if !ok {
			continue
		}
		parser.parse(&graph, src)
	}
	return graph
}

// SemanticConflicts reports the symbol-level collisions between branches a and
// b: symbols both define (direct), and symbols one defines while the other
// references (dependency). The result is sorted for stable output.
func SemanticConflicts(a, b SymbolGraph) []SemanticConflict {
	var conflicts []SemanticConflict
	seen := map[string]struct{}{}

	// Direct: both branches change the same symbol.
	for sym := range a.Defines {
		if _, ok := b.Defines[sym]; ok {
			conflicts = append(conflicts, SemanticConflict{Kind: kindDirect, Symbol: sym})
			seen[sym] = struct{}{}
		}
	}

	// Dependency: a changes a symbol b uses, or vice versa. A direct conflict
	// already subsumes a dependency one, so skip symbols already reported.
	dependency := func(defs, refs map[string]struct{}) {
		for sym := range defs {
			if _, isRef := refs[sym]; !isRef {
				continue
			}
			if _, dup := seen[sym]; dup {
				continue
			}
			conflicts = append(conflicts, SemanticConflict{Kind: kindDependency, Symbol: sym})
			seen[sym] = struct{}{}
		}
	}
	dependency(a.Defines, b.Refs)
	dependency(b.Defines, a.Refs)

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Symbol != conflicts[j].Symbol {
			return conflicts[i].Symbol < conflicts[j].Symbol
		}
		return conflicts[i].Kind < conflicts[j].Kind
	})
	return conflicts
}

// BranchSource supplies a branch's changed Go files and their contents at that
// branch. Declared at the point of use; the daemon backs it with the git repo
// (three-dot diff against the base, plus `git show`).
type BranchSource interface {
	ChangedFiles(ctx context.Context, branch string) ([]string, error)
	ReadFile(ctx context.Context, branch, path string) ([]byte, error)
}

// Analyzer computes semantic conflicts between branches from their actual code.
type Analyzer struct {
	src BranchSource
}

// NewAnalyzer builds an Analyzer over a branch source.
func NewAnalyzer(src BranchSource) *Analyzer {
	return &Analyzer{src: src}
}

// Conflicts returns the symbol-level conflicts between two branches' changes.
func (a *Analyzer) Conflicts(ctx context.Context, branchA, branchB string) ([]SemanticConflict, error) {
	graphA, err := a.branchGraph(ctx, branchA)
	if err != nil {
		return nil, fmt.Errorf("conflict.Conflicts: branch %q: %w", branchA, err)
	}
	graphB, err := a.branchGraph(ctx, branchB)
	if err != nil {
		return nil, fmt.Errorf("conflict.Conflicts: branch %q: %w", branchB, err)
	}
	return SemanticConflicts(graphA, graphB), nil
}

// branchGraph reads a branch's changed files in supported languages and builds
// its symbol graph.
func (a *Analyzer) branchGraph(ctx context.Context, branch string) (SymbolGraph, error) {
	files, err := a.src.ChangedFiles(ctx, branch)
	if err != nil {
		return SymbolGraph{}, err
	}
	contents := make(map[string][]byte)
	for _, path := range files {
		if !supported(path) {
			continue
		}
		src, err := a.src.ReadFile(ctx, branch, path)
		if err != nil {
			return SymbolGraph{}, fmt.Errorf("read %s: %w", path, err)
		}
		contents[path] = src
	}
	return BuildSymbolGraph(contents), nil
}
