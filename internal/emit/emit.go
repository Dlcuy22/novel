// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package emit turns a type-checked Novel AST into Lua 5.1 source for LuaJIT.
//
// Purpose:
//
//	Lowers declarations and statements to Lua (this file) and expressions to
//	Lua (expr.go). The output requires("novel"), the runtime shim, and maps
//	Novel builtins onto it: spawn -> novel.spawn, chan/send/recv/close ->
//	novel.*, error() -> novel.error.
//
// Key Components:
//   - Emitter, New(): accumulates Lua source with indentation state
//   - File(): lowers a whole *ast.File and returns the Lua string
//
// Dependencies:
//   - internal/ast: the tree being lowered
//   - internal/token: operator kinds for assignment/inc-dec lowering
//
// Note:
//
//	File ends by emitting novel.spawn(main) + novel.run() so main can block on
//	channels and spawned green threads actually drain (language-spec.md §13.2).
package emit

import (
	"fmt"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
	"github.com/dlcuy22/novel/internal/types"
)

// Emitter renders Lua source from an AST.
type Emitter struct {
	buf     strings.Builder
	indent  int
	hasMain bool
	// module is true when lowering an imported .nv file (not the entry file).
	// In module mode top-level declarations are scoped to the module (forward-
	// declared locals) and the file returns a table of its `pub` exports
	// instead of emitting the main() driver. See emit.
	module bool
	// repl is true when emitting REPL chunks. Top-level declarations emit as
	// globals (no `local`) so they persist in the session environment across
	// lines. A single Emitter instance is reused for the whole session so
	// struct-field and import knowledge accumulates. See repl.go.
	repl bool
	// requireRewrite maps a raw import path to the name passed to require(), so
	// imports of other .nv files resolve to their bundled package.preload key
	// (e.g. "./utils" -> "utils"). Imports with no entry (std/* and plain Lua
	// modules like "bit") pass through unchanged.
	requireRewrite map[string]string
	// imports holds the local binding name of each imported module so calls
	// like `io.println(...)` lower to dot-calls (no implicit self), while
	// struct method calls lower to colon-calls (receiver passed as self).
	imports map[string]bool
	// structFields maps a struct type name to its field names in declaration
	// order, so positional literals (Rect{ 2.0, 5.0 }) can be rewritten to the
	// named form the runtime expects. Populated by a pre-scan in File.
	structFields map[string][]string
	// importedStructFields maps "module.Type" to the field order of a struct
	// declared in an imported module, so a positional cross-module literal
	// (geometry.Point{ 1.0, 2.0 }) resolves field names. Set by the bundler.
	importedStructFields map[string][]string
	// contLabels is a stack of `continue` jump-target labels, one per enclosing
	// loop that uses continue. labelSeq makes each label name unique so nested
	// loops never collide. See loopBody.
	contLabels []string
	labelSeq   int
	// curResults is the result-type list of the function currently being
	// emitted, used to size the early `return` a `!` propagation emits (one zero
	// value per non-error result, then the error).
	curResults []ast.Type
	// bangTemps maps a `!` call that has been hoisted into a temp+check prelude
	// to the temp variable holding its success value, so the call site renders
	// as that temp. bangSeq makes temp names unique. See repl.go's hoistStmt.
	bangTemps map[*ast.CallExpr]string
	bangSeq   int
}

// exportsVar is the local table an imported module assembles its `pub`
// declarations into and returns. The double-underscore prefix avoids colliding
// with user identifiers.
const exportsVar = "__novel_exports"

// New returns a fresh Emitter.
func New() *Emitter {
	return &Emitter{
		imports:              map[string]bool{},
		structFields:         map[string][]string{},
		importedStructFields: map[string][]string{},
		bangTemps:            map[*ast.CallExpr]string{},
	}
}

// SetRequireRewrite installs the import-path -> require-name map used to point
// .nv imports at their bundled module key. Must be called before File/Module.
func (e *Emitter) SetRequireRewrite(m map[string]string) { e.requireRewrite = m }

// SetImportedStructFields installs the "module.Type" -> field-order map used to
// resolve positional cross-module struct literals. Must be set before File.
func (e *Emitter) SetImportedStructFields(m map[string][]string) {
	if m != nil {
		e.importedStructFields = m
	}
}

// LuaModuleName returns the local binding name an import path lowers to (its
// final segment, sans ./ and .nv). Exposed so the bundler can key cross-module
// struct fields by the same name the emitter uses for module access.
func LuaModuleName(path string) string { return luaModuleName(path) }

// requireName returns the name to pass to require() for an import path,
// applying any rewrite (so .nv imports resolve to their bundle key).
func (e *Emitter) requireName(path string) string {
	if r, ok := e.requireRewrite[path]; ok {
		return r
	}
	return path
}

