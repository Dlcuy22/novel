// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// scope.go: lexical scoping and the declaration/statement/expression walk for
// the type checker.
//
// Purpose:
//   Tracks block-scoped bindings so the checker can flag undefined identifiers,
//   unused locals (spec §4), and assignment to const. A scope stack is pushed
//   per function body and per nested block; locals must be used before their
//   scope closes or a hard error is reported.

package types

import (
	"github.com/dlcuy22/novel/internal/token"
)

// binding is one named local in a scope.
type binding struct {
	pos     token.Position
	isConst bool
	used    bool
}

// scope is one lexical block's bindings.
type scope struct {
	vars map[string]*binding
}

func newScope() *scope { return &scope{vars: map[string]*binding{}} }

func (c *Checker) push() { c.scopes = append(c.scopes, newScope()) }

// pop closes the innermost scope, reporting any local that was never used
// (spec §4: unused variables are a compile-time error). The blank name "_" is
// exempt, matching range loops and intentional discards.
func (c *Checker) pop() {
	s := c.scopes[len(c.scopes)-1]
	for name, b := range s.vars {
		if name != "_" && !b.used {
			c.errorf(b.pos, "declared and not used: %s", name)
		}
	}
	c.scopes = c.scopes[:len(c.scopes)-1]
}

// declare binds name in the innermost scope. Re-declaring a name in the same
// scope shadows the earlier binding (and would have already had its use status
// settled); shadowing across scopes is allowed per spec §4.
func (c *Checker) declare(name string, pos token.Position, isConst bool) {
	if name == "" {
		return
	}
	s := c.scopes[len(c.scopes)-1]
	s.vars[name] = &binding{pos: pos, isConst: isConst}
}

// resolve finds a binding by walking scopes inner to outer, marking it used.
// Returns the binding and whether it was found as a local.
func (c *Checker) resolve(name string) (*binding, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if b, ok := c.scopes[i].vars[name]; ok {
			b.used = true
			return b, true
		}
	}
	return nil, false
}

// known reports whether name refers to anything in scope: a local, a global,
// a function, a type, an import, or a builtin. Used to flag undefined idents.
func (c *Checker) known(name string) bool {
	if _, ok := c.resolve(name); ok {
		return true
	}
	if c.globals[name] || c.imports[name] {
		return true
	}
	if _, ok := c.funcs[name]; ok {
		return true
	}
	if c.types[name] {
		return true
	}
	return isBuiltin(name) || isPrimitive(name)
}

// isBuiltin reports whether name is a built-in callable available everywhere.
func isBuiltin(name string) bool {
	switch name {
	case "recv", "send", "close", "error", "len", "append", "spawn":
		return true
	}
	return false
}

// isPrimitive reports whether name is a built-in type name, which may appear
// as a bare identifier in type positions and conversions. Includes `any`, the
// dynamic escape hatch for values whose shape the checker can't track (e.g. a
// table handed back from a Lua stdlib module, like an http request or a socket
// connection); `any` is erased in emitted Lua exactly like every other type.
func isPrimitive(name string) bool {
	switch name {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "bool", "string", "byte", "rune", "error",
		"any":
		return true
	}
	return false
}
