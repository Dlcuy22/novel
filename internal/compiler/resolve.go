// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// resolve.go: the Novel module resolver.
//
// Purpose:
//   Walks the import graph from an entry .nv file, resolving imports that refer
//   to other .nv files (by file name, with or without the .nv suffix) so they
//   can be bundled into one Lua output. Imports that don't name a .nv file
//   (std/* and plain Lua modules like "bit") are left untouched: they keep
//   lowering to a bare require() resolved on LUA_PATH at runtime.
//
// Key Components:
//   - resolveGraph(): entry path -> ordered modules + per-file require rewrites
//   - module: one resolved .nv file (key, path, parsed AST, import rewrites)
//
// Design:
//   Each .nv module gets a canonical key: its path relative to the entry file's
//   directory, sans extension, slash-separated. That key is both the
//   package.preload bucket the bundler fills and the argument the importing
//   file's require() is rewritten to, so two files referring to the same module
//   by different relative spellings still share one instance.

package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/nvlpath"
	"github.com/dlcuy22/novel/internal/parser"
	"github.com/dlcuy22/novel/internal/project"
	"github.com/dlcuy22/novel/internal/token"
)

// module is one resolved .nv source file in the import graph.
type module struct {
	key       string            // canonical require/preload key ("" for entry)
	absPath   string            // absolute file path
	src       string            // source text
	file      *ast.File         // parsed AST
	isEntry   bool              // the program entry (lowered with the main driver)
	rewrite   map[string]string // raw import path -> require key (for .nv imports)
	parseErrs []parser.Error
}

// resolveConfig carries the optional global store and project dependencies used
// when an import is not a local file: a bare import may name a global Novel
// module ($NVLPATH/modules/novel) or a declared dependency in novel.toml.
type resolveConfig struct {
	store       *nvlpath.Store
	deps        map[string]project.Dep // dependency name -> spec
	manifestDir string                 // novel.toml directory (for path deps)
}

// resolveError is a problem found while resolving imports (a missing relative
// import or an unreadable file), tagged with the position of the offending
// import in the file that referenced it.
type resolveError struct {
	pos  token.Position
	file string // absolute path of the file containing the bad import
	msg  string
}

// resolveGraph reads and parses the entry file and every .nv file reachable
// from it, in deterministic order (entry first). Reachable files include local
// .nv imports, global Novel modules from the store, and path/version
// dependencies from novel.toml. Returned modules carry their own import-rewrite
// map. Resolution problems are returned separately so the compiler can surface
// them as diagnostics; a failure to read the entry file is a hard error.
func resolveGraph(entryPath string, cfg resolveConfig) ([]*module, []resolveError, error) {
	entryAbs, err := filepath.Abs(entryPath)
	if err != nil {
		return nil, nil, err
	}
	entryDir := filepath.Dir(entryAbs)

	entrySrc, err := os.ReadFile(entryAbs)
	if err != nil {
		return nil, nil, fmt.Errorf("read entry: %w", err)
	}

	var (
		modules []*module
		resErrs []resolveError
		visited = map[string]*module{}
		queue   []string
	)

	enqueue := func(abs, key string, isEntry bool, src string) *module {
		if m, ok := visited[abs]; ok {
			return m
		}
		p := parser.New(src)
		f := p.ParseFile()
		m := &module{
			key:       key,
			absPath:   abs,
			src:       src,
			file:      f,
			isEntry:   isEntry,
			rewrite:   map[string]string{},
			parseErrs: p.Errors(),
		}
		visited[abs] = m
		modules = append(modules, m)
		queue = append(queue, abs)
		return m
	}

	enqueue(entryAbs, "", true, string(entrySrc))

	for len(queue) > 0 {
		abs := queue[0]
		queue = queue[1:]
		m := visited[abs]
		dir := filepath.Dir(abs)

		for _, imp := range m.file.Imports {
			target, key, ok := resolveModule(dir, entryDir, imp.Path, cfg)
			if !ok {
				// Relative imports must name a .nv file; a miss is an error.
				// Bare imports that miss are assumed to be Lua/stdlib modules
				// (resolved at runtime via LUA_PATH) and pass through untouched.
				if isRelative(imp.Path) {
					resErrs = append(resErrs, resolveError{
						pos:  imp.Pos,
						file: abs,
						msg:  fmt.Sprintf("cannot find module %q", imp.Path),
					})
				}
				continue
			}
			src, err := os.ReadFile(target)
			if err != nil {
				resErrs = append(resErrs, resolveError{
					pos:  imp.Pos,
					file: abs,
					msg:  fmt.Sprintf("cannot read module %q: %v", imp.Path, err),
				})
				continue
			}
			dep := enqueue(target, key, false, string(src))
			m.rewrite[imp.Path] = dep.key
		}
	}

	return modules, resErrs, nil
}

