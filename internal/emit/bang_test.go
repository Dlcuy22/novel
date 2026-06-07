package emit

import (
	"strings"
	"testing"
)

func TestEmitBangPropagationPrelude(t *testing.T) {
	// `f()!` hoists into a temp + early-return check; the call site uses the
	// temp, and the early return zero-fills non-error results then the error.
	lua := emitOK(t, `fn risky() (int, error) {
    ret 0, error("x")
}
fn caller() (int, error) {
    let v = risky()!
    ret v, nil
}`)
	checks := []string{
		"local __bang_1, __bangerr_1 = risky()",
		"if __bangerr_1 ~= nil then",
		"return nil, __bangerr_1",
		"local v = __bang_1",
	}
	for _, c := range checks {
		if !strings.Contains(lua, c) {
			t.Errorf("missing %q in:\n%s", c, lua)
		}
	}
}

func TestEmitBangZeroFillsMultipleResults(t *testing.T) {
	// A function returning (int, string, error) must early-return two nils then
	// the error.
	lua := emitOK(t, `fn risky() (int, error) {
    ret 0, error("x")
}
fn caller() (int, string, error) {
    let v = risky()!
    ret v, "ok", nil
}`)
	if !strings.Contains(lua, "return nil, nil, __bangerr_1") {
		t.Errorf("expected two zero-fills before the error:\n%s", lua)
	}
}

func TestEmitBareBangStatementEmitsNoDanglingTemp(t *testing.T) {
	// `f()!` as a statement is fully realized by its prelude; no bare temp line.
	lua := emitOK(t, `fn risky() (int, error) {
    ret 1, nil
}
fn caller() (int, error) {
    risky()!
    ret 99, nil
}`)
	// The prelude is present...
	if !strings.Contains(lua, "local __bang_1, __bangerr_1 = risky()") {
		t.Errorf("missing prelude:\n%s", lua)
	}
	// ...but no line consists of just the temp (which would be invalid Lua).
	for _, line := range strings.Split(lua, "\n") {
		if strings.TrimSpace(line) == "__bang_1" {
			t.Errorf("dangling bare temp statement in:\n%s", lua)
		}
	}
}

func TestEmitNestedBangOrdersPreludes(t *testing.T) {
	// inner!()  feeding outer!() must hoist the inner prelude before the outer.
	lua := emitOK(t, `fn inner() (int, error) {
    ret 1, nil
}
fn outer(n int) (int, error) {
    ret n, nil
}
fn caller() (int, error) {
    let v = outer(inner()!)!
    ret v, nil
}`)
	innerIdx := strings.Index(lua, "= inner()")
	outerIdx := strings.Index(lua, "= outer(")
	if innerIdx < 0 || outerIdx < 0 {
		t.Fatalf("expected both preludes in:\n%s", lua)
	}
	if innerIdx > outerIdx {
		t.Errorf("inner prelude should precede outer prelude:\n%s", lua)
	}
}
