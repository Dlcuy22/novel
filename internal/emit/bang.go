// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// bang.go: lowering the `!` error-propagation suffix (language-spec.md §8.1).
//
// Purpose:
//   A call written `f(x)!` returns early with the error when f's second return
//   value is non-nil, otherwise yields f's first value. Lua has no such
//   construct, so before emitting a statement we hoist each `!` call into a
//   prelude:
//
//     local __bang_1, __bangerr_1 = f(x)
//     if __bangerr_1 ~= nil then
//       return nil, __bangerr_1      -- zeros for non-error results, then err
//     end
//
//   and remember that the original call now renders as the temp __bang_1. The
//   type checker has already verified the enclosing function returns
//   (..., error), so the early return is well-formed.

package emit

import (
	"fmt"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
)

// hoistStmt emits the `!` prelude for every error-propagating call evaluated by
// statement s, before s itself is rendered. Only positions evaluated exactly
// once at this point are hoisted (let/assign/return/expr/send/spawn values, an
// if condition, a range iterable); a `!` elsewhere is left to render inline
// (unchanged from prior behavior).
func (e *Emitter) hoistStmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.LetStmt:
		e.hoistExprs(n.Values)
	case *ast.AssignStmt:
		e.hoistExprs(n.Values)
	case *ast.ReturnStmt:
		e.hoistExprs(n.Values)
	case *ast.ExprStmt:
		e.hoistExpr(n.X)
	case *ast.SendStmt:
		e.hoistExpr(n.Chan)
		e.hoistExpr(n.Val)
	case *ast.SpawnStmt:
		e.hoistExpr(n.Call)
	case *ast.IfStmt:
		e.hoistExpr(n.Cond)
	case *ast.ForStmt:
		if n.Kind == ast.ForRange {
			e.hoistExpr(n.Iter)
		}
	}
}

func (e *Emitter) hoistExprs(xs []ast.Expr) {
	for _, x := range xs {
		e.hoistExpr(x)
	}
}

// hoistExpr walks an expression post-order, emitting a prelude for each `!`
// call so inner propagations resolve before the outer call that consumes them.
func (e *Emitter) hoistExpr(x ast.Expr) {
	switch n := x.(type) {
	case nil:
		return
	case *ast.BinaryExpr:
		e.hoistExpr(n.Left)
		e.hoistExpr(n.Right)
	case *ast.UnaryExpr:
		e.hoistExpr(n.Operand)
	case *ast.SelectorExpr:
		e.hoistExpr(n.X)
	case *ast.IndexExpr:
		e.hoistExpr(n.X)
		e.hoistExpr(n.Index)
	case *ast.InterpLit:
		for _, p := range n.Parts {
			e.hoistExpr(p.Expr)
		}
	case *ast.StructLit:
		for _, f := range n.Fields {
			e.hoistExpr(f.Value)
		}
		e.hoistExprs(n.Positional)
	case *ast.SliceLit:
		e.hoistExprs(n.Elems)
	case *ast.MapLit:
		for _, ent := range n.Entries {
			e.hoistExpr(ent.Key)
			e.hoistExpr(ent.Value)
		}
	case *ast.ChanExpr:
		e.hoistExpr(n.Cap)
	case *ast.CallExpr:
		// Recurse into operands first so a nested `!` is hoisted before this
		// call renders.
		e.hoistExpr(n.Fn)
		e.hoistExprs(n.Args)
		if n.Bang {
			e.emitBangPrelude(n)
		}
	}
}

// emitBangPrelude renders one `!` call into a temp + early-return check and
// records the temp so the call site renders as it. Idempotent per call node.
func (e *Emitter) emitBangPrelude(call *ast.CallExpr) {
	if _, done := e.bangTemps[call]; done {
		return
	}
	e.bangSeq++
	val := fmt.Sprintf("__bang_%d", e.bangSeq)
	err := fmt.Sprintf("__bangerr_%d", e.bangSeq)

	// Render the call itself (its bang-children already map to their temps).
	e.line("local %s, %s = %s", val, err, e.call(call))
	e.line("if %s ~= nil then", err)
	e.indent++
	e.line("return %s", e.bangReturn(err))
	e.indent--
	e.line("end")

	e.bangTemps[call] = val
}

// bangReturn builds the early-return value list: one nil per non-error result,
// then the error temp. Matches the enclosing function's (..., error) signature
// the type checker enforced.
func (e *Emitter) bangReturn(errVar string) string {
	zeros := len(e.curResults) - 1
	if zeros < 0 {
		zeros = 0
	}
	parts := make([]string, 0, zeros+1)
	for i := 0; i < zeros; i++ {
		parts = append(parts, "nil")
	}
	parts = append(parts, errVar)
	return strings.Join(parts, ", ")
}