// resolveModule resolves one import path to a .nv file to bundle, returning the
// absolute path and the require/preload key to use. Resolution order:
//  1. a local .nv file (relative to the importer, then the entry directory);
//  2. a novel.toml dependency (a path dep, or a version dep -> store module);
//  3. a global Novel module in the store ($NVLPATH/modules/novel).
//
// A miss returns ok=false; the caller treats a relative miss as an error and a
// bare miss as a runtime Lua require.
func resolveModule(importerDir, entryDir, path string, cfg resolveConfig) (abs, key string, ok bool) {
	if a, found := resolveNvFile(importerDir, entryDir, path); found {
		return a, moduleKey(entryDir, a), true
	}
	if isRelative(path) {
		return "", "", false
	}

	// novel.toml dependency by this name.
	if dep, declared := cfg.deps[path]; declared {
		if dep.Path != "" {
			cand := filepath.Join(cfg.manifestDir, dep.Path)
			if a, found := resolveExplicitNv(cand); found {
				return a, "@dep/" + path, true
			}
		}
		if cfg.store != nil {
			if a, found := cfg.store.FindNovelModule(path); found {
				return a, "@" + path, true
			}
		}
	}

	// Global Novel module in the store, even without a novel.toml entry.
	if cfg.store != nil {
		if a, found := cfg.store.FindNovelModule(path); found {
			return a, "@" + path, true
		}
	}
	return "", "", false
}

// resolveExplicitNv resolves a path-dependency target to a .nv file: the path
// itself, or that path plus .nv.
func resolveExplicitNv(base string) (string, bool) {
	candidates := []string{base}
	if !strings.HasSuffix(base, ".nv") {
		candidates = []string{base + ".nv", base}
	}
	for _, c := range candidates {
		if isFile(c) {
			if a, err := filepath.Abs(c); err == nil {
				return a, true
			}
		}
	}
	return "", false
}

// isRelative reports whether an import path explicitly names a local file
// (begins with ./ or ../), as opposed to a bare module name.
func isRelative(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// moduleKey is a file's canonical name: its path relative to the entry
// directory, with the .nv extension dropped and separators normalized to "/".
// Falls back to the cleaned absolute path if a relative path can't be formed.
func moduleKey(entryDir, abs string) string {
	rel, err := filepath.Rel(entryDir, abs)
	if err != nil {
		rel = abs
	}
	rel = strings.TrimSuffix(rel, ".nv")
	return filepath.ToSlash(rel)
}

// resolveNvFile tries to resolve an import path to a .nv file on disk. Relative
// paths (./ , ../) resolve against the importing file's directory; bare paths
// try the importing directory first, then the entry directory. It accepts the
// path with or without a trailing .nv. Returns the absolute path and whether a
// file was found.
func resolveNvFile(importerDir, entryDir, path string) (string, bool) {
	var bases []string
	if isRelative(path) {
		bases = append(bases, filepath.Join(importerDir, path))
	} else {
		bases = append(bases,
			filepath.Join(importerDir, path),
			filepath.Join(entryDir, path),
		)
	}
	for _, base := range bases {
		// Explicit .nv, or the bare name plus .nv.
		candidates := []string{base}
		if !strings.HasSuffix(base, ".nv") {
			candidates = []string{base + ".nv", base}
		}
		for _, c := range candidates {
			if isFile(c) {
				abs, err := filepath.Abs(c)
				if err != nil {
					continue
				}
				return abs, true
			}
		}
	}
	return "", false
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
