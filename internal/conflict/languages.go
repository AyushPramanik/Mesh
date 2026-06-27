package conflict

import "regexp"

// langSpec describes one heuristic-parsed language: the extensions it owns, how
// to strip its comments and strings, the declaration patterns that mark a
// defined symbol, and its reserved words. heuristicLangs holds the full set;
// register a new language by appending one entry.
type langSpec struct {
	name          string
	exts          []string
	lineComments  []string
	blockComments [][2]string
	stringDelims  []byte
	defPatterns   []string
	keywords      []string
}

func (s langSpec) parser() heuristicParser {
	patterns := make([]*regexp.Regexp, len(s.defPatterns))
	for i, p := range s.defPatterns {
		patterns[i] = regexp.MustCompile(p)
	}
	kw := make(map[string]struct{}, len(s.keywords))
	for _, k := range s.keywords {
		kw[k] = struct{}{}
	}
	return heuristicParser{
		defPatterns: patterns,
		stripper: &commentStripper{
			lineComments:  s.lineComments,
			blockComments: s.blockComments,
			stringDelims:  s.stringDelims,
		},
		keywords: kw,
	}
}

// heuristicLangs is the registry's language coverage beyond Go. Patterns are
// multiline (the (?m) flag anchors ^ to line starts) and capture the declared
// name in group 1.
var heuristicLangs = []langSpec{
	{
		name:         "python",
		exts:         []string{".py", ".pyi"},
		lineComments: []string{"#"},
		stringDelims: []byte{'"', '\''},
		defPatterns: []string{
			`(?m)^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)`,
			`(?m)^\s*class\s+([A-Za-z_]\w*)`,
			`(?m)^([A-Za-z_]\w*)\s*[:=]`, // module-level binding / annotated constant
		},
		keywords: []string{
			"def", "class", "return", "if", "elif", "else", "for", "while", "import",
			"from", "as", "with", "try", "except", "finally", "raise", "pass", "break",
			"continue", "in", "not", "and", "or", "is", "None", "True", "False", "self",
			"cls", "lambda", "yield", "global", "nonlocal", "del", "assert", "async",
			"await", "print", "len", "range", "str", "int", "dict", "list", "set",
		},
	},
	{
		name:          "javascript",
		exts:          []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"', '\'', '`'},
		defPatterns: []string{
			`(?m)^\s*(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s*\*?\s*([A-Za-z_$][\w$]*)`,
			`(?m)^\s*(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][\w$]*)`,
			`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)`,
			`(?m)^\s*(?:export\s+)?(?:declare\s+)?(?:interface|type|enum)\s+([A-Za-z_$][\w$]*)`,
		},
		keywords: []string{
			"function", "return", "const", "let", "var", "class", "extends", "new",
			"if", "else", "for", "while", "do", "switch", "case", "break", "continue",
			"import", "export", "from", "default", "async", "await", "yield", "this",
			"super", "typeof", "instanceof", "in", "of", "try", "catch", "finally",
			"throw", "delete", "void", "null", "undefined", "true", "false", "interface",
			"type", "enum", "public", "private", "protected", "readonly", "static",
			"abstract", "implements", "namespace", "declare", "as", "get", "set",
		},
	},
	{
		name:          "java",
		exts:          []string{".java"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"', '\''},
		defPatterns: []string{
			`(?m)\b(?:class|interface|enum|record)\s+([A-Za-z_]\w*)`,
			// Method/field: visibility (and optional modifiers) then type then name.
			`(?m)^\s*(?:public|private|protected)\s+(?:static\s+|final\s+|abstract\s+|synchronized\s+|native\s+)*[\w<>\[\],.\s]+?\s+([A-Za-z_]\w*)\s*[(=]`,
		},
		keywords: []string{
			"class", "interface", "enum", "record", "extends", "implements", "new",
			"public", "private", "protected", "static", "final", "abstract", "void",
			"return", "if", "else", "for", "while", "do", "switch", "case", "break",
			"continue", "try", "catch", "finally", "throw", "throws", "import", "package",
			"this", "super", "instanceof", "null", "true", "false", "int", "long", "short",
			"byte", "char", "boolean", "float", "double", "String", "synchronized",
			"volatile", "transient", "native", "default", "var",
		},
	},
	{
		name:          "rust",
		exts:          []string{".rs"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"'},
		defPatterns: []string{
			`(?m)\bfn\s+([A-Za-z_]\w*)`,
			`(?m)\b(?:struct|enum|trait|union)\s+([A-Za-z_]\w*)`,
			`(?m)\b(?:type|const|static)\s+([A-Za-z_]\w*)`,
			`(?m)\bmacro_rules!\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"fn", "let", "mut", "struct", "enum", "trait", "impl", "type", "const",
			"static", "pub", "use", "mod", "match", "if", "else", "for", "while", "loop",
			"return", "self", "Self", "where", "as", "ref", "move", "dyn", "async",
			"await", "true", "false", "crate", "super", "break", "continue", "in", "unsafe",
		},
	},
	{
		name:         "ruby",
		exts:         []string{".rb"},
		lineComments: []string{"#"},
		// =begin/=end block comments are rare; line comments cover the common case.
		stringDelims: []byte{'"', '\''},
		defPatterns: []string{
			`(?m)\bdef\s+(?:self\.)?([A-Za-z_]\w*[!?=]?)`,
			`(?m)\b(?:class|module)\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"def", "end", "class", "module", "if", "elsif", "else", "unless", "while",
			"until", "for", "do", "begin", "rescue", "ensure", "return", "yield", "self",
			"nil", "true", "false", "and", "or", "not", "then", "case", "when", "require",
			"require_relative", "attr_accessor", "attr_reader", "attr_writer", "new",
			"puts", "in", "next", "break", "super",
		},
	},
}
