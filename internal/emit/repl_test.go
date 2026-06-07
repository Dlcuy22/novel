package emit

import (
	"strings"
	"testing"

	"github.com/dlcuy22/novel/internal/parser"
)

// replOK parses one REPL line and emits its chunk using the given (persistent)
// emitter, failing on any parse error.
func replOK(t *testing.T, e *Emitter, src string) string {
	t.Helper()
	p := parser.New(src)
	u := p.ParseRepl()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors for %q: %v", src, p.Errors())
	}
	return e.Repl(u)
}

func TestReplBareExpressionAutoPrints(t *testing.T) {
	lua := replOK(t, NewRepl(), "1 + 1")
	if !strings.Contains(lua, "novel.replprint((1 + 1))") {
		t.Errorf("bare expression not wrapped in replprint:\n%s", lua)
	}
}

func TestReplLetEmitsGlobal(t *testing.T) {
	// A let must persist across lines, so it emits as a session global (no
	// `local`).
	lua := replOK(t, NewRepl(), "let x = 10")
	if strings.Contains(lua, "local x") {
		t.Errorf("REPL let should not be a local:\n%s", lua)
	}
	if !strings.Contains(lua, "x = 10") {
		t.Errorf("REPL let should assign a global:\n%s", lua)
	}
}

func TestReplStructEmitsGlobal(t *testing.T) {
	lua := replOK(t, NewRepl(), "type Rect struct { width float64 }")
	if strings.Contains(lua, "local Rect") {
		t.Errorf("REPL struct should not be a local:\n%s", lua)
	}
	if !strings.Contains(lua, "Rect = {}") {
		t.Errorf("REPL struct should assign a global table:\n%s", lua)
	}
}

func TestReplImportEmitsGlobalRequire(t *testing.T) {
	lua := replOK(t, NewRepl(), `import std.math`)
	if !strings.Contains(lua, `math = require("std/math")`) {
		t.Errorf("REPL import should bind a global require:\n%s", lua)
	}
	if strings.Contains(lua, "local math") {
		t.Errorf("REPL import should not be a local:\n%s", lua)
	}
}

func TestReplSessionRemembersStructFields(t *testing.T) {
	// The same emitter, used across lines, must remember a struct declared on an
	// earlier line so a later positional literal resolves field names.
	e := NewRepl()
	replOK(t, e, "type Rect struct { width float64\nheight float64 }")
	lua := replOK(t, e, "let r = Rect{ 2.0, 5.0 }")
	if !strings.Contains(lua, "width = 2.0") || !strings.Contains(lua, "height = 5.0") {
		t.Errorf("positional literal did not resolve remembered fields:\n%s", lua)
	}
}

func TestReplSessionRemembersImportsForDotCall(t *testing.T) {
	// After an import on one line, a call on the module on a later line must use
	// a dot-call (module function), not a colon-call (method).
	e := NewRepl()
	replOK(t, e, `import std.io`)
	lua := replOK(t, e, `io.println("hi")`)
	if !strings.Contains(lua, `io.println("hi")`) {
		t.Errorf("module call should use dot, got:\n%s", lua)
	}
}
