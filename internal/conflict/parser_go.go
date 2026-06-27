package conflict

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// goParser parses Go with the standard library's own parser (pure Go, no CGO).
// It is the highest-fidelity parser in the registry: it walks a real AST rather
// than matching declaration patterns, so it distinguishes definition sites from
// references precisely.
type goParser struct{}

func (goParser) parse(graph *SymbolGraph, src []byte) {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return
	}

	// defSites are the identifiers that are definition names, so they are not
	// also counted as references.
	defSites := map[*ast.Ident]struct{}{}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			graph.Defines[d.Name.Name] = struct{}{}
			defSites[d.Name] = struct{}{}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					graph.Defines[s.Name.Name] = struct{}{}
					defSites[s.Name] = struct{}{}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						graph.Defines[name.Name] = struct{}{}
						defSites[name] = struct{}{}
					}
				}
			}
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if _, isDef := defSites[id]; !isDef {
				graph.Refs[id.Name] = struct{}{}
			}
		}
		return true
	})
}
