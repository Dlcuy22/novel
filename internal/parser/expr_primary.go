// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// expr_primary.go: primary expressions and type references.
//
// Purpose:
//   Parses the leaves of the expression grammar (identifiers, literals,
//   parenthesized exprs, slice/chan literals, recv/send builtins) plus the
//   type-reference grammar used in declarations and casts. Also splits a
//   $"..." interpolation literal into alternating literal/expr parts,
//   re-parsing embedded expressions with a sub-parser.

package parser

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/lexer"
	"github.com/dlcuy22/novel/internal/token"
)

func (p *Parser) parsePrimary() ast.Expr {
	tok := p.cur()
	switch tok.Type {
	case token.IDENT:
		// `map[K]V{...}` is a map literal, not an index into a `map` variable.
		if tok.Literal == "map" && p.peek().Type == token.LBRACK {
			return p.parseMapLit()
		}
		p.advance()
		return &ast.Ident{Name: tok.Literal, Pos: tok.Pos}
	case token.INT, token.FLOAT, token.STRING, token.RAWSTRING, token.TRUE, token.FALSE, token.NIL:
		p.advance()
		return &ast.BasicLit{Kind: tok.Type, Value: tok.Literal, Pos: tok.Pos}
	case token.INTERPSTR:
		p.advance()
		return p.parseInterp(tok.Literal, tok.Pos)
	case token.LPAREN:
		p.advance()
		e := p.parseExprAllowStruct()
		p.expect(token.RPAREN, "')'")
		return e
	case token.LBRACK:
		return p.parseSliceLit()
	case token.CHAN:
		return p.parseChanExpr()
	case token.RECV:
		// recv(ch) parses as a normal call; fall through to ident handling.
		p.advance()
		return &ast.Ident{Name: "recv", Pos: tok.Pos}
	case token.SEND:
		p.advance()
		return &ast.Ident{Name: "send", Pos: tok.Pos}
	}
	p.errorf(tok.Pos, "unexpected token %q in expression", tok.Literal)
	p.advance()
	return &ast.Ident{Name: "_", Pos: tok.Pos}
}

// parseSliceLit handles `[e1, e2]`, `[]T{...}`, and `[]T` (empty when followed
// by `=`/nothing). Fixed arrays `[N]T` are parsed as slices for now.
func (p *Parser) parseSliceLit() ast.Expr {
	lb := p.advance() // LBRACK
	// Bare list literal: [1, 2, 3]
	if !p.at(token.RBRACK) {
		lit := &ast.SliceLit{Pos: lb.Pos}
		for !p.at(token.RBRACK) && !p.at(token.EOF) {
			lit.Elems = append(lit.Elems, p.parseExprAllowStruct())
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RBRACK, "']'")
		return lit
	}
	// `[]T{...}` typed form
	p.expect(token.RBRACK, "']'")
	elem := p.parseType()
	lit := &ast.SliceLit{ElemType: &elem, Pos: lb.Pos}
	if p.accept(token.LBRACE) {
		for !p.at(token.RBRACE) && !p.at(token.EOF) {
			lit.Elems = append(lit.Elems, p.parseExprAllowStruct())
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RBRACE, "'}'")
	}
	return lit
}

// parseChanExpr handles `chan<T>()` and `chan<T>(cap)`.
func (p *Parser) parseChanExpr() ast.Expr {
	tok := p.advance() // CHAN
	p.expect(token.LT, "'<' in channel type")
	elem := p.parseType()
	p.expect(token.GT, "'>' in channel type")
	ce := &ast.ChanExpr{Elem: elem, Pos: tok.Pos}
	p.expect(token.LPAREN, "'(' in channel constructor")
	if !p.at(token.RPAREN) {
		ce.Cap = p.parseExpr()
	}
	p.expect(token.RPAREN, "')' in channel constructor")
	return ce
}

// parseMapLit handles `map[K]V{}` and `map[K]V{ k: v, ... }`.
func (p *Parser) parseMapLit() ast.Expr {
	tok := p.advance() // IDENT "map"
	p.expect(token.LBRACK, "'[' in map type")
	key := p.parseType()
	p.expect(token.RBRACK, "']' in map type")
	val := p.parseType()
	lit := &ast.MapLit{Key: key, Val: val, Pos: tok.Pos}
	p.expect(token.LBRACE, "'{' in map literal")
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		k := p.parseExprAllowStruct()
		p.expect(token.COLON, "':' between map key and value")
		v := p.parseExprAllowStruct()
		lit.Entries = append(lit.Entries, ast.MapEntry{Key: k, Value: v})
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RBRACE, "'}' to close map literal")
	return lit
}

// parseInterp splits a $"..." literal body into alternating literal/expr parts.
// Embedded expressions are re-lexed and parsed with a fresh sub-parser.
func (p *Parser) parseInterp(body string, pos token.Position) ast.Expr {
	lit := &ast.InterpLit{Pos: pos}
	var cur []byte
	i := 0
	for i < len(body) {
		c := body[i]
		if c == '{' {
			if len(cur) > 0 {
				lit.Parts = append(lit.Parts, ast.InterpPart{Lit: lexer.Unescape(string(cur))})
				cur = nil
			}
			// find matching '}'
			depth := 1
			j := i + 1
			for j < len(body) && depth > 0 {
				if body[j] == '{' {
					depth++
				} else if body[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			exprSrc := body[i+1 : j]
			lit.Parts = append(lit.Parts, ast.InterpPart{Expr: parseExprString(exprSrc)})
			i = j + 1
			continue
		}
		cur = append(cur, c)
		i++
	}
	if len(cur) > 0 {
		lit.Parts = append(lit.Parts, ast.InterpPart{Lit: lexer.Unescape(string(cur))})
	}
	return lit
}

// parseExprString parses a standalone expression (used for interpolation).
func parseExprString(src string) ast.Expr {
	sp := New("pkg _")
	sp.toks = tokensOf(src)
	sp.pos = 0
	return sp.parseExpr()
}

// Types.

var primitiveTypes = map[string]bool{
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "bool": true, "string": true,
	"byte": true, "rune": true, "error": true, "any": true,
}

func isPrimitiveType(name string) bool { return primitiveTypes[name] }

// parseType parses a type reference: T, []T, [N]T, map[K]V, chan<T>.
func (p *Parser) parseType() ast.Type {
	switch p.cur().Type {
	case token.LBRACK:
		p.advance()
		var arrLen int
		if p.at(token.INT) {
			arrLen = atoiSafe(p.advance().Literal)
		}
		p.expect(token.RBRACK, "']' in type")
		elem := p.parseType()
		return ast.Type{Slice: arrLen == 0, ArrLen: arrLen, Elem: &elem}
	case token.CHAN:
		p.advance()
		p.expect(token.LT, "'<' in channel type")
		elem := p.parseType()
		p.expect(token.GT, "'>' in channel type")
		return ast.Type{Chan: true, Elem: &elem}
	case token.IDENT:
		name := p.advance().Literal
		if name == "map" {
			p.expect(token.LBRACK, "'[' in map type")
			key := p.parseType()
			p.expect(token.RBRACK, "']' in map type")
			val := p.parseType()
			return ast.Type{Name: "map", MapKey: &key, Elem: &val}
		}
		return ast.Type{Name: name}
	}
	p.errorf(p.cur().Pos, "expected a type, got %q", p.cur().Literal)
	return ast.Type{Name: "_"}
}
