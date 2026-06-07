// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// walk.go: the declaration, statement, and expression walk.
//
// Purpose:
//   Visits every node, applying the semantic rules: undefined identifiers,
//   unused locals, assignment to const, call arity for user-defined functions,
//   unknown type names, and `!` error-propagation context. Scope bookkeeping
//   lives in scope.go; this file decides when to declare, resolve, and report.

package types

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

// checkDecl dispatches on a top-level declaration.
func (c *Checker) checkDecl(d ast.Decl) {
	switch n := d.(type) {
	case *ast.FuncDecl:
		c.checkFuncDecl(n)
	case *ast.StructDecl:
		c.checkStructDecl(n)
	case *ast.VarDecl:
		c.checkVarDecl(n)
	}
}

// checkFuncDecl walks a function body with a fresh scope holding the receiver
// and parameters. Receiver and parameters are exempt from the unused-variable
// rule (matching Go), so they are declared pre-used.
func (c *Checker) checkFuncDecl(fn *ast.FuncDecl) {
	// Validate the signature's type names before walking the body.
	if fn.Receiver != nil {
		c.checkType(fn.Receiver.Type, fn.Pos)
	}
	for _, p := range fn.Params {
		c.checkType(p.Type, fn.Pos)
	}
	for _, r := range fn.Results {
		c.checkType(r, fn.Pos)
	}

	prevResults := c.curResults
	c.curResults = fn.Results
	defer func() { c.curResults = prevResults }()

	c.push()
	if fn.Receiver != nil {
		c.declareUsed(fn.Receiver.Name)
	}
	for _, p := range fn.Params {
		c.declareUsed(p.Name)
	}
	c.checkStmts(fn.Body)
	c.pop()
}

func (c *Checker) checkStructDecl(s *ast.StructDecl) {
	for _, f := range s.Fields {
		if f.Embedded {
			continue // embedded field's type is its own name; promotion unhandled
		}
		c.checkType(f.Type, s.Pos)
	}
}

// checkVarDecl walks a top-level let/const. Its value expressions are checked
// for undefined references, but globals are exempt from the unused rule (they
// model package-level state, not locals).
func (c *Checker) checkVarDecl(v *ast.VarDecl) {
	c.checkType(v.Type, v.Pos)
	for _, val := range v.Values {
		c.checkExpr(val)
	}
}

// declareUsed binds a name that is exempt from the unused-variable rule
// (parameters, receivers, loop variables).
func (c *Checker) declareUsed(name string) {
	if name == "" || name == "_" {
		return
	}
	c.declare(name, token.Position{}, false)
	if b, ok := c.scopes[len(c.scopes)-1].vars[name]; ok {
		b.used = true
	}
}

// checkStmts walks the statements of a block without opening a new scope; the
// caller owns scope lifetime (function bodies share the parameter scope).
func (c *Checker) checkStmts(b *ast.Block) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		c.checkStmt(s)
	}
}

// checkBlock walks a nested block in its own scope.
func (c *Checker) checkBlock(b *ast.Block) {
	if b == nil {
		return
	}
	c.push()
	c.checkStmts(b)
	c.pop()
}

func (c *Checker) checkStmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.Block:
		c.checkBlock(n)
	case *ast.LetStmt:
		c.checkLetStmt(n)
	case *ast.AssignStmt:
		c.checkAssign(n, false)
	case *ast.IncDecStmt:
		c.checkIncDec(n)
	case *ast.ExprStmt:
		c.checkExpr(n.X)
	case *ast.ReturnStmt:
		for _, v := range n.Values {
			c.checkExpr(v)
		}
	case *ast.IfStmt:
		c.checkIf(n)
	case *ast.ForStmt:
		c.checkFor(n)
	case *ast.SpawnStmt:
		c.checkExpr(n.Call)
	case *ast.SendStmt:
		c.checkExpr(n.Chan)
		c.checkExpr(n.Val)
	case *ast.BreakStmt, *ast.ContinueStmt:
		// nothing to resolve
	}
}

// checkLetStmt declares each name after checking the value expressions, so a
// `let x = x` can't resolve to itself. The blank name is skipped.
func (c *Checker) checkLetStmt(n *ast.LetStmt) {
	c.checkType(n.Type, n.Pos)
	for _, v := range n.Values {
		c.checkExpr(v)
	}
	for _, name := range n.Names {
		if name == "_" {
			continue
		}
		c.declare(name, n.Pos, n.Const)
	}
}

// checkAssign handles `targets op= values`. In a classic-for init clause an
// unresolved identifier target is a declaration (mirroring the emitter, which
// lowers `for i = 0; ...` to `local i`); elsewhere it must already be in scope.
// Assigning to a constant is an error.
func (c *Checker) checkAssign(n *ast.AssignStmt, forInit bool) {
	for _, v := range n.Values {
		c.checkExpr(v)
	}
	for _, t := range n.Targets {
		id, isIdent := t.(*ast.Ident)
		if !isIdent {
			c.checkExpr(t) // index/selector target: verify its base is in scope
			continue
		}
		if b, ok := c.resolve(id.Name); ok {
			if b.isConst {
				c.errorf(id.Pos, "cannot assign to constant: %s", id.Name)
			}
			continue
		}
		if forInit {
			c.declareUsed(id.Name)
			continue
		}
		// Reassigning a global is fine; assigning to a never-declared name is a
		// likely typo (it would silently create a Lua global).
		if !c.globals[id.Name] {
			c.errorf(id.Pos, "undefined: %s", id.Name)
		}
	}
}

func (c *Checker) checkIncDec(n *ast.IncDecStmt) {
	if id, ok := n.Target.(*ast.Ident); ok {
		if b, found := c.resolve(id.Name); found {
			if b.isConst {
				c.errorf(id.Pos, "cannot assign to constant: %s", id.Name)
			}
			return
		}
		if !c.globals[id.Name] {
			c.errorf(id.Pos, "undefined: %s", id.Name)
		}
		return
	}
	c.checkExpr(n.Target)
}

func (c *Checker) checkIf(n *ast.IfStmt) {
	c.checkExpr(n.Cond)
	c.checkBlock(n.Then)
	switch els := n.Else.(type) {
	case *ast.IfStmt:
		c.checkIf(els)
	case *ast.Block:
		c.checkBlock(els)
	}
}

// checkFor handles all four loop shapes. The classic form wraps init/cond/post
// and body in one scope so the induction variable is visible throughout, then
// the body gets its own inner scope. Loop variables are exempt from the unused
// rule.
func (c *Checker) checkFor(n *ast.ForStmt) {
	switch n.Kind {
	case ast.ForClassic:
		c.push()
		if a, ok := n.Init.(*ast.AssignStmt); ok {
			c.checkAssign(a, true)
		} else if n.Init != nil {
			c.checkStmt(n.Init)
		}
		if n.Cond != nil {
			c.checkExpr(n.Cond)
		}
		if n.Post != nil {
			c.checkStmt(n.Post)
		}
		c.checkBlock(n.Body)
		c.pop()
	case ast.ForRange:
		c.checkExpr(n.Iter)
		c.push()
		c.declareUsed(n.Key)
		c.declareUsed(n.Val)
		c.checkStmts(n.Body)
		c.pop()
	case ast.ForWhile:
		c.checkExpr(n.Cond)
		c.checkBlock(n.Body)
	case ast.ForInfinite:
		c.checkBlock(n.Body)
	}
}
