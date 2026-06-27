package conflict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSymbolGraph_Python(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"svc.py": []byte(`import os

TIMEOUT = 30

class Service:
    def start(self):
        return connect(TIMEOUT)

def connect(timeout):
    return True
`),
	})
	assert.Contains(t, g.Defines, "Service")
	assert.Contains(t, g.Defines, "start")
	assert.Contains(t, g.Defines, "connect")
	assert.Contains(t, g.Defines, "TIMEOUT")
	// Referenced from start, defined elsewhere.
	assert.Contains(t, g.Refs, "connect")
	// Comment/keyword noise is excluded.
	assert.NotContains(t, g.Refs, "class")
	assert.NotContains(t, g.Refs, "def")
}

func TestBuildSymbolGraph_TypeScript(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"api.ts": []byte(`export interface User { id: string }

export async function fetchUser(id: string): Promise<User> {
  return store.get(id); // store referenced, not defined
}

export const MAX = 100;
class Repo {}
`),
	})
	assert.Contains(t, g.Defines, "User")
	assert.Contains(t, g.Defines, "fetchUser")
	assert.Contains(t, g.Defines, "MAX")
	assert.Contains(t, g.Defines, "Repo")
	assert.Contains(t, g.Refs, "store")
}

func TestBuildSymbolGraph_Rust(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"lib.rs": []byte(`pub struct Config { pub port: u16 }

pub fn serve(cfg: Config) -> bool {
    bind(cfg.port) // bind referenced
}

const DEFAULT_PORT: u16 = 8080;
`),
	})
	assert.Contains(t, g.Defines, "Config")
	assert.Contains(t, g.Defines, "serve")
	assert.Contains(t, g.Defines, "DEFAULT_PORT")
	assert.Contains(t, g.Refs, "bind")
}

func TestBuildSymbolGraph_Ruby(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"user.rb": []byte(`class User
  def save
    persist(self) # persist referenced
  end

  def self.find(id)
    id
  end
end
`),
	})
	assert.Contains(t, g.Defines, "User")
	assert.Contains(t, g.Defines, "save")
	assert.Contains(t, g.Defines, "find")
	assert.Contains(t, g.Refs, "persist")
}

func TestBuildSymbolGraph_Java(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"Account.java": []byte(`public class Account {
    public void withdraw(int amount) {
        validate(amount); // validate referenced
    }
}
`),
	})
	assert.Contains(t, g.Defines, "Account")
	assert.Contains(t, g.Defines, "withdraw")
	assert.Contains(t, g.Refs, "validate")
}

func TestBuildSymbolGraph_CommentsAndStringsExcluded(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"x.py": []byte(`# def ghost(): nothing here
msg = "def phantom and class spectre"
def real():
    pass
`),
	})
	assert.Contains(t, g.Defines, "real")
	assert.NotContains(t, g.Defines, "ghost")
	assert.NotContains(t, g.Defines, "phantom")
	// Identifiers that only ever appear inside a comment or string are not refs.
	assert.NotContains(t, g.Refs, "ghost")
	assert.NotContains(t, g.Refs, "phantom")
	assert.NotContains(t, g.Refs, "spectre")
}

func TestBuildSymbolGraph_MixedLanguagesInOneGraph(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"a.go": []byte("package p\nfunc GoFn() {}"),
		"b.py": []byte("def py_fn():\n    pass"),
		"c.ts": []byte("export function tsFn() {}"),
	})
	assert.Contains(t, g.Defines, "GoFn")
	assert.Contains(t, g.Defines, "py_fn")
	assert.Contains(t, g.Defines, "tsFn")
}

func TestSemanticConflicts_CrossFileDependency_Python(t *testing.T) {
	// Branch A redefines authenticate; branch B (different file) calls it.
	a := BuildSymbolGraph(map[string][]byte{
		"auth.py": []byte("def authenticate(token):\n    return token != ''"),
	})
	b := BuildSymbolGraph(map[string][]byte{
		"handler.py": []byte("def handle():\n    return authenticate('t')"),
	})

	conflicts := SemanticConflicts(a, b)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "authenticate", conflicts[0].Symbol)
	assert.Equal(t, kindDependency, conflicts[0].Kind)
}

func TestBuildSymbolGraph_UnsupportedExtensionSkipped(t *testing.T) {
	g := BuildSymbolGraph(map[string][]byte{
		"notes.md":  []byte("# def heading\nclass of 2024"),
		"data.json": []byte(`{"def": 1, "class": 2}`),
	})
	assert.Empty(t, g.Defines)
}

func TestSupportedExtensions_CoversRegisteredLanguages(t *testing.T) {
	exts := SupportedExtensions()
	for _, want := range []string{".go", ".py", ".js", ".ts", ".tsx", ".java", ".rs", ".rb"} {
		assert.Contains(t, exts, want, "expected %s to be supported", want)
	}
}

func TestParserForPath_CaseInsensitive(t *testing.T) {
	_, ok := parserForPath("Main.PY")
	assert.True(t, ok)
}
