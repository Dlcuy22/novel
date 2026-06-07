// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package lexer turns Novel source text into a stream of tokens.
//
// Purpose:
//
//	Covers the lexical surface in language-spec.md §2: line/block comments,
//	identifiers and keywords, int/float literals (with type suffixes),
//	plain/raw/interpolated strings, and the full operator set.
//
// Key Components:
//   - Lexer, New(): scanner over a single source string
//   - Tokenize(): scans the whole input to an []token.Token ending in EOF
//   - Next(): pulls one token; Errors() returns lexical errors
//
// Dependencies:
//   - internal/token: the token vocabulary this package produces
//
// Note:
//
//	Interpolated strings ($"...{expr}...") are emitted as a single INTERPSTR
//	token; splitting interpolation into sub-expressions is the parser's job.
package lexer

import (
	"strings"

	"github.com/dlcuy22/novel/internal/token"
)

// Lexer scans a single source file.
type Lexer struct {
	src  string
	pos  int // current offset into src
	line int
	col  int
	errs []Error
}

// Error is a lexical error with a source position.
type Error struct {
	Pos token.Position
	Msg string
}

// New returns a Lexer over the given source.
func New(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

// Errors returns any lexical errors collected during scanning.
func (l *Lexer) Errors() []Error { return l.errs }

func (l *Lexer) errorf(p token.Position, msg string) {
	l.errs = append(l.errs, Error{Pos: p, Msg: msg})
}

func (l *Lexer) cur() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peek(n int) byte {
	if l.pos+n >= len(l.src) {
		return 0
	}
	return l.src[l.pos+n]
}

func (l *Lexer) advance() byte {
	c := l.cur()
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *Lexer) position() token.Position {
	return token.Position{Line: l.line, Column: l.col}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isLetter(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isAlnum(c byte) bool { return isLetter(c) || isDigit(c) }

// Tokenize scans the entire source and returns all tokens ending with EOF.
func (l *Lexer) Tokenize() []token.Token {
	var toks []token.Token
	for {
		t := l.Next()
		toks = append(toks, t)
		if t.Type == token.EOF {
			return toks
		}
	}
}

// Next returns the next token in the stream.
func (l *Lexer) Next() token.Token {
	l.skipTrivia()
	pos := l.position()
	c := l.cur()

	switch {
	case c == 0:
		return token.Token{Type: token.EOF, Pos: pos}
	case isLetter(c):
		return l.lexIdent(pos)
	case isDigit(c):
		return l.lexNumber(pos)
	case c == '"':
		return l.lexString(pos)
	case c == '`':
		return l.lexRawString(pos)
	case c == '$' && l.peek(1) == '"':
		return l.lexInterpString(pos)
	}
	return l.lexOperator(pos)
}

func (l *Lexer) skipTrivia() {
	for {
		c := l.cur()
		if isSpace(c) {
			l.advance()
			continue
		}
		// line comment
		if c == '/' && l.peek(1) == '/' {
			for l.cur() != '\n' && l.cur() != 0 {
				l.advance()
			}
			continue
		}
		// block comment
		if c == '/' && l.peek(1) == '*' {
			l.advance()
			l.advance()
			for !(l.cur() == '*' && l.peek(1) == '/') && l.cur() != 0 {
				l.advance()
			}
			l.advance() // *
			l.advance() // /
			continue
		}
		return
	}
}

func (l *Lexer) lexIdent(pos token.Position) token.Token {
	start := l.pos
	for isAlnum(l.cur()) {
		l.advance()
	}
	lit := l.src[start:l.pos]
	return token.Token{Type: token.Lookup(lit), Literal: lit, Pos: pos}
}

func (l *Lexer) lexNumber(pos token.Position) token.Token {
	start := l.pos
	isFloat := false

	// Hex literal: 0x / 0X followed by hex digits. Kept verbatim (Lua accepts
	// the same syntax), so no float handling and no suffix scan.
	if l.cur() == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') {
		l.advance() // 0
		l.advance() // x
		for isHexDigit(l.cur()) {
			l.advance()
		}
		return token.Token{Type: token.INT, Literal: l.src[start:l.pos], Pos: pos}
	}

	for isDigit(l.cur()) {
		l.advance()
	}
	if l.cur() == '.' && isDigit(l.peek(1)) {
		isFloat = true
		l.advance()
		for isDigit(l.cur()) {
			l.advance()
		}
	}
	// numeric suffix: u, i8/i16/i32/i64, f32/f64
	for isAlnum(l.cur()) {
		if l.cur() == 'f' {
			isFloat = true
		}
		l.advance()
	}
	lit := l.src[start:l.pos]
	typ := token.INT
	if isFloat {
		typ = token.FLOAT
	}
	return token.Token{Type: typ, Literal: lit, Pos: pos}
}

func (l *Lexer) lexString(pos token.Position) token.Token {
	l.advance() // opening "
	start := l.pos
	for l.cur() != '"' && l.cur() != 0 {
		if l.cur() == '\\' {
			l.advance()
		}
		l.advance()
	}
	lit := l.src[start:l.pos]
	if l.cur() == 0 {
		l.errorf(pos, "unterminated string literal")
	} else {
		l.advance() // closing "
	}
	// Decode escapes now so the token carries the real characters; the emitter
	// re-encodes them for Lua. Raw strings (backticks) skip this.
	return token.Token{Type: token.STRING, Literal: Unescape(lit), Pos: pos}
}

// Unescape decodes the backslash escapes Novel recognizes in double-quoted and
// interpolated strings (\n \t \r \\ \" \0). An unknown escape is kept verbatim,
// backslash included.
func Unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case '0':
			b.WriteByte(0)
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func (l *Lexer) lexRawString(pos token.Position) token.Token {
	l.advance() // opening `
	start := l.pos
	for l.cur() != '`' && l.cur() != 0 {
		l.advance()
	}
	lit := l.src[start:l.pos]
	if l.cur() == 0 {
		l.errorf(pos, "unterminated raw string literal")
	} else {
		l.advance() // closing `
	}
	return token.Token{Type: token.RAWSTRING, Literal: lit, Pos: pos}
}

func (l *Lexer) lexInterpString(pos token.Position) token.Token {
	l.advance() // $
	l.advance() // opening "
	start := l.pos
	for l.cur() != '"' && l.cur() != 0 {
		if l.cur() == '\\' {
			l.advance()
		}
		l.advance()
	}
	lit := l.src[start:l.pos]
	if l.cur() == 0 {
		l.errorf(pos, "unterminated interpolated string literal")
	} else {
		l.advance() // closing "
	}
	return token.Token{Type: token.INTERPSTR, Literal: lit, Pos: pos}
}

// two reports a two-character operator if the next char matches, else falls
// back to the one-character form.
func (l *Lexer) two(next byte, twoTyp, oneTyp token.Type, pos token.Position) token.Token {
	first := l.advance()
	if l.cur() == next {
		l.advance()
		return token.Token{Type: twoTyp, Literal: string([]byte{first, next}), Pos: pos}
	}
	return token.Token{Type: oneTyp, Literal: string(first), Pos: pos}
}

func (l *Lexer) lexOperator(pos token.Position) token.Token {
	c := l.cur()
	switch c {
	case '+':
		// ++, +=, or +
		l.advance()
		switch l.cur() {
		case '+':
			l.advance()
			return token.Token{Type: token.INC, Literal: "++", Pos: pos}
		case '=':
			l.advance()
			return token.Token{Type: token.PLUSEQ, Literal: "+=", Pos: pos}
		}
		return token.Token{Type: token.PLUS, Literal: "+", Pos: pos}
	case '*':
		return l.two('=', token.STAREQ, token.STAR, pos)
	case '/':
		return l.two('=', token.SLASHEQ, token.SLASH, pos)
	case '%':
		l.advance()
		return token.Token{Type: token.PERCENT, Literal: "%", Pos: pos}
	case '-':
		// --, -=, ->, or -
		l.advance()
		switch l.cur() {
		case '-':
			l.advance()
			return token.Token{Type: token.DEC, Literal: "--", Pos: pos}
		case '=':
			l.advance()
			return token.Token{Type: token.MINUSEQ, Literal: "-=", Pos: pos}
		case '>':
			l.advance()
			return token.Token{Type: token.RARROW, Literal: "->", Pos: pos}
		}
		return token.Token{Type: token.MINUS, Literal: "-", Pos: pos}
	case '=':
		// ==, =>, or =
		l.advance()
		switch l.cur() {
		case '=':
			l.advance()
			return token.Token{Type: token.EQ, Literal: "==", Pos: pos}
		case '>':
			l.advance()
			return token.Token{Type: token.FATARROW, Literal: "=>", Pos: pos}
		}
		return token.Token{Type: token.ASSIGN, Literal: "=", Pos: pos}
	case '!':
		return l.two('=', token.NEQ, token.NOT, pos)
	case '<':
		// <=, <<, <-, or <
		l.advance()
		switch l.cur() {
		case '=':
			l.advance()
			return token.Token{Type: token.LE, Literal: "<=", Pos: pos}
		case '<':
			l.advance()
			return token.Token{Type: token.SHL, Literal: "<<", Pos: pos}
		case '-':
			l.advance()
			return token.Token{Type: token.ARROW, Literal: "<-", Pos: pos}
		}
		return token.Token{Type: token.LT, Literal: "<", Pos: pos}
	case '>':
		// >=, >>, or >
		l.advance()
		switch l.cur() {
		case '=':
			l.advance()
			return token.Token{Type: token.GE, Literal: ">=", Pos: pos}
		case '>':
			l.advance()
			return token.Token{Type: token.SHR, Literal: ">>", Pos: pos}
		}
		return token.Token{Type: token.GT, Literal: ">", Pos: pos}
	case '&':
		return l.two('&', token.AND, token.AMP, pos)
	case '|':
		return l.two('|', token.OR, token.PIPE, pos)
	case '^':
		l.advance()
		return token.Token{Type: token.CARET, Literal: "^", Pos: pos}
	case ':':
		return l.two('=', token.DECLARE, token.COLON, pos)
	case '.':
		// ... , .. , or .
		if l.peek(1) == '.' && l.peek(2) == '.' {
			l.advance()
			l.advance()
			l.advance()
			return token.Token{Type: token.ELLIPSIS, Literal: "...", Pos: pos}
		}
		if l.peek(1) == '.' {
			l.advance()
			l.advance()
			return token.Token{Type: token.DOTDOT, Literal: "..", Pos: pos}
		}
		l.advance()
		return token.Token{Type: token.DOT, Literal: ".", Pos: pos}
	}

	// single-character punctuation
	single := map[byte]token.Type{
		'(': token.LPAREN, ')': token.RPAREN,
		'{': token.LBRACE, '}': token.RBRACE,
		'[': token.LBRACK, ']': token.RBRACK,
		',': token.COMMA, ';': token.SEMI,
	}
	if t, ok := single[c]; ok {
		l.advance()
		return token.Token{Type: t, Literal: string(c), Pos: pos}
	}

	l.advance()
	l.errorf(pos, "unexpected character "+string(c))
	return token.Token{Type: token.ILLEGAL, Literal: string(c), Pos: pos}
}
