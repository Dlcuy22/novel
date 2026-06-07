// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package parser builds an AST from a token stream via recursive descent.
//
// Purpose:
//
//	Turns Novel source into an *ast.File: imports, then declarations and their
//	bodies. There is no package header; a file is runnable when it declares a
//	`main` function. Statements live in stmt.go; the Pratt (precedence-climbing)
//	expression grammar lives in expr.go and expr_primary.go; small shared
//	utilities in helpers.go.
//
// Key Components:
//   - Parser, New(): parser state over a lexed token slice
//   - ParseFile(): entrypoint, returns the root *ast.File
//   - Errors(): accumulated parse errors with positions
//
// Dependencies:
//   - internal/lexer: produces the token stream consumed here
//   - internal/token, internal/ast: input vocabulary and output nodes
//
// Spec note:
//
//	§2.3 lists `->` as a "return type arrow", but every function example
//	writes the return type with no arrow (e.g. `fn add(a int, b int) int`).
//	We accept an optional `->` before results so both forms parse; reconcile
//	the spec before committing to one.
package parser

import (
	"fmt"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/lexer"
	"github.com/dlcuy22/novel/internal/token"
)

// Error is a parse error with a source position.
type Error struct {
	Pos token.Position
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

// Parser holds parsing state over a token stream.
type Parser struct {
	toks []token.Token
	pos  int
	errs []Error
	// noStructLit suppresses treating `Ident{...}` as a struct literal. It is
	// set while parsing control-flow header expressions (if/for conditions and
	// range iterables), where `for xs {` must mean "loop over the block", not
	// "struct literal xs{...}". Same rule as Go. Parens/calls/index re-enable
	// it for their inner expressions.
	noStructLit bool
}

// New creates a Parser for the given source.
func New(src string) *Parser {
	return &Parser{toks: lexer.New(src).Tokenize()}
}

// Errors returns parse errors collected so far.
func (p *Parser) Errors() []Error { return p.errs }

func (p *Parser) cur() token.Token { return p.toks[p.pos] }

func (p *Parser) peek() token.Token {
	if p.pos+1 < len(p.toks) {
		return p.toks[p.pos+1]
	}
	return p.toks[len(p.toks)-1]
}

func (p *Parser) at(tt token.Type) bool { return p.cur().Type == tt }

func (p *Parser) advance() token.Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *Parser) accept(tt token.Type) bool {
	if p.at(tt) {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) errorf(pos token.Position, format string, args ...any) {
	p.errs = append(p.errs, Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

func (p *Parser) expect(tt token.Type, what string) token.Token {
	if !p.at(tt) {
		p.errorf(p.cur().Pos, "expected %s", what)
		return p.cur()
	}
	return p.advance()
}

// ParseFile parses a full source file: imports, then declarations. Novel has
// no package header; a leading `pkg <name>` line (from older code) is consumed
// and reported as a removed feature so migration errors are clear.
func (p *Parser) ParseFile() *ast.File {
	file := &ast.File{Pos: p.cur().Pos}

	if p.at(token.PKG) {
		pkgTok := p.advance() // PKG
		p.accept(token.IDENT) // optional package name
		p.errorf(pkgTok.Pos, "`pkg` declarations are no longer used; remove this line")
	}

	for p.at(token.IMPORT) {
		file.Imports = append(file.Imports, p.parseImport())
	}

	for !p.at(token.EOF) {
		before := p.pos
		if d := p.parseDecl(); d != nil {
			file.Decls = append(file.Decls, d)
		}
		if p.pos == before { // no progress: avoid an infinite loop on bad input
			p.advance()
		}
	}
	return file
}

// parseImport parses a dotted import path with an optional alias:
//
//	import std.io
//	import std.math as m
//	import geometry
//
// The dotted segments are joined with "/" so the resolver and emitter handle
// the path the same way they handle on-disk module paths ("std/io").
func (p *Parser) parseImport() ast.Import {
	impTok := p.advance() // IMPORT
	segs := []string{p.expect(token.IDENT, "import path").Literal}
	for p.accept(token.DOT) {
		segs = append(segs, p.expect(token.IDENT, "import path segment after '.'").Literal)
	}
	alias := ""
	if p.accept(token.AS) {
		alias = p.expect(token.IDENT, "import alias after 'as'").Literal
	}
	return ast.Import{Alias: alias, Path: strings.Join(segs, "/"), Pos: impTok.Pos}
}

// Declarations.

func (p *Parser) parseDecl() ast.Decl {
	pub := p.accept(token.PUB)
	switch p.cur().Type {
	case token.FN:
		return p.parseFuncDecl(pub)
	case token.TYPE:
		return p.parseStructDecl(pub)
	case token.LET, token.CONST:
		return p.parseVarDecl(pub)
	default:
		p.errorf(p.cur().Pos, "expected declaration (fn, type, let, const), got %q", p.cur().Literal)
		return nil
	}
}

func (p *Parser) parseFuncDecl(pub bool) ast.Decl {
	fnTok := p.advance() // FN
	fd := &ast.FuncDecl{Public: pub, Pos: fnTok.Pos}

	// Optional receiver: fn (c Circle) area() ...
	if p.at(token.LPAREN) {
		p.advance()
		recv := ast.Param{Name: p.expect(token.IDENT, "receiver name").Literal}
		recv.Type = p.parseType()
		p.expect(token.RPAREN, "')' after receiver")
		fd.Receiver = &recv
	}

	fd.Name = p.expect(token.IDENT, "function name").Literal
	fd.Params = p.parseParams()

	// Optional `->`, then zero or more result types.
	p.accept(token.RARROW)
	fd.Results = p.parseResults()

	fd.Body = p.parseBlock()
	return fd
}

func (p *Parser) parseParams() []ast.Param {
	p.expect(token.LPAREN, "'(' to start parameters")
	var params []ast.Param
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		var pm ast.Param
		pm.Name = p.expect(token.IDENT, "parameter name").Literal
		if p.accept(token.ELLIPSIS) {
			pm.Variadic = true
		}
		pm.Type = p.parseType()
		params = append(params, pm)
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN, "')' to close parameters")
	return params
}

// parseResults reads the result type list. A single type needs no parens; a
// tuple is `(T1, T2)`.
func (p *Parser) parseResults() []ast.Type {
	if p.at(token.LBRACE) { // no results
		return nil
	}
	if p.accept(token.LPAREN) {
		var res []ast.Type
		for !p.at(token.RPAREN) && !p.at(token.EOF) {
			res = append(res, p.parseType())
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RPAREN, "')' to close result list")
		return res
	}
	return []ast.Type{p.parseType()}
}

func (p *Parser) parseStructDecl(pub bool) ast.Decl {
	typeTok := p.advance() // TYPE
	name := p.expect(token.IDENT, "type name").Literal
	p.expect(token.STRUCT, "'struct'")
	p.expect(token.LBRACE, "'{' to start struct body")

	sd := &ast.StructDecl{Public: pub, Name: name, Pos: typeTok.Pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		fpub := p.accept(token.PUB)
		fname := p.expect(token.IDENT, "field name").Literal
		// Embedded struct: just a type name with no following field type.
		if p.at(token.RBRACE) || p.peekFieldEnd() {
			sd.Fields = append(sd.Fields, ast.StructField{
				Public: fpub, Name: fname, Embedded: true,
				Type: ast.Type{Name: fname},
			})
			continue
		}
		ftype := p.parseType()
		sd.Fields = append(sd.Fields, ast.StructField{Public: fpub, Name: fname, Type: ftype})
	}
	p.expect(token.RBRACE, "'}' to close struct")
	return sd
}

// peekFieldEnd reports whether the current token ends an embedded-field line
// (the next field starts with an identifier on a new line, or the struct ends).
// Heuristic: embedded fields are a lone identifier followed by another field
// identifier or '}'. Since we have no newline tokens, treat a following IDENT
// or PUB or RBRACE as the end of an embedded field.
func (p *Parser) peekFieldEnd() bool {
	switch p.cur().Type {
	case token.IDENT:
		// `name int` -> not embedded; `Named\n score` -> embedded. We can't see
		// newlines, so disambiguate: if the current IDENT is a known primitive
		// it's a type; otherwise assume it begins the next field.
		return !isPrimitiveType(p.cur().Literal)
	case token.PUB, token.RBRACE:
		return true
	}
	return false
}

func (p *Parser) parseVarDecl(pub bool) ast.Decl {
	isConst := p.cur().Type == token.CONST
	tok := p.advance() // LET or CONST
	vd := &ast.VarDecl{Const: isConst, Public: pub, Pos: tok.Pos}
	vd.Names = p.parseNameList()
	if !p.at(token.ASSIGN) && !p.at(token.EOF) {
		vd.Type = p.parseType()
	}
	if p.accept(token.ASSIGN) {
		vd.Values = p.parseExprList()
	}
	return vd
}

func (p *Parser) parseNameList() []string {
	names := []string{p.expect(token.IDENT, "name").Literal}
	for p.accept(token.COMMA) {
		names = append(names, p.expect(token.IDENT, "name").Literal)
	}
	return names
}