// File lowers the entry file to Lua source (globals for functions, plus the
// main() driver). Behavior is unchanged from the single-file pipeline.
func (e *Emitter) File(f *ast.File) string { return e.emit(f, false) }

// Module lowers an imported .nv file to a Lua module chunk: top-level names are
// module-scoped and the chunk returns a table of the file's `pub` exports, so a
// require() of it yields those exports.
func (e *Emitter) Module(f *ast.File) string { return e.emit(f, true) }

// emit lowers a file in either entry mode or module mode.
func (e *Emitter) emit(f *ast.File, module bool) string {
	e.module = module
	e.buf.Reset()
	e.line("-- Generated by Novel. Do not edit by hand.")
	e.line("-- Novel version: %s", types.NovelVersion)
	e.line(`local novel = require("novel")`)

	for _, imp := range f.Imports {
		name := imp.Alias
		if name == "" {
			name = luaModuleName(imp.Path)
		}
		e.imports[name] = true
		e.line(`local %s = require(%q)`, name, e.requireName(imp.Path))
	}
	e.line("")

	// Pre-scan struct declarations so positional literals can resolve field
	// names regardless of declaration order relative to their use.
	for _, d := range f.Decls {
		if s, ok := d.(*ast.StructDecl); ok {
			names := make([]string, len(s.Fields))
			for i, fld := range s.Fields {
				names[i] = fld.Name
			}
			e.structFields[s.Name] = names
		}
	}

	// In module mode, forward-declare every top-level name as a local so
	// declarations stay module-scoped (no leaked globals colliding across
	// bundled modules) while forward references still resolve.
	if e.module {
		if names := topLevelNames(f); len(names) > 0 {
			e.line("local %s", strings.Join(names, ", "))
		}
	}

	for _, d := range f.Decls {
		e.decl(d)
	}

	if e.module {
		e.emitExports(f)
		return e.buf.String()
	}

	// Drive execution: run main() as a green thread (so it can block on
	// channels) then drain the scheduler.
	if e.hasMain {
		e.line("novel.spawn(main)")
		e.line("novel.run()")
	}
	return e.buf.String()
}

// topLevelNames returns the names of every top-level declaration (free
// functions, structs, and vars) so module mode can forward-declare them.
func topLevelNames(f *ast.File) []string {
	var names []string
	for _, d := range f.Decls {
		switch n := d.(type) {
		case *ast.FuncDecl:
			if n.Receiver == nil {
				names = append(names, n.Name)
			}
		case *ast.StructDecl:
			names = append(names, n.Name)
		case *ast.VarDecl:
			names = append(names, n.Names...)
		}
	}
	return names
}

// emitExports assembles the module's `pub` declarations into a table and
// returns it. Methods ride along on their struct's table, so exporting a struct
// exports its methods too.
func (e *Emitter) emitExports(f *ast.File) {
	e.line("local %s = {}", exportsVar)
	for _, d := range f.Decls {
		switch n := d.(type) {
		case *ast.FuncDecl:
			if n.Public && n.Receiver == nil {
				e.line("%s.%s = %s", exportsVar, n.Name, n.Name)
			}
		case *ast.StructDecl:
			if n.Public {
				e.line("%s.%s = %s", exportsVar, n.Name, n.Name)
			}
		case *ast.VarDecl:
			if n.Public {
				for _, name := range n.Names {
					e.line("%s.%s = %s", exportsVar, name, name)
				}
			}
		}
	}
	e.line("return %s", exportsVar)
}

func (e *Emitter) decl(d ast.Decl) {
	switch n := d.(type) {
	case *ast.FuncDecl:
		e.funcDecl(n)
	case *ast.StructDecl:
		e.structDecl(n)
	case *ast.VarDecl:
		e.varDecl(n)
	}
}

func (e *Emitter) funcDecl(fn *ast.FuncDecl) {
	if fn.Receiver == nil && fn.Name == "main" {
		e.hasMain = true
	}
	params := make([]string, 0, len(fn.Params)+1)
	if fn.Receiver != nil {
		params = append(params, fn.Receiver.Name)
	}
	for _, p := range fn.Params {
		if p.Variadic {
			params = append(params, "...") // Lua varargs; name dropped
		} else {
			params = append(params, p.Name)
		}
	}

	name := fn.Name
	if fn.Receiver != nil {
		// Method: attach to the receiver type's method table.
		e.line("function %s.%s(%s)", fn.Receiver.Type.Name, name, strings.Join(params, ", "))
	} else {
		e.line("function %s(%s)", name, strings.Join(params, ", "))
	}
	e.indent++
	// Bind a variadic parameter to a real table so .len()/iteration work.
	for _, p := range fn.Params {
		if p.Variadic {
			e.line("local %s = { ... }", p.Name)
		}
	}
	prevResults := e.curResults
	e.curResults = fn.Results
	e.block(fn.Body)
	e.curResults = prevResults
	e.indent--
	e.line("end")
	e.line("")
}

