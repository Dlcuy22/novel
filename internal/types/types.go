// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package types implements Novel's compile-time type checker.
//
// Purpose:
//
//	Where Novel's compile-time guarantees live, since the emitted Lua is
//	untyped (language-spec.md §13.3). This pass enforces the rules that can be
//	checked soundly without a full type system: undefined identifiers, unused
//	variables (a hard error per §4), assignment to const, wrong argument counts
//	for user-defined functions, unknown type names, and misuse of the `!`
//	error-propagation suffix on functions that don't return (T, error).
//
// Key Components:
//   - Checker, New(): holds accumulated semantic errors and the symbol tables
//   - Check(): walks an *ast.File recording errors
//   - Errors(): the collected []Error
//
// Dependencies:
//   - internal/ast: the tree being checked
//   - internal/token: source positions for diagnostics
//
// Note:
//
//	Numeric types mix freely at runtime (all lower to LuaJIT numbers), so this
//	pass deliberately does NOT report numeric type mismatches; doing so soundly
//	needs a real type lattice and would reject valid programs like `let f
//	float64 = anInt`. Rules here are conservative: every diagnostic must
//	correspond to a genuine error, never a false positive on working code.
package types

import (
	"fmt"
	"sort"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

const NovelVersion = "0.2.8"

// Error is a type-check error with a source position.
type Error struct {
	Pos token.Position
	Msg string
}

func (e Error) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Pos.Line, e.Pos.Column, e.Msg)
}

// funcSig records the parameter/result shape of a declared function so call
// sites can be checked for arity and `!` propagation.
type funcSig struct {
	params   int
	variadic bool
	results  int
}

// Checker performs semantic analysis over a parsed file.
type Checker struct {
	errs []Error

	// Global symbol tables, populated by a pre-scan before bodies are walked so
	// forward references (a function calling one declared later) resolve.
	funcs   map[string]funcSig // free functions by name
	types   map[string]bool    // declared struct type names
	globals map[string]bool    // top-level let/const names
	imports map[string]bool    // imported module binding names

	scopes     []*scope   // lexical scope stack for the function body being walked
	curResults []ast.Type // result types of the function currently being walked
}

// New returns a fresh Checker.
func New() *Checker {
	return &Checker{
		funcs:   map[string]funcSig{},
		types:   map[string]bool{},
		globals: map[string]bool{},
		imports: map[string]bool{},
	}
}

// Errors returns type errors collected during Check, sorted by position so
// diagnostics are stable regardless of map iteration order.
func (c *Checker) Errors() []Error {
	sort.SliceStable(c.errs, func(i, j int) bool {
		a, b := c.errs[i].Pos, c.errs[j].Pos
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return c.errs
}

func (c *Checker) errorf(pos token.Position, format string, args ...any) {
	c.errs = append(c.errs, Error{Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Check walks the file and records semantic errors (language-spec.md §13.3).
func (c *Checker) Check(f *ast.File) {
	if f == nil {
		return
	}

	for _, imp := range f.Imports {
		c.imports[importName(imp)] = true
	}

	// Pre-scan declarations so forward references resolve regardless of order.
	for _, d := range f.Decls {
		switch n := d.(type) {
		case *ast.FuncDecl:
			if n.Receiver == nil {
				c.funcs[n.Name] = funcSig{
					params:   len(n.Params),
					variadic: hasVariadic(n.Params),
					results:  len(n.Results),
				}
			}
		case *ast.StructDecl:
			c.types[n.Name] = true
		case *ast.VarDecl:
			for _, name := range n.Names {
				c.globals[name] = true
			}
		}
	}

	for _, d := range f.Decls {
		c.checkDecl(d)
	}
}

func importName(imp ast.Import) string {
	if imp.Alias != "" {
		return imp.Alias
	}
	path := imp.Path
	if len(path) > 3 && path[len(path)-3:] == ".nv" {
		path = path[:len(path)-3]
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

func hasVariadic(params []ast.Param) bool {
	for _, p := range params {
		if p.Variadic {
			return true
		}
	}
	return false
}
