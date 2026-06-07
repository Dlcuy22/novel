package compiler

import (
	"strings"
	"testing"
)

func TestReplSessionBareExpression(t *testing.T) {
	s := NewReplSession()
	res := s.Compile("1 + 1")
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, "novel.replprint((1 + 1))") {
		t.Errorf("expected auto-print chunk, got:\n%s", res.Lua)
	}
}

func TestReplSessionPersistsAcrossLines(t *testing.T) {
	s := NewReplSession()
	// Declare a struct, then use a positional literal on a later line.
	if r := s.Compile("type Rect struct { width float64\nheight float64 }"); r.HasErrors() {
		t.Fatalf("decl errors: %v", r.Diagnostics)
	}
	res := s.Compile("let r = Rect{ 2.0, 5.0 }")
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, "width = 2.0") {
		t.Errorf("session did not remember struct fields:\n%s", res.Lua)
	}
}

func TestReplSessionParseErrorReported(t *testing.T) {
	s := NewReplSession()
	res := s.Compile("let =")
	if !res.HasErrors() {
		t.Fatal("expected a parse diagnostic")
	}
}

func TestReplSessionResetClearsState(t *testing.T) {
	s := NewReplSession()
	s.Compile("type Rect struct { width float64 }")
	s.Reset()
	// After reset the struct is forgotten, so a positional literal can't resolve
	// field names and falls back to bare positional slots.
	res := s.Compile("let r = Rect{ 2.0 }")
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if strings.Contains(res.Lua, "width = 2.0") {
		t.Errorf("reset should have cleared remembered struct fields:\n%s", res.Lua)
	}
}

func TestNeedsMoreInput(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"1 + 1", false},
		{"let x = 10", false},
		{"type Rect struct {", true},
		{"type Rect struct {\n  width float64\n}", false},
		{"f(1, 2", true},
		{"f(1, 2)", false},
		{"let xs = [1, 2", true},
		{`"} unbalanced in string is fine"`, false},
		{"// { in a comment is fine", false},
	}
	for _, c := range cases {
		if got := NeedsMoreInput(c.src); got != c.want {
			t.Errorf("NeedsMoreInput(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}