func (e *Emitter) structDecl(s *ast.StructDecl) {
	// A struct becomes a table with a constructor-friendly metatable carrying
	// its methods. Methods are added later via `function T.method(...)`. In
	// module mode the name is already forward-declared as a local, so assign to
	// it rather than redeclaring (which would shadow it and break methods and
	// forward references bound to the outer local). In REPL mode the name is a
	// session global so it persists across lines.
	if e.module || e.repl {
		e.line("%s = {}", s.Name)
	} else {
		e.line("local %s = {}", s.Name)
	}
	e.line("%s.__index = %s", s.Name, s.Name)
	e.line("")
}

func (e *Emitter) varDecl(v *ast.VarDecl) {
	names := strings.Join(v.Names, ", ")
	// Module mode forward-declares the names; REPL mode wants session globals.
	// Both emit without `local`.
	keyword := "local "
	if e.module || e.repl {
		keyword = ""
	}
	if len(v.Values) == 0 {
		if keyword == "local " { // forward decl / globals don't need a bare decl
			e.line("local %s", names)
		}
		return
	}
	e.line("%s%s = %s", keyword, names, e.exprList(v.Values))
}

// Statements.

func (e *Emitter) block(b *ast.Block) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		// Emit any `!` error-propagation prelude (temp + early-return check)
		// before the statement that consumes the call. See bang.go.
		e.hoistStmt(s)
		e.stmt(s)
	}
}

func (e *Emitter) stmt(s ast.Stmt) {
	switch n := s.(type) {
	case *ast.Block:
		e.line("do")
		e.indent++
		e.block(n)
		e.indent--
		e.line("end")
	case *ast.LetStmt:
		e.letStmt(n)
	case *ast.AssignStmt:
		e.assignStmt(n)
	case *ast.IncDecStmt:
		op := "+"
		if n.Op == token.DEC {
			op = "-"
		}
		t := e.expr(n.Target)
		e.line("%s = %s %s 1", t, t, op)
	case *ast.ExprStmt:
		// A bare `!` call statement is fully realized by its hoisted prelude
		// (the call ran into a temp + early-return check); emitting the temp
		// alone would be an invalid bare-name Lua statement, so skip it.
		if call, ok := n.X.(*ast.CallExpr); ok && call.Bang {
			if _, hoisted := e.bangTemps[call]; hoisted {
				break
			}
		}
		e.line("%s", e.expr(n.X))
	case *ast.ReturnStmt:
		if len(n.Values) == 0 {
			e.line("return")
		} else {
			e.line("return %s", e.exprList(n.Values))
		}
	case *ast.IfStmt:
		e.ifStmt(n, "if")
	case *ast.ForStmt:
		e.forStmt(n)
	case *ast.SpawnStmt:
		e.spawnStmt(n)
	case *ast.SendStmt:
		e.line("novel.send(%s, %s)", e.expr(n.Chan), e.expr(n.Val))
	case *ast.BreakStmt:
		e.line("break")
	case *ast.ContinueStmt:
		// Lowered to a goto targeting a label at the end of the loop body
		// (emitted by emitLoopBody). LuaJIT supports goto even in 5.1 mode.
		if len(e.contLabels) > 0 {
			e.line("goto %s", e.contLabels[len(e.contLabels)-1])
		}
	}
}

// blockHasContinue reports whether b contains a continue that targets the
// enclosing loop. A continue inside a nested loop belongs to that loop, so we
// stop at ForStmt and never descend into it.
func blockHasContinue(b *ast.Block) bool {
	if b == nil {
		return false
	}
	for _, s := range b.Stmts {
		if stmtHasContinue(s) {
			return true
		}
	}
	return false
}

func stmtHasContinue(s ast.Stmt) bool {
	switch n := s.(type) {
	case *ast.ContinueStmt:
		return true
	case *ast.Block:
		return blockHasContinue(n)
	case *ast.IfStmt:
		return blockHasContinue(n.Then) || stmtHasContinue(n.Else)
	}
	return false
}

// emitLoopBody emits a loop body plus an optional post statement (classic for).
// When the body uses continue, the body is wrapped in `do ... end` so its
// locals go out of scope before the `::label::` jump target; otherwise Lua
// rejects a goto that jumps into a local's scope. The label sits before the
// post statement so `continue` still advances the induction variable.
func (e *Emitter) emitLoopBody(body *ast.Block, post ast.Stmt) {
	if blockHasContinue(body) {
		e.labelSeq++
		label := fmt.Sprintf("__continue_%d", e.labelSeq)
		e.contLabels = append(e.contLabels, label)
		e.line("do")
		e.indent++
		e.block(body)
		e.indent--
		e.line("end")
		e.line("::%s::", label)
		e.contLabels = e.contLabels[:len(e.contLabels)-1]
	} else {
		e.block(body)
	}
	if post != nil {
		e.stmt(post)
	}
}

