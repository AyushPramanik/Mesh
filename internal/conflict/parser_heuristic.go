package conflict

import "regexp"

// heuristicParser extracts symbols from languages Mesh has no native Go parser
// for. It does not build a real AST: it strips comments and string literals,
// matches top-level declaration patterns to find defined symbols, and treats
// every remaining identifier as a reference. This is deliberately approximate —
// name-based matching without type or scope resolution — which is exactly the
// fidelity the best-effort intent contract calls for. The same approach drives
// the Go path's symbol matching too; the only difference is Go gets a precise
// AST to extract from.
//
// A tree-sitter-backed parser can replace any language here behind the
// symbolParser interface without changing the conflict pipeline.
type heuristicParser struct {
	// defPatterns each capture a declared symbol name in group 1.
	defPatterns []*regexp.Regexp
	// stripper removes comments and string/char literals before extraction so
	// their contents are not mistaken for identifiers.
	stripper *commentStripper
	// keywords are language reserved words excluded from references to cut noise.
	keywords map[string]struct{}
}

// identRE matches an identifier token across the supported languages. Ruby's
// trailing ? and ! on method names are handled in its declaration patterns; for
// reference purposes the bare name is sufficient.
var identRE = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

func (h heuristicParser) parse(graph *SymbolGraph, src []byte) {
	clean := h.stripper.strip(src)

	for _, re := range h.defPatterns {
		for _, m := range re.FindAllSubmatch(clean, -1) {
			if len(m) > 1 && len(m[1]) > 0 {
				graph.Defines[string(m[1])] = struct{}{}
			}
		}
	}

	for _, tok := range identRE.FindAll(clean, -1) {
		name := string(tok)
		if _, isKeyword := h.keywords[name]; isKeyword {
			continue
		}
		graph.Refs[name] = struct{}{}
	}
}

// commentStripper blanks out comments and string literals for a language so
// their contents do not pollute symbol extraction. It replaces removed spans
// with spaces of equal length to keep declaration patterns line-anchored.
type commentStripper struct {
	lineComments  []string
	blockComments [][2]string
	stringDelims  []byte
}

func (c *commentStripper) strip(src []byte) []byte {
	if c == nil {
		return src
	}
	out := make([]byte, len(src))
	copy(out, src)

	blank := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}

	for i := 0; i < len(out); {
		// Block comments.
		if open, close, n := c.matchBlock(out, i); n > 0 {
			end := indexFrom(out, close, i+n)
			if end < 0 {
				blank(i, len(out))
				break
			}
			blank(i, end+len(close))
			i = end + len(close)
			_ = open
			continue
		}
		// Line comments.
		if marker, n := c.matchLine(out, i); n > 0 {
			end := indexByteFrom(out, '\n', i)
			if end < 0 {
				end = len(out)
			}
			blank(i, end)
			i = end
			_ = marker
			continue
		}
		// String / char literals (no escaping subtleties: blank to the next
		// matching delimiter on the same line, good enough for symbol extraction).
		if c.isStringDelim(out[i]) {
			delim := out[i]
			end := i + 1
			for end < len(out) && out[end] != delim && out[end] != '\n' {
				if out[end] == '\\' {
					end++
				}
				end++
			}
			blank(i, end+1)
			if end >= len(out) {
				break
			}
			i = end + 1
			continue
		}
		i++
	}
	return out
}

func (c *commentStripper) matchBlock(s []byte, i int) (open, close string, n int) {
	for _, bc := range c.blockComments {
		if hasPrefixAt(s, i, bc[0]) {
			return bc[0], bc[1], len(bc[0])
		}
	}
	return "", "", 0
}

func (c *commentStripper) matchLine(s []byte, i int) (marker string, n int) {
	for _, lc := range c.lineComments {
		if hasPrefixAt(s, i, lc) {
			return lc, len(lc)
		}
	}
	return "", 0
}

func (c *commentStripper) isStringDelim(b byte) bool {
	for _, d := range c.stringDelims {
		if b == d {
			return true
		}
	}
	return false
}

func hasPrefixAt(s []byte, i int, p string) bool {
	if i+len(p) > len(s) {
		return false
	}
	return string(s[i:i+len(p)]) == p
}

func indexFrom(s []byte, sub string, from int) int {
	for i := from; i+len(sub) <= len(s); i++ {
		if string(s[i:i+len(sub)]) == sub {
			return i
		}
	}
	return -1
}

func indexByteFrom(s []byte, b byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
