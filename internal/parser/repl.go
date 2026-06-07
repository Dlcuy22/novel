// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// repl.go: the REPL parse entrypoint.
//
// Purpose:
//   Parses a single line of interactive input (language-spec.md §12) without
//   the `pkg` header ParseFile requires. The input may be an import, a
//   declaration, a statement, or a bare expression; a bare expression is
//   surfaced separately so the front end can auto-print its value.

package parser

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

// ParseRepl parses one REPL input into a ReplUnit. `let`/`const` are parsed as
// top-level declarations (not statements) so the binding persists as a session
// global across lines rather than emitting a throwaway `local`.
func (p *Parser) ParseRepl() *ast.ReplUnit {
	u := &ast.ReplUnit{Pos: p.cur().Pos}

	switch p.cur().Type {
	case token.EOF:
		return u // empty input
	case token.IMPORT:
		imp := p.parseImport()
		u.Import = &imp
		return u
	case token.FN, token.TYPE, token.PUB, token.LET, token.CONST:
		u.Decl = p.parseDecl()
		return u
	case token.IF, token.FOR, token.SPAWN, token.RETURN, token.BREAK, token.CONTINUE, token.LBRACE:
		u.Stmt = p.parseStmt()
		return u
	}

	// Anything else is a simple statement (assignment, inc/dec, channel send) or
	// a bare expression. A bare expression is recorded in Expr for auto-print.
	s := p.parseSimpleStmt()
	if es, ok := s.(*ast.ExprStmt); ok {
		u.Expr = es.X
	} else {
		u.Stmt = s
	}
	return u
}
