package conflict

import (
	"path"
	"sort"
	"strings"
)

// symbolParser extracts the symbols a single source file defines and references
// into a SymbolGraph. Implementations are language-specific but all target the
// same graph shape, so SemanticConflicts is language-agnostic.
//
// A parser must be side-effect-free and tolerant of partial or invalid input: a
// mid-edit file that does not fully parse should still yield whatever symbols
// are recoverable, never panic. This matches intents being best-effort
// predictions rather than locks.
type symbolParser interface {
	// parse merges the file's defined and referenced symbols into graph.
	parse(graph *SymbolGraph, src []byte)
}

// registry maps a lowercase file extension (including the leading dot) to the
// parser for that language. It is built once at package init and is read-only
// thereafter — no mutable package-level state. Add a language by registering its
// parser here; the rest of the pipeline (BuildSymbolGraph, Analyzer) discovers
// it through parserForPath with no further changes.
var registry = buildRegistry()

func buildRegistry() map[string]symbolParser {
	reg := map[string]symbolParser{
		".go": goParser{},
	}
	// Heuristic, pure-Go parsers for the languages agents most commonly write.
	// Tree-sitter-backed parsers can replace any of these later behind the same
	// symbolParser interface without touching callers.
	for _, spec := range heuristicLangs {
		p := spec.parser()
		for _, ext := range spec.exts {
			reg[ext] = p
		}
	}
	return reg
}

// parserForPath returns the parser for path's extension, if a language is
// registered for it. Extension matching is case-insensitive.
func parserForPath(p string) (symbolParser, bool) {
	parser, ok := registry[strings.ToLower(path.Ext(p))]
	return parser, ok
}

// supported reports whether a parser is registered for path's extension.
func supported(p string) bool {
	_, ok := parserForPath(p)
	return ok
}

// SupportedExtensions returns the sorted set of file extensions for which a
// language parser is registered (each including the leading dot). It lets
// callers (and tests) see the analyzer's language coverage without reaching into
// the registry.
func SupportedExtensions() []string {
	exts := make([]string, 0, len(registry))
	for ext := range registry {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}
