// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package compiler wires the Novel pipeline together.
//
// Purpose:
//
//	The single entrypoint shared by the `novel` CLI, the REPL, and the
//	language server, so diagnostics and codegen stay identical everywhere.
//	Runs source -> lexer -> parser -> type checker -> Lua emitter.
//
// Key Components:
//   - Compile(): compiles a single source string (REPL/LSP), no module graph
//   - CompileFile(): resolves the import graph from an entry .nv file and
//     bundles every reachable .nv module into one Lua output
//   - Result, Diagnostic: the compile output and a position-tagged message
//   - FormatDiagnostics(): renders diagnostics one per line for the front ends
//
// Dependencies:
//   - internal/lexer, internal/parser, internal/types, internal/emit: the
//     pipeline stages, run in order
//   - resolve.go: the module resolver feeding CompileFile
//
// Note:
//
//	Emission is skipped when any earlier stage reports an error.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/emit"
	"github.com/dlcuy22/novel/internal/lexer"
	"github.com/dlcuy22/novel/internal/nvlpath"
	"github.com/dlcuy22/novel/internal/parser"
	"github.com/dlcuy22/novel/internal/project"
	"github.com/dlcuy22/novel/internal/types"
)

// Diagnostic is a position-tagged message from any pipeline stage.
type Diagnostic struct {
	File   string // source file the diagnostic belongs to ("" for single-source)
	Line   int
	Column int
	Msg    string
	Stage  string // "lex", "parse", "type", or "resolve"
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: [%s] %s", d.Line, d.Column, d.Stage, d.Msg)
}

// Result is the output of compiling a program.
type Result struct {
	Lua         string
	Diagnostics []Diagnostic
}

// HasErrors reports whether any diagnostics were produced.
func (r Result) HasErrors() bool { return len(r.Diagnostics) > 0 }

// ParseResult is the output of parsing without emission, carrying the AST
// and the original source lines so the LSP can inspect declarations and
// extract doc-comments above them.
type ParseResult struct {
	File  *ast.File
	Lines []string
}

// ParseOnly runs lex -> parse -> type over a single source string and returns
// the AST and source lines. Emission is not performed. This is used by the LSP
// hover handler to resolve symbols without generating Lua.
func ParseOnly(src string) (ParseResult, []Diagnostic) {
	diags, file := compileSource(src, "")
	return ParseResult{File: file, Lines: strings.Split(src, "\n")}, diags
}

// Compile runs the full pipeline over a single source string and returns the
// emitted Lua plus any diagnostics. Emission is skipped when earlier stages
// report errors. Imports are lowered to bare require() calls; no .nv module
// graph is resolved (use CompileFile for that). Used by the REPL and the LSP.
func Compile(src string) Result {
	diags, file := compileSource(src, "")
	if len(diags) > 0 {
		return Result{Diagnostics: diags}
	}
	return Result{Lua: emit.New().File(file)}
}

// compileSource runs lex -> parse -> type over one source string, tagging every
// diagnostic with file, and returns the parsed AST. The AST is returned even
// when diagnostics exist so callers may inspect imports for graph resolution.
func compileSource(src, file string) ([]Diagnostic, *ast.File) {
	var diags []Diagnostic

	lx := lexer.New(src)
	_ = lx.Tokenize()
	for _, e := range lx.Errors() {
		diags = append(diags, Diagnostic{file, e.Pos.Line, e.Pos.Column, e.Msg, "lex"})
	}

	p := parser.New(src)
	f := p.ParseFile()
	for _, e := range p.Errors() {
		diags = append(diags, Diagnostic{file, e.Pos.Line, e.Pos.Column, e.Msg, "parse"})
	}

	chk := types.New()
	chk.Check(f)
	for _, e := range chk.Errors() {
		diags = append(diags, Diagnostic{file, e.Pos.Line, e.Pos.Column, e.Msg, "type"})
	}

	return diags, f
}

// CompileFile compiles an entry .nv file and every .nv module reachable through
// its imports, bundling them into a single Lua program. Imports that don't name
// a .nv file (std/* and plain Lua modules) are left as runtime require() calls.
// It discovers the global store (NVLPATH) and the nearest novel.toml so global
// modules and declared dependencies resolve. Emission is skipped if any module
// reports a diagnostic.
func CompileFile(path string) Result {
	return CompileFileWith(path, discoverResolveConfig(path))
}

// CompileFileWith is CompileFile with an explicit resolveConfig, so the store
// and project dependencies can be injected (used by tests).
func CompileFileWith(path string, cfg resolveConfig) Result {
	modules, resErrs, err := resolveGraph(path, cfg)
	if err != nil {
		return Result{Diagnostics: []Diagnostic{{
			File: path, Line: 1, Column: 1, Stage: "resolve", Msg: err.Error(),
		}}}
	}

	var diags []Diagnostic

	// Resolver problems (missing/unreadable .nv imports) come first.
	for _, re := range resErrs {
		diags = append(diags, Diagnostic{
			File: displayPath(path, modules, re.file),
			Line: re.pos.Line, Column: re.pos.Column, Stage: "resolve", Msg: re.msg,
		})
	}

	// Re-run the per-source pipeline for each module so diagnostics carry the
	// owning file. (Parsing happened in the resolver; re-parsing here keeps the
	// pipeline uniform and is cheap relative to running the program.)
	for _, m := range modules {
		file := path
		if !m.isEntry {
			file = m.absPath
		}
		md, _ := compileSource(m.src, file)
		diags = append(diags, md...)
	}

	if len(diags) > 0 {
		return Result{Diagnostics: diags}
	}

	return Result{Lua: bundle(modules)}
}

