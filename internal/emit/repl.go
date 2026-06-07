// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// repl.go: REPL chunk emission.
//
// Purpose:
//   Lowers one ast.ReplUnit to a self-contained Lua chunk for the persistent
//   REPL driver (runtime/repl.lua). A single Emitter instance is reused for the
//   whole session via NewRepl, so accumulated import names and struct fields
//   stay available across lines. Top-level declarations emit as session globals
//   (see emit.go's structDecl/varDecl), and a bare expression is wrapped in
//   novel.replprint(...) so its value is echoed.

package emit

import (
	"github.com/dlcuy22/novel/internal/ast"
)

// NewRepl returns an Emitter configured for REPL session emission. Reuse the
// same instance for every line so it remembers imports (for dot-vs-colon call
// lowering) and struct fields (for positional literals).
func NewRepl() *Emitter {
	e := New()
	e.repl = true
	return e
}

// Repl lowers one REPL unit to a Lua chunk. The returned string is a complete
// chunk to hand to the driver. An empty unit yields an empty chunk.
func (e *Emitter) Repl(u *ast.ReplUnit) string {
	e.buf.Reset()
	switch {
	case u == nil:
		return ""
	case u.Import != nil:
		e.replImport(*u.Import)
	case u.Decl != nil:
		// Pre-scan a struct decl so later positional literals resolve fields.
		if s, ok := u.Decl.(*ast.StructDecl); ok {
			names := make([]string, len(s.Fields))
			for i, f := range s.Fields {
				names[i] = f.Name
			}
			e.structFields[s.Name] = names
		}
		e.decl(u.Decl)
	case u.Stmt != nil:
		e.stmt(u.Stmt)
	case u.Expr != nil:
		// Auto-print: novel.replprint echoes the value(s), quoting strings and
		// skipping a lone nil (so void calls print nothing, like Python).
		e.line("novel.replprint(%s)", e.expr(u.Expr))
	}
	return e.buf.String()
}

// replImport lowers an import to a session-global require binding, mirroring the
// file emitter but without `local` so the module persists across lines.
func (e *Emitter) replImport(imp ast.Import) {
	name := imp.Alias
	if name == "" {
		name = luaModuleName(imp.Path)
	}
	e.imports[name] = true
	e.line("%s = require(%q)", name, e.requireName(imp.Path))
}
