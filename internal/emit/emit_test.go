package emit

import (
	"strings"
	"testing"

	"github.com/dlcuy22/novel/internal/parser"
)

// emitOK parses src and emits Lua, failing on any parse error.
func emitOK(t *testing.T, src string) string {
	t.Helper()
	p := parser.New(src)
	f := p.ParseFile()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	return New().File(f)
}

func TestEmitFunctionAndCall(t *testing.T) {
	lua := emitOK(t, `fn add(a int, b int) int {
    ret a + b
}`)
	if !strings.Contains(lua, "function add(a, b)") {
		t.Errorf("missing function decl:\n%s", lua)
	}
	if !strings.Contains(lua, "return (a + b)") {
		t.Errorf("missing return expr:\n%s", lua)
	}
}

func TestEmitMainDriver(t *testing.T) {
	// main() must be lowered as a green thread + drained, or spawned work
	// would never run (language-spec.md §13.2).
	lua := emitOK(t, `fn main() {
}`)
	if !strings.Contains(lua, "novel.spawn(main)") {
		t.Errorf("main not spawned:\n%s", lua)
	}
	if !strings.Contains(lua, "novel.run()") {
		t.Errorf("scheduler not drained:\n%s", lua)
	}
}

func TestEmitConcurrencyBuiltins(t *testing.T) {
	lua := emitOK(t, `fn main() {
    let ch = chan<int>(4)
    spawn worker(ch)
    let v = recv(ch)
    ch <- v
}`)
	checks := []string{
		"novel.chan(4)",
		"novel.spawn(function() worker(ch) end)",
		"novel.recv(ch)",
		"novel.send(ch, v)",
	}
	for _, c := range checks {
		if !strings.Contains(lua, c) {
			t.Errorf("missing %q in:\n%s", c, lua)
		}
	}
}

func TestEmitClassicForScopesLocal(t *testing.T) {
	// The induction var must be a loop-scoped local, never a leaked global.
	lua := emitOK(t, `fn main() {
    for i = 0; i < 3; i++ {
        f(i)
    }
}`)
	if !strings.Contains(lua, "local i = 0") {
		t.Errorf("induction var not declared local:\n%s", lua)
	}
	if strings.Contains(lua, "\ni = 0") {
		t.Errorf("induction var leaked as global:\n%s", lua)
	}
}

func TestEmitModuleCallUsesDot(t *testing.T) {
	// Imported module functions use dot-calls (no implicit self).
	lua := emitOK(t, `import std.io
fn main() {
    io.println("hi")
}`)
	if !strings.Contains(lua, `io.println("hi")`) {
		t.Errorf("module call should use dot:\n%s", lua)
	}
}

func TestEmitContinueUsesGoto(t *testing.T) {
	// continue lowers to a goto plus an end-of-body label (Lua 5.1 has no
	// continue keyword). A loop without continue must not emit a label.
	lua := emitOK(t, `fn main() {
    for n in xs {
        if n == 0 {
            continue
        }
        f(n)
    }
}`)
	if !strings.Contains(lua, "goto __continue_1") {
		t.Errorf("continue not lowered to goto:\n%s", lua)
	}
	if !strings.Contains(lua, "::__continue_1::") {
		t.Errorf("missing continue label:\n%s", lua)
	}

	plain := emitOK(t, `fn main() {
    for n in xs {
        f(n)
    }
}`)
	if strings.Contains(plain, "::__continue") {
		t.Errorf("loop without continue should not emit a label:\n%s", plain)
	}
}

func TestEmitNestedContinueUniqueLabels(t *testing.T) {
	// Nested loops that each use continue get distinct labels so a goto never
	// crosses loops.
	lua := emitOK(t, `fn main() {
    for i in a {
        if i == 0 {
            continue
        }
        for j in b {
            if j == 0 {
                continue
            }
            f(i, j)
        }
    }
}`)
	if !strings.Contains(lua, "__continue_1") || !strings.Contains(lua, "__continue_2") {
		t.Errorf("nested loops should get distinct labels:\n%s", lua)
	}
}

func TestEmitPositionalStructLitNamesFields(t *testing.T) {
	// Positional struct literals must map each value to its declared field
	// name, not emit a bare array-style table (which the runtime can't read).
	lua := emitOK(t, `type Rect struct {
    width  float64
    height float64
}
fn main() {
    let r = Rect{ 2.0, 5.0 }
}`)
	if !strings.Contains(lua, "width = 2.0") || !strings.Contains(lua, "height = 5.0") {
		t.Errorf("positional fields not named:\n%s", lua)
	}
}

func TestEmitMapLiteral(t *testing.T) {
	// map[K]V{...} lowers to a Lua table with bracketed keys.
	lua := emitOK(t, `fn main() {
    let m = map[string]int{ "a": 1 }
}`)
	if !strings.Contains(lua, `["a"] = 1`) {
		t.Errorf("map literal not lowered to bracketed keys:\n%s", lua)
	}
}
