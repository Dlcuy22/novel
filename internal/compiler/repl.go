// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// repl.go: the persistent REPL compile session.
//
// Purpose:
//   Backs `novel repl`. Each input line is lexed and parsed as a ReplUnit and
//   lowered to a Lua chunk by a persistent emitter, so session state (imports,
//   struct fields) accumulates across lines. Unlike CompileFile, no
//   whole-program type checking runs: the checker's rules (unused variables,
//   arity over a closed program) don't fit line-at-a-time input, so the REPL
//   relies on parse errors plus runtime errors from the driver, matching the
//   interactive model in language-spec.md §12.

package compiler

import (
	"github.com/dlcuy22/novel/internal/emit"
	"github.com/dlcuy22/novel/internal/lexer"
	"github.com/dlcuy22/novel/internal/parser"
	"github.com/dlcuy22/novel/internal/token"
)

// REPL framing markers, shared with the Lua driver (runtime/repl.lua). The Go
// front end writes a chunk then ReplEOFMark; the driver runs it and replies
// with ReplDoneMark so the front end knows the line is done. Keep these byte
// for byte in sync with the constants in runtime/repl.lua.
const (
	ReplEOFMark  = "__NOVEL_REPL_EOF_8f3a__"
	ReplDoneMark = "__NOVEL_REPL_DONE_8f3a__"
)

// NeedsMoreInput reports whether src has unclosed brackets and the REPL should
// keep reading continuation lines. It lexes src (so braces inside strings and
// comments are ignored) and checks the net nesting depth of (), [], and {}.
func NeedsMoreInput(src string) bool {
	depth := 0
	for _, t := range lexer.New(src).Tokenize() {
		switch t.Type {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		}
	}
	return depth > 0
}

// ReplSession holds the state that persists between REPL inputs: a single
// emitter whose accumulated knowledge (imports, struct fields) carries forward.
type ReplSession struct {
	emitter *emit.Emitter
}

// NewReplSession returns a fresh interactive session.
func NewReplSession() *ReplSession {
	return &ReplSession{emitter: emit.NewRepl()}
}

// ReplResult is the outcome of compiling one REPL line.
type ReplResult struct {
	Lua         string       // the Lua chunk to execute (empty on error/blank)
	Diagnostics []Diagnostic // lex/parse diagnostics, if any
}

// HasErrors reports whether any diagnostics were produced.
func (r ReplResult) HasErrors() bool { return len(r.Diagnostics) > 0 }

// Compile lexes and parses one line of REPL input and lowers it to a Lua chunk.
// Lexer and parser errors are returned as diagnostics; the chunk is empty when
// the line is blank or failed to parse.
func (s *ReplSession) Compile(line string) ReplResult {
	var diags []Diagnostic

	lx := lexer.New(line)
	_ = lx.Tokenize()
	for _, e := range lx.Errors() {
		diags = append(diags, Diagnostic{"", e.Pos.Line, e.Pos.Column, e.Msg, "lex"})
	}

	p := parser.New(line)
	unit := p.ParseRepl()
	for _, e := range p.Errors() {
		diags = append(diags, Diagnostic{"", e.Pos.Line, e.Pos.Column, e.Msg, "parse"})
	}

	if len(diags) > 0 {
		return ReplResult{Diagnostics: diags}
	}

	return ReplResult{Lua: s.emitter.Repl(unit)}
}

// Reset clears all session state, as if the REPL had just started.
func (s *ReplSession) Reset() { s.emitter = emit.NewRepl() }
