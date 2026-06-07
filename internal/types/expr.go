// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// expr.go: expression checking, call arity, type-name validation, and the `!`
// error-propagation rule.
//
// Purpose:
//   Resolves identifiers (flagging undefined references and marking locals
//   used), validates call arity against declared functions, checks struct
//   literal and annotation type names, and enforces that `!` only appears in a
//   function whose result list ends in error (spec §8.1, §13.3).

package types

import (
	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

func (c *Checker) checkExpr(x ast.Expr) {
	switch n := x.(type) {
	case nil:
		return
	case *ast.Ident:
		if !c.known(n.Name) {
			c.errorf(n.Pos, "undefined: %s", n.Name)
		}
	case *ast.BasicLit:
		// literals carry no references
	case *ast.InterpLit:
		for _, p := range n.Parts {
			if p.Expr != nil {
				c.checkExpr(p.Expr)
			}
		}
	case *ast.BinaryExpr:
		c.checkExpr(n.Left)
		c.checkExpr(n.Right)
	case *ast.UnaryExpr:
		c.checkExpr(n.Operand)
	case *ast.CallExpr:
		c.checkCall(n)
	case *ast.SelectorExpr:
		// Resolve the base; the field/method name's validity needs type info we
		// don't track, so it is left to runtime.
		c.checkExpr(n.X)
	case *ast.IndexExpr:
		c.checkExpr(n.X)
		c.checkExpr(n.Index)
	case *ast.StructLit:
		c.checkStructLit(n)
	case *ast.SliceLit:
		if n.ElemType != nil {
			c.checkType(*n.ElemType, n.Pos)
		}
		for _, e := range n.Elems {
			c.checkExpr(e)
		}
	case *ast.MapLit:
		c.checkType(n.Key, n.Pos)
		c.checkType(n.Val, n.Pos)
		for _, ent := range n.Entries {
			c.checkExpr(ent.Key)
			c.checkExpr(ent.Value)
		}
	case *ast.ChanExpr:
		c.checkType(n.Elem, n.Pos)
		if n.Cap != nil {
			c.checkExpr(n.Cap)
		}
	}
}

// checkCall validates a call: the `!` propagation context, the argument
// expressions, and (for user-defined free functions) the argument count.
func (c *Checker) checkCall(n *ast.CallExpr) {
	if n.Bang && !c.enclosingReturnsError() {
		c.errorf(n.Pos, "cannot use '!' propagation: enclosing function does not return an error")
	}

	for _, a := range n.Args {
		c.checkExpr(a)
	}

	switch fn := n.Fn.(type) {
	case *ast.Ident:
		switch {
		case isBuiltin(fn.Name):
			// recv/send/close/error: untyped builtins, no arity rule.
		case c.isLocalOrGlobal(fn.Name):
			// A binding holding a callable value; treated dynamically.
		case isPrimitive(fn.Name):
			// Type conversion like int(x); accept.
		default:
			if sig, ok := c.funcs[fn.Name]; ok {
				c.checkArity(n, fn.Name, sig)
			} else {
				c.errorf(fn.Pos, "undefined: %s", fn.Name)
			}
		}
	case *ast.SelectorExpr:
		// Method or module call: resolve the receiver/module base only.
		c.checkExpr(fn.X)
	default:
		c.checkExpr(n.Fn)
	}
}

// isLocalOrGlobal reports whether name is bound as a local (marking it used) or
// a package-level variable.
func (c *Checker) isLocalOrGlobal(name string) bool {
	if _, ok := c.resolve(name); ok {
		return true
	}
	return c.globals[name]
}

func (c *Checker) checkArity(n *ast.CallExpr, name string, sig funcSig) {
	got := len(n.Args)
	if sig.variadic {
		if got < sig.params-1 {
			c.errorf(n.Pos, "not enough arguments in call to %s: have %d, want at least %d",
				name, got, sig.params-1)
		}
		return
	}
	if got != sig.params {
		c.errorf(n.Pos, "wrong number of arguments in call to %s: have %d, want %d",
			name, got, sig.params)
	}
}

func (c *Checker) checkStructLit(n *ast.StructLit) {
	switch {
	case n.Module != "":
		// A module-qualified literal (mod.Type{...}): verify the module is in
		// scope. The type itself lives in another module, whose declarations the
		// checker does not track, so its existence is left to runtime.
		if !c.known(n.Module) {
			c.errorf(n.Pos, "undefined: %s", n.Module)
		}
	case n.Type != "" && !c.types[n.Type]:
		c.errorf(n.Pos, "undefined type: %s", n.Type)
	}
	for _, f := range n.Fields {
		c.checkExpr(f.Value)
	}
	for _, v := range n.Positional {
		c.checkExpr(v)
	}
}

// checkType validates the named parts of a type reference, descending into
// slice/array/channel/map element and key types. A zero Type (inferred) and
// the placeholder "_" pass silently.
func (c *Checker) checkType(t ast.Type, pos token.Position) {
	if t.Elem != nil {
		c.checkType(*t.Elem, pos)
	}
	if t.MapKey != nil {
		c.checkType(*t.MapKey, pos)
	}
	if t.Name == "" || t.Name == "_" || t.Name == "map" {
		return
	}
	if isPrimitive(t.Name) || c.types[t.Name] {
		return
	}
	c.errorf(pos, "undefined type: %s", t.Name)
}

// enclosingReturnsError reports whether the function currently being walked has
// a result list ending in error, the precondition for `!` propagation.
func (c *Checker) enclosingReturnsError() bool {
	if len(c.curResults) == 0 {
		return false
	}
	return c.curResults[len(c.curResults)-1].Name == "error"
}
