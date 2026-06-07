// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// stmt.go: statement parsing for the Novel parser.
//
// Purpose:
//   Parses block bodies and every statement form (let/const, assignment,
//   inc/dec, return, if/elif/else, the four for shapes, spawn, channel-send
//   sugar, break/continue). Control-flow header expressions are parsed with
//   struct literals suppressed (see parseExprNoStruct in expr.go).

package parser

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

// parseBlock parses `{ stmt* }`.
func (p *Parser) parseBlock() *ast.Block {
	lb := p.expect(token.LBRACE, "'{'")
	blk := &ast.Block{Pos: lb.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		before := p.pos
		if s := p.parseStmt(); s != nil {
			blk.Stmts = append(blk.Stmts, s)
		}
		p.accept(token.SEMI) // optional separator
		if p.pos == before {
			p.advance()
		}
	}
	p.expect(token.RBRACE, "'}' to close block")
	return blk
}

func (p *Parser) parseStmt() ast.Stmt {
	switch p.cur().Type {
	case token.LET, token.CONST:
		return p.parseLetStmt()
	case token.RETURN:
		return p.parseReturnStmt()
	case token.IF:
		return p.parseIfStmt()
	case token.FOR:
		return p.parseForStmt()
	case token.SPAWN:
		return p.parseSpawnStmt()
	case token.BREAK:
		t := p.advance()
		return &ast.BreakStmt{Pos: t.Pos}
	case token.CONTINUE:
		t := p.advance()
		return &ast.ContinueStmt{Pos: t.Pos}
	case token.LBRACE:
		return p.parseBlock()
	case token.IDENT:
		// `return` was renamed to `ret`. Since `return` is no longer in the
		// keyword map it now lexes as a plain IDENT, so we match on the literal
		// (not a token type) to catch the old keyword at statement position and
		// report it, recovering by parsing the rest as a return statement so one
		// clear error surfaces instead of a cascade. If `return` is ever
		// re-added to the keyword map this branch stops firing; keep them in sync.
		if p.cur().Literal == "return" {
			p.errorf(p.cur().Pos, "`return` was renamed to `ret`")
			return p.parseReturnStmt()
		}
		return p.parseSimpleStmt()
	default:
		return p.parseSimpleStmt()
	}
}

func (p *Parser) parseLetStmt() ast.Stmt {
	isConst := p.cur().Type == token.CONST
	tok := p.advance()
	ls := &ast.LetStmt{Const: isConst, Pos: tok.Pos}
	ls.Names = p.parseNameList()
	if !p.at(token.ASSIGN) {
		ls.Type = p.parseType()
	}
	if p.accept(token.ASSIGN) {
		ls.Values = p.parseExprList()
	}
	return ls
}

func (p *Parser) parseReturnStmt() ast.Stmt {
	tok := p.advance() // RETURN
	rs := &ast.ReturnStmt{Pos: tok.Pos}
	// A return with no values is followed by '}' or a separator.
	if !p.at(token.RBRACE) && !p.at(token.SEMI) && !p.at(token.EOF) {
		rs.Values = p.parseExprList()
	}
	return rs
}

func (p *Parser) parseIfStmt() ast.Stmt {
	tok := p.advance() // IF or ELIF
	is := &ast.IfStmt{Pos: tok.Pos}
	is.Cond = p.parseExprNoStruct()
	is.Then = p.parseBlock()
	switch p.cur().Type {
	case token.ELIF:
		is.Else = p.parseIfStmt() // recurse: elif chains nest in Else
	case token.ELSE:
		p.advance()
		is.Else = p.parseBlock()
	}
	return is
}