func (e *Emitter) letStmt(n *ast.LetStmt) {
	names := strings.Join(n.Names, ", ")
	if len(n.Values) == 0 {
		e.line("local %s", names)
		return
	}
	e.line("local %s = %s", names, e.exprList(n.Values))
}

func (e *Emitter) assignStmt(n *ast.AssignStmt) {
	targets := make([]string, len(n.Targets))
	for i, t := range n.Targets {
		targets[i] = e.expr(t)
	}
	tgt := strings.Join(targets, ", ")
	val := e.exprList(n.Values)
	switch n.Op {
	case token.ASSIGN:
		e.line("%s = %s", tgt, val)
	case token.PLUSEQ:
		e.line("%s = %s + %s", tgt, tgt, val)
	case token.MINUSEQ:
		e.line("%s = %s - %s", tgt, tgt, val)
	case token.STAREQ:
		e.line("%s = %s * %s", tgt, tgt, val)
	case token.SLASHEQ:
		e.line("%s = %s / %s", tgt, tgt, val)
	}
}

func (e *Emitter) ifStmt(n *ast.IfStmt, keyword string) {
	e.line("%s %s then", keyword, e.expr(n.Cond))
	e.indent++
	e.block(n.Then)
	e.indent--
	switch els := n.Else.(type) {
	case *ast.IfStmt:
		e.ifStmt(els, "elseif")
	case *ast.Block:
		e.line("else")
		e.indent++
		e.block(els)
		e.indent--
		e.line("end")
	case nil:
		e.line("end")
	}
}

func (e *Emitter) forStmt(n *ast.ForStmt) {
	switch n.Kind {
	case ast.ForInfinite:
		e.line("while true do")
	case ast.ForWhile:
		e.line("while %s do", e.expr(n.Cond))
	case ast.ForClassic:
		e.classicFor(n)
		return
	case ast.ForRange:
		e.rangeFor(n)
		return
	}
	e.indent++
	e.emitLoopBody(n.Body, nil)
	e.indent--
	e.line("end")
}

// classicFor lowers `for init; cond; post { }`. The whole loop is wrapped in a
// `do ... end` so the induction variable is a fresh local each loop, never a
// leaked global. `for i = 0; ...` declares `i`, so an ASSIGN init is emitted as
// a `local`.
func (e *Emitter) classicFor(n *ast.ForStmt) {
	e.line("do")
	e.indent++
	if n.Init != nil {
		e.forInit(n.Init)
	}
	cond := "true"
	if n.Cond != nil {
		cond = e.expr(n.Cond)
	}
	e.line("while %s do", cond)
	e.indent++
	e.emitLoopBody(n.Body, n.Post)
	e.indent--
	e.line("end")
	e.indent--
	e.line("end")
}

// forInit emits a classic-for init clause, treating a simple `i = v` assignment
// as a loop-scoped declaration.
func (e *Emitter) forInit(s ast.Stmt) {
	if a, ok := s.(*ast.AssignStmt); ok && a.Op == token.ASSIGN {
		targets := make([]string, len(a.Targets))
		for i, t := range a.Targets {
			targets[i] = e.expr(t)
		}
		e.line("local %s = %s", strings.Join(targets, ", "), e.exprList(a.Values))
		return
	}
	e.stmt(s)
}

func (e *Emitter) rangeFor(n *ast.ForStmt) {
	iter := e.expr(n.Iter)
	key, val := n.Key, n.Val
	if val == "" {
		// `for v in xs` binds the element, not the index: use ipairs value.
		e.line("for _, %s in ipairs(%s) do", key, iter)
	} else {
		e.line("for %s, %s in ipairs(%s) do", key, val, iter)
	}
	e.indent++
	e.emitLoopBody(n.Body, nil)
	e.indent--
	e.line("end")
}

func (e *Emitter) spawnStmt(n *ast.SpawnStmt) {
	// spawn f(args) -> novel.spawn(function() f(args) end)
	e.line("novel.spawn(function() %s end)", e.expr(n.Call))
}

func (e *Emitter) line(format string, args ...any) {
	e.buf.WriteString(strings.Repeat("  ", e.indent))
	fmt.Fprintf(&e.buf, format, args...)
	e.buf.WriteByte('\n')
}

func luaModuleName(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimSuffix(path, ".nv")
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
