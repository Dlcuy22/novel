// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// expr.go: the Pratt (precedence-climbing) expression parser.
//
// Purpose:
//   Parses expressions by binding power: parseBinary climbs precedence over
//   parseUnary, which feeds parsePostfix (calls, selectors, index, struct
//   literals, the `!` error-propagation suffix). The noStructLit toggle and
//   its parseExpr{AllowStruct,NoStruct} wrappers resolve the
//   composite-literal-vs-block ambiguity in control-flow headers.

package parser

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

// parseExprList parses one or more comma-separated expressions.
func (p *Parser) parseExprList() []ast.Expr {
	exprs := []ast.Expr{p.parseExpr()}
	for p.accept(token.COMMA) {
		exprs = append(exprs, p.parseExpr())
	}
	return exprs
}

func (p *Parser) parseExpr() ast.Expr { return p.parseBinary(1) }

// parseExprAllowStruct parses an expression with struct literals re-enabled,
// restoring the previous setting afterwards. Used inside delimiters (parens,
// call args, index brackets, literal bodies) where the control-flow-header
// ambiguity does not apply.
func (p *Parser) parseExprAllowStruct() ast.Expr {
	saved := p.noStructLit
	p.noStructLit = false
	x := p.parseExpr()
	p.noStructLit = saved
	return x
}

// parseExprNoStruct parses a control-flow header expression (if/for condition,
// range iterable) with bare struct literals suppressed, so `for xs {` opens the
// loop body instead of being read as `xs{...}`.
func (p *Parser) parseExprNoStruct() ast.Expr {
	saved := p.noStructLit
	p.noStructLit = true
	x := p.parseExpr()
	p.noStructLit = saved
	return x
}

// precedence returns the binding power of a binary operator, or 0 if tt is not
// a binary operator. Higher binds tighter.
func precedence(tt token.Type) int {
	switch tt {
	case token.OR:
		return 1
	case token.AND:
		return 2
	case token.EQ, token.NEQ, token.LT, token.GT, token.LE, token.GE:
		return 3
	case token.PLUS, token.MINUS, token.PIPE, token.CARET:
		return 4
	case token.STAR, token.SLASH, token.PERCENT, token.AMP, token.SHL, token.SHR:
		return 5
	case token.DOTDOT: // range, used in match arms; low precedence
		return 1
	}
	return 0
}

// parseBinary is precedence-climbing over parseUnary operands.
func (p *Parser) parseBinary(minPrec int) ast.Expr {
	left := p.parseUnary()
	for {
		op := p.cur().Type
		prec := precedence(op)
		if prec < minPrec {
			return left
		}
		opTok := p.advance()
		right := p.parseBinary(prec + 1)
		left = &ast.BinaryExpr{Op: op, Left: left, Right: right, Pos: opTok.Pos}
	}
}

func (p *Parser) parseUnary() ast.Expr {
	switch p.cur().Type {
	case token.MINUS, token.NOT, token.NEQ:
		// NEQ ('!=') can't start an expression; only NOT/MINUS are real unary.
		op := p.advance()
		return &ast.UnaryExpr{Op: op.Type, Operand: p.parseUnary(), Pos: op.Pos}
	}
	return p.parsePostfix()
}

// parsePostfix handles call, selector, index, struct-literal, and the `!`
// error-propagation suffix, layered over a primary expression.
func (p *Parser) parsePostfix() ast.Expr {
	x := p.parsePrimary()
	for {
		switch p.cur().Type {
		case token.LPAREN:
			x = p.finishCall(x)
		case token.DOT:
			dot := p.advance()
			var name string
			// Allow keywords as selector names to support FFI structures using keywords
			if p.cur().Type == token.IDENT || p.cur().Type.IsKeyword() {
				name = p.advance().Literal
			} else {
				p.expect(token.IDENT, "field or method name")
			}
			x = &ast.SelectorExpr{X: x, Name: name, Pos: dot.Pos}
		case token.LBRACK:
			lb := p.advance()
			idx := p.parseExprAllowStruct()
			p.expect(token.RBRACK, "']'")
			x = &ast.IndexExpr{X: x, Index: idx, Pos: lb.Pos}
		case token.LBRACE:
			// Struct literal, unless we're in a control-flow header (where `{`
			// opens the loop/if body; see Parser.noStructLit). A bare type name
			// `Type{...}` or a module-qualified `mod.Type{...}` both qualify.
			if p.noStructLit {
				return x
			}
			if id, ok := x.(*ast.Ident); ok {
				x = p.finishStructLit("", id.Name, id.Pos)
				continue
			}
			if sel, ok := x.(*ast.SelectorExpr); ok {
				if base, ok := sel.X.(*ast.Ident); ok {
					x = p.finishStructLit(base.Name, sel.Name, base.Pos)
					continue
				}
			}
			return x
		case token.NOT:
			// `call!` error-propagation suffix.
			p.advance()
			if call, ok := x.(*ast.CallExpr); ok {
				call.Bang = true
			} else {
				p.errorf(p.cur().Pos, "'!' propagation must follow a call")
			}
		default:
			return x
		}
	}
}

func (p *Parser) finishCall(fn ast.Expr) ast.Expr {
	lp := p.advance() // LPAREN
	call := &ast.CallExpr{Fn: fn, Pos: lp.Pos}
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		call.Args = append(call.Args, p.parseExprAllowStruct())
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN, "')' to close call")
	return call
}

func (p *Parser) finishStructLit(module, typeName string, pos token.Position) ast.Expr {
	p.expect(token.LBRACE, "'{'")
	lit := &ast.StructLit{Module: module, Type: typeName, Pos: pos}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		// named `field: value` vs positional `value`
		if p.at(token.IDENT) && p.peek().Type == token.COLON {
			name := p.advance().Literal
			p.advance() // COLON
			lit.Fields = append(lit.Fields, ast.FieldInit{Name: name, Value: p.parseExprAllowStruct()})
		} else {
			lit.Positional = append(lit.Positional, p.parseExprAllowStruct())
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RBRACE, "'}' to close struct literal")
	return lit
}
