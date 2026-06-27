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
	{
		name:          "c/c++",
		exts:          []string{".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"', '\''},
		defPatterns: []string{
			`(?:class|struct|enum|union|namespace)\s+([A-Za-z_]\w*)`,
			// Function definition: a return type then name(args) then an opening
			// brace. Requiring { (not ;) skips bare declarations and prototypes.
			`(?m)^[A-Za-z_][\w\s\*&:<>,]*?\b([A-Za-z_]\w*)\s*\([^;{}]*\)\s*(?:const\s*)?\{`,
			`(?m)\busing\s+([A-Za-z_]\w*)\s*=`,
		},
		keywords: []string{
			"class", "struct", "enum", "union", "namespace", "template", "typename",
			"public", "private", "protected", "virtual", "override", "final", "static",
			"const", "constexpr", "inline", "explicit", "friend", "operator", "new",
			"delete", "return", "if", "else", "for", "while", "do", "switch", "case",
			"break", "continue", "goto", "try", "catch", "throw", "using", "typedef",
			"sizeof", "nullptr", "true", "false", "this", "void", "int", "char", "bool",
			"float", "double", "long", "short", "unsigned", "signed", "auto", "extern",
			"include", "define", "ifdef", "ifndef", "endif", "pragma", "std",
		},
	},
	{
		name:          "csharp",
		exts:          []string{".cs"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"', '\''},
		defPatterns: []string{
			`(?:class|interface|enum|struct|record)\s+([A-Za-z_]\w*)`,
			`(?m)\bnamespace\s+([A-Za-z_][\w.]*)`,
			`(?m)^\s*(?:public|private|protected|internal)\s+(?:static\s+|virtual\s+|override\s+|async\s+|sealed\s+|abstract\s+|partial\s+)*[\w<>\[\],.?\s]+?\s+([A-Za-z_]\w*)\s*[(]`,
		},
		keywords: []string{
			"class", "interface", "enum", "struct", "record", "namespace", "using",
			"public", "private", "protected", "internal", "static", "virtual", "override",
			"abstract", "sealed", "partial", "async", "await", "var", "new", "return",
			"if", "else", "for", "foreach", "while", "do", "switch", "case", "break",
			"continue", "try", "catch", "finally", "throw", "this", "base", "null",
			"true", "false", "void", "int", "string", "bool", "double", "float", "long",
			"object", "get", "set", "in", "out", "ref", "is", "as", "default",
		},
	},
	{
		name:          "php",
		exts:          []string{".php"},
		lineComments:  []string{"//", "#"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"', '\''},
		defPatterns: []string{
			`(?m)\bfunction\s+([A-Za-z_]\w*)`,
			`(?:class|interface|trait|enum)\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"function", "class", "interface", "trait", "enum", "extends", "implements",
			"new", "public", "private", "protected", "static", "final", "abstract",
			"const", "return", "if", "else", "elseif", "for", "foreach", "while", "do",
			"switch", "case", "break", "continue", "try", "catch", "finally", "throw",
			"use", "namespace", "this", "self", "parent", "null", "true", "false", "echo",
			"print", "array", "as", "global", "var", "fn", "match", "yield",
		},
	},
	{
		name:          "swift",
		exts:          []string{".swift"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"'},
		defPatterns: []string{
			`(?m)\bfunc\s+([A-Za-z_]\w*)`,
			`(?:class|struct|enum|protocol|extension|actor)\s+([A-Za-z_]\w*)`,
			`(?m)^\s*(?:public\s+|private\s+|internal\s+|fileprivate\s+|open\s+|static\s+)*(?:let|var)\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"func", "class", "struct", "enum", "protocol", "extension", "actor", "let",
			"var", "if", "else", "guard", "for", "while", "repeat", "switch", "case",
			"break", "continue", "return", "self", "Self", "super", "init", "deinit",
			"public", "private", "internal", "fileprivate", "open", "static", "final",
			"override", "import", "throws", "throw", "try", "catch", "do", "defer", "in",
			"nil", "true", "false", "where", "as", "is", "some", "any", "async", "await",
		},
	},
	{
		name:          "kotlin",
		exts:          []string{".kt", ".kts"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"'},
		defPatterns: []string{
			`(?m)\bfun\s+(?:<[^>]*>\s*)?([A-Za-z_]\w*)`,
			`(?:class|interface|object)\s+([A-Za-z_]\w*)`,
			`(?m)^\s*(?:public\s+|private\s+|internal\s+|protected\s+|const\s+)*(?:val|var)\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"fun", "class", "interface", "object", "val", "var", "if", "else", "when",
			"for", "while", "do", "return", "this", "super", "is", "as", "in", "import",
			"package", "public", "private", "internal", "protected", "open", "abstract",
			"final", "override", "companion", "data", "sealed", "enum", "const", "lateinit",
			"by", "try", "catch", "finally", "throw", "null", "true", "false", "suspend",
		},
	},
	{
		name:          "scala",
		exts:          []string{".scala", ".sc"},
		lineComments:  []string{"//"},
		blockComments: [][2]string{{"/*", "*/"}},
		stringDelims:  []byte{'"'},
		defPatterns: []string{
			`(?m)\bdef\s+([A-Za-z_]\w*)`,
			`(?:class|object|trait)\s+([A-Za-z_]\w*)`,
			`(?m)^\s*(?:val|var)\s+([A-Za-z_]\w*)`,
		},
		keywords: []string{
			"def", "class", "object", "trait", "val", "var", "if", "else", "match",
			"case", "for", "while", "do", "return", "this", "super", "extends", "with",
			"import", "package", "private", "protected", "override", "final", "abstract",
			"sealed", "implicit", "lazy", "new", "type", "yield", "try", "catch", "finally",
			"throw", "null", "true", "false", "of",
		},
	},
	{
		name:         "shell",
		exts:         []string{".sh", ".bash", ".zsh"},
		lineComments: []string{"#"},
		stringDelims: []byte{'"', '\''},
		defPatterns: []string{
			`(?m)^\s*function\s+([A-Za-z_]\w*)`,
			`(?m)^\s*([A-Za-z_]\w*)\s*\(\s*\)`,
		},
		keywords: []string{
			"function", "if", "then", "elif", "else", "fi", "for", "while", "until", "do",
			"done", "case", "esac", "in", "return", "local", "export", "readonly", "declare",
			"echo", "exit", "break", "continue", "select", "time", "set", "unset", "true", "false",
		},
	},
}
