// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package token defines the lexical tokens of the Novel language.
//
// Purpose:
//
//	Shared vocabulary between the lexer (produces tokens) and the parser
//	(consumes them). No behavior beyond classification and keyword lookup.
//
// Key Components:
//   - Type: enum of every token category (operators, keywords, literals)
//   - Lookup(): maps an identifier to its keyword Type, or IDENT
//   - Token, Position: a lexeme and its 1-based source location
package token

// Type identifies the category of a token.
type Type int

const (
	ILLEGAL Type = iota // an unexpected/unknown character
	EOF                 // end of input

	// Identifiers and literals.
	IDENT     // foo, bar, main
	INT       // 42, 42u, 42i64
	FLOAT     // 3.14, 3.14f32
	STRING    // "hello"
	RAWSTRING // `raw\nstring`
	INTERPSTR // $"hello {name}"

	// Operators and punctuation.
	PLUS     // +
	MINUS    // -
	STAR     // *
	SLASH    // /
	PERCENT  // %
	ASSIGN   // =
	DECLARE  // :=
	PLUSEQ   // +=
	MINUSEQ  // -=
	STAREQ   // *=
	SLASHEQ  // /=
	INC      // ++
	DEC      // --
	EQ       // ==
	NEQ      // !=
	LT       // <
	GT       // >
	LE       // <=
	GE       // >=
	AND      // &&
	OR       // ||
	NOT      // !
	AMP      // &
	PIPE     // |
	CARET    // ^
	SHL      // <<
	SHR      // >>
	ARROW    // <- (channel) ; also used for -> return arrow (see token.RARROW)
	RARROW   // ->
	FATARROW // => (match arms)
	BANG     // ! suffix for error propagation (lexed as NOT, disambiguated by parser)
	DOTDOT   // .. (match ranges)
	ELLIPSIS // ... (variadic)

	LPAREN // (
	RPAREN // )
	LBRACE // {
	RBRACE // }
	LBRACK // [
	RBRACK // ]
	COMMA  // ,
	DOT    // .
	COLON  // :
	SEMI   // ;

	// Keywords.
	keywordsStart
	FN
	LET
	CONST
	TYPE
	STRUCT
	IF
	ELIF
	ELSE
	FOR
	BREAK
	CONTINUE
	RETURN // the `ret` keyword
	SPAWN
	CHAN
	SEND
	RECV
	IMPORT
	PKG // removed keyword; still lexed so the parser can emit a migration error
	NIL
	TRUE
	FALSE
	IN
	AS
	PUB
	keywordsEnd
)

var keywords = map[string]Type{
	"fn":       FN,
	"let":      LET,
	"const":    CONST,
	"type":     TYPE,
	"struct":   STRUCT,
	"if":       IF,
	"elif":     ELIF,
	"else":     ELSE,
	"for":      FOR,
	"break":    BREAK,
	"continue": CONTINUE,
	"ret":      RETURN,
	"spawn":    SPAWN,
	"chan":     CHAN,
	"send":     SEND,
	"recv":     RECV,
	"import":   IMPORT,
	"pkg":      PKG,
	"nil":      NIL,
	"true":     TRUE,
	"false":    FALSE,
	"in":       IN,
	"as":       AS,
	"pub":      PUB,
}

// Lookup maps an identifier to its keyword token type, or IDENT if it is not a
// keyword.
func Lookup(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return IDENT
}

/*
IsKeyword checks if the token type represents a keyword.

    returns:
          bool: true if the token falls within the keyword range
*/
func (t Type) IsKeyword() bool {
	return t > keywordsStart && t < keywordsEnd
}


// Position is a 1-based source location.
type Position struct {
	Line   int
	Column int
}

// Token is a single lexical unit with its literal text and source position.
type Token struct {
	Type    Type
	Literal string
	Pos     Position
}