// discoverResolveConfig builds the resolver config from the real environment:
// the global store from NVLPATH and the nearest novel.toml above the entry file.
func discoverResolveConfig(entryPath string) resolveConfig {
	cfg := resolveConfig{
		store: nvlpath.Discover(types.NovelVersion),
		deps:  map[string]project.Dep{},
	}
	entryAbs, err := filepath.Abs(entryPath)
	if err != nil {
		return cfg
	}
	if m, ok, _ := project.Load(filepath.Dir(entryAbs)); ok {
		cfg.manifestDir = m.Dir
		for _, d := range m.Deps {
			cfg.deps[d.Name] = d
		}
		// A project may pin a runtime/luajit version; re-resolve the store with
		// it so the right runtime dir and LUA_PATH are used.
		if m.RuntimeVersion != "" || m.LuaJIT != "" {
			cfg.store = nvlpath.Resolve(nvlpath.Options{
				NVLPath:        os.Getenv("NVLPATH"),
				WorkDir:        filepath.Dir(entryAbs),
				Version:        m.RuntimeVersion,
				LuaJIT:         m.LuaJIT,
				DefaultVersion: types.NovelVersion,
			})
		}
	}
	return cfg
}

// displayPath returns the path to show for a diagnostic in file abs: the
// caller-supplied entry path for the entry module, else the absolute path.
func displayPath(entryPath string, modules []*module, abs string) string {
	for _, m := range modules {
		if m.absPath == abs {
			if m.isEntry {
				return entryPath
			}
			return abs
		}
	}
	return abs
}

// bundle lowers every module to Lua and stitches them into one program. Each
// dependency becomes a package.preload entry (a lazily-evaluated module
// function) registered before the entry code runs; require() in the entry then
// resolves to the bundled module instead of a file on disk. A program with no
// .nv dependencies emits exactly as the single-file pipeline would, so existing
// single-file output is unchanged.
func bundle(modules []*module) string {
	var entry *module
	var deps []*module
	byKey := map[string]*module{}
	for _, m := range modules {
		byKey[m.key] = m
		if m.isEntry {
			entry = m
		} else {
			deps = append(deps, m)
		}
	}

	entryEmitter := emit.New()
	entryEmitter.SetRequireRewrite(entry.rewrite)
	entryEmitter.SetImportedStructFields(importedStructFields(entry, byKey))
	entryLua := entryEmitter.File(entry.file)

	if len(deps) == 0 {
		return entryLua
	}

	var b strings.Builder
	b.WriteString("-- Novel bundle. Generated; do not edit by hand.\n")
	b.WriteString("-- Bundled modules are registered before the entry runs.\n\n")
	for _, m := range deps {
		me := emit.New()
		me.SetRequireRewrite(m.rewrite)
		me.SetImportedStructFields(importedStructFields(m, byKey))
		moduleLua := me.Module(m.file)
		fmt.Fprintf(&b, "package.preload[%q] = function()\n", m.key)
		b.WriteString(indentLua(moduleLua))
		b.WriteString("end\n\n")
	}
	b.WriteString(entryLua)
	return b.String()
}

// importedStructFields collects the declaration-order field names of every
// struct in each .nv module that m imports, keyed by "<binding>.<Struct>" (the
// same name the emitter uses to access the module). This lets a positional
// cross-module struct literal (geometry.Point{ 1.0, 2.0 }) resolve field names.
func importedStructFields(m *module, byKey map[string]*module) map[string][]string {
	out := map[string][]string{}
	for _, imp := range m.file.Imports {
		key, ok := m.rewrite[imp.Path]
		if !ok {
			continue // a runtime Lua import, not a bundled .nv module
		}
		dep, ok := byKey[key]
		if !ok {
			continue
		}
		binding := imp.Alias
		if binding == "" {
			binding = emit.LuaModuleName(imp.Path)
		}
		for _, d := range dep.file.Decls {
			if s, ok := d.(*ast.StructDecl); ok {
				names := make([]string, len(s.Fields))
				for i, f := range s.Fields {
					names[i] = f.Name
				}
				out[binding+"."+s.Name] = names
			}
		}
	}
	return out
}

// indentLua indents each non-empty line of a Lua chunk by two spaces, so a
// bundled module reads clearly inside its package.preload function wrapper.
func indentLua(lua string) string {
	var b strings.Builder
	for _, line := range strings.Split(lua, "\n") {
		if line == "" {
			b.WriteByte('\n')
			continue
		}
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatDiagnostics renders diagnostics one per line. Each diagnostic uses its
// own File when set (multi-module builds), falling back to the passed file
// (single-source compiles, where Diagnostic.File is empty).
func FormatDiagnostics(file string, diags []Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		name := d.File
		if name == "" {
			name = file
		}
		fmt.Fprintf(&b, "%s:%s\n", name, d.String())
	}
	return b.String()
}