func (p *Parser) parseForStmt() ast.Stmt {
	tok := p.advance() // FOR
	fs := &ast.ForStmt{Pos: tok.Pos}

	// for { }  -> infinite
	if p.at(token.LBRACE) {
		fs.Kind = ast.ForInfinite
		fs.Body = p.parseBlock()
		return fs
	}

	// for k, v in iter { }  /  for v in iter { } -> range
	if p.at(token.IDENT) && (p.peek().Type == token.IN || p.peek().Type == token.COMMA) {
		// Distinguish range form by scanning for IN before a LBRACE/SEMI.
		if p.looksLikeRange() {
			fs.Kind = ast.ForRange
			fs.Key = p.advance().Literal
			if p.accept(token.COMMA) {
				fs.Val = p.expect(token.IDENT, "range value name").Literal
			}
			p.expect(token.IN, "'in' in range loop")
			fs.Iter = p.parseExprNoStruct()
			fs.Body = p.parseBlock()
			return fs
		}
	}

	// Classic vs while: a classic loop has a ';' before the body.
	if p.hasSemiBeforeBrace() {
		fs.Kind = ast.ForClassic
		if !p.at(token.SEMI) {
			fs.Init = p.parseSimpleStmt()
		}
		p.expect(token.SEMI, "';' after for-init")
		if !p.at(token.SEMI) {
			fs.Cond = p.parseExpr()
		}
		p.expect(token.SEMI, "';' after for-condition")
		if !p.at(token.LBRACE) {
			fs.Post = p.parseSimpleStmt()
		}
		fs.Body = p.parseBlock()
		return fs
	}

	// for cond { } -> while
	fs.Kind = ast.ForWhile
	fs.Cond = p.parseExprNoStruct()
	fs.Body = p.parseBlock()
	return fs
}

// looksLikeRange scans ahead for an IN token before a block opens.
func (p *Parser) looksLikeRange() bool {
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].Type {
		case token.IN:
			return true
		case token.LBRACE, token.SEMI, token.EOF:
			return false
		}
	}
	return false
}

// hasSemiBeforeBrace scans ahead for a ';' before the loop body opens, marking
// a C-style for loop.
func (p *Parser) hasSemiBeforeBrace() bool {
	depth := 0
	for i := p.pos; i < len(p.toks); i++ {
		switch p.toks[i].Type {
		case token.LPAREN, token.LBRACK:
			depth++
		case token.RPAREN, token.RBRACK:
			depth--
		case token.SEMI:
			if depth == 0 {
				return true
			}
		case token.LBRACE, token.EOF:
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

func (p *Parser) parseSpawnStmt() ast.Stmt {
	tok := p.advance() // SPAWN
	return &ast.SpawnStmt{Call: p.parseExpr(), Pos: tok.Pos}
}

// parseSimpleStmt handles assignments, inc/dec, channel-send sugar, and bare
// expression statements (the init/post clauses of a classic for use this too).
func (p *Parser) parseSimpleStmt() ast.Stmt {
	pos := p.cur().Pos
	first := p.parseExpr()

	switch p.cur().Type {
	case token.INC, token.DEC:
		op := p.advance().Type
		return &ast.IncDecStmt{Target: first, Op: op, Pos: pos}
	case token.ARROW: // ch <- v
		p.advance()
		return &ast.SendStmt{Chan: first, Val: p.parseExpr(), Pos: pos}
	case token.ASSIGN, token.PLUSEQ, token.MINUSEQ, token.STAREQ, token.SLASHEQ:
		op := p.advance().Type
		targets := []ast.Expr{first}
		values := p.parseExprList()
		return &ast.AssignStmt{Targets: targets, Op: op, Values: values, Pos: pos}
	case token.COMMA:
		// multi-target assignment: a, b = f()
		targets := []ast.Expr{first}
		for p.accept(token.COMMA) {
			targets = append(targets, p.parseExpr())
		}
		op := p.expect(token.ASSIGN, "'=' in multiple assignment").Type
		values := p.parseExprList()
		return &ast.AssignStmt{Targets: targets, Op: op, Values: values, Pos: pos}
	}
	return &ast.ExprStmt{X: first, Pos: pos}
}
