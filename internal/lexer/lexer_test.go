package lexer

import (
	"testing"

	"github.com/dlcuy22/novel/internal/token"
)

func typesOf(toks []token.Token) []token.Type {
	out := make([]token.Type, 0, len(toks))
	for _, t := range toks {
		out = append(out, t.Type)
	}
	return out
}

func TestLexBasics(t *testing.T) {
	src := `import std.io
// a comment
fn add(a int, b int) int {
    let x = 5
    ret a + b
}`
	want := []token.Type{
		token.IMPORT, token.IDENT, token.DOT, token.IDENT,
		token.FN, token.IDENT, token.LPAREN,
		token.IDENT, token.IDENT, token.COMMA,
		token.IDENT, token.IDENT, token.RPAREN, token.IDENT, token.LBRACE,
		token.LET, token.IDENT, token.ASSIGN, token.INT,
		token.RETURN, token.IDENT, token.PLUS, token.IDENT,
		token.RBRACE,
		token.EOF,
	}
	got := typesOf(New(src).Tokenize())
	if len(got) != len(want) {
		t.Fatalf("token count: got %d, want %d\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLexOperators(t *testing.T) {
	src := `:= == != <= >= && || << >> <- -> => .. ... += -= *= /= ++ --`
	want := []token.Type{
		token.DECLARE, token.EQ, token.NEQ, token.LE, token.GE,
		token.AND, token.OR, token.SHL, token.SHR, token.ARROW,
		token.RARROW, token.FATARROW, token.DOTDOT, token.ELLIPSIS,
		token.PLUSEQ, token.MINUSEQ, token.STAREQ, token.SLASHEQ,
		token.INC, token.DEC,
		token.EOF,
	}
	got := typesOf(New(src).Tokenize())
	if len(got) != len(want) {
		t.Fatalf("token count: got %d, want %d\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func TestLexStrings(t *testing.T) {
	l := New("\"hi\" `raw` $\"x={n}\"")
	toks := l.Tokenize()
	if toks[0].Type != token.STRING || toks[0].Literal != "hi" {
		t.Errorf("string: got %v %q", toks[0].Type, toks[0].Literal)
	}
	if toks[1].Type != token.RAWSTRING || toks[1].Literal != "raw" {
		t.Errorf("rawstring: got %v %q", toks[1].Type, toks[1].Literal)
	}
	if toks[2].Type != token.INTERPSTR || toks[2].Literal != "x={n}" {
		t.Errorf("interpstring: got %v %q", toks[2].Type, toks[2].Literal)
	}
	if len(l.Errors()) != 0 {
		t.Errorf("unexpected errors: %v", l.Errors())
	}
}

func TestLexStringEscapes(t *testing.T) {
	// Double-quoted strings decode escapes at lex time; raw strings do not.
	cases := []struct {
		src  string
		want string
	}{
		{`"a\tb"`, "a\tb"},
		{`"l1\nl2"`, "l1\nl2"},
		{`"q\"e"`, "q\"e"},
		{`"back\\slash"`, "back\\slash"},
		{"`raw\\tkept`", "raw\\tkept"},
	}
	for _, c := range cases {
		got := New(c.src).Tokenize()[0]
		if got.Literal != c.want {
			t.Errorf("%s: got %q, want %q", c.src, got.Literal, c.want)
		}
	}
}

func TestLexNumberSuffixes(t *testing.T) {
	cases := map[string]token.Type{
		"42":      token.INT,
		"42u":     token.INT,
		"42i64":   token.INT,
		"3.14":    token.FLOAT,
		"3.14f32": token.FLOAT,
		"0xFF":    token.INT,
		"0x3c":    token.INT,
	}
	for src, want := range cases {
		got := New(src).Tokenize()[0]
		if got.Type != want {
			t.Errorf("%q: got %v, want %v", src, got.Type, want)
		}
	}

	// Hex literals must keep their full text (the 0x prefix and all digits),
	// not be truncated at the 'x'.
	hex := New("0xFF").Tokenize()[0]
	if hex.Literal != "0xFF" {
		t.Errorf("hex literal text: got %q, want 0xFF", hex.Literal)
	}
}
