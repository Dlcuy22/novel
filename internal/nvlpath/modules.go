// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// modules.go: global module lookup and LuaJIT version checking.

package nvlpath

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// FindNovelModule resolves an import path to a global .nv module under
// $NVLPATH/modules/novel, with or without the .nv suffix. Returns the absolute
// path and whether it was found.
func (s *Store) FindNovelModule(importPath string) (string, bool) {
	root, ok := s.NovelModulesDir()
	if !ok {
		return "", false
	}
	base := filepath.Join(root, filepath.FromSlash(importPath))
	candidates := []string{base}
	if !strings.HasSuffix(base, ".nv") {
		candidates = []string{base + ".nv", base}
	}
	for _, c := range candidates {
		if isFile(c) {
			return c, true
		}
	}
	return "", false
}

// FindLuaModule reports whether an import path names a global .lua module under
// $NVLPATH/modules/lua. These are resolved at runtime via LUA_PATH; this lookup
// is only used to classify an import (Lua vs unknown) for diagnostics.
func (s *Store) FindLuaModule(importPath string) (string, bool) {
	root, ok := s.LuaModulesDir()
	if !ok {
		return "", false
	}
	base := filepath.Join(root, filepath.FromSlash(importPath))
	candidates := []string{base}
	if !strings.HasSuffix(base, luaExtensions) {
		candidates = []string{base + luaExtensions, base}
	}
	for _, c := range candidates {
		if isFile(c) {
			return c, true
		}
	}
	return "", false
}

// ListNovelModules returns the import paths of every global .nv module in the
// store, slash-separated and sans extension, sorted. Used by the LSP to tell
// the editor what is importable.
func (s *Store) ListNovelModules() []string {
	root, ok := s.NovelModulesDir()
	if !ok {
		return nil
	}
	return listModules(root, ".nv")
}

// ListLuaModules returns the names of every global .lua module in the store.
func (s *Store) ListLuaModules() []string {
	root, ok := s.LuaModulesDir()
	if !ok {
		return nil
	}
	return listModules(root, luaExtensions)
}

// listModules walks root collecting files with ext, returning their paths
// relative to root, slash-separated and sans extension.
func listModules(root, ext string) []string {
	var out []string
	_ = filepathWalkFiles(root, func(rel string) {
		if strings.HasSuffix(rel, ext) {
			name := strings.TrimSuffix(rel, ext)
			out = append(out, filepath.ToSlash(name))
		}
	})
	sort.Strings(out)
	return out
}

// LuaJITInfo describes the LuaJIT the toolchain will run with.
type LuaJITInfo struct {
	Binary    string // resolved binary name or path
	Version   string // reported version, e.g. "2.1.1700000000" ("" if not found)
	Required  string // the pinned version, if any
	Found     bool   // whether the binary ran
	Satisfied bool   // whether Version satisfies Required (true when unpinned)
}

// CheckLuaJIT runs `<binary> -v` and reports the version plus whether it
// satisfies the store/project pin. A pin is satisfied when the reported version
// string contains the required string (so "2.1" matches "2.1.x"); unpinned
// always satisfies.
func (s *Store) CheckLuaJIT(binary string) LuaJITInfo {
	if binary == "" {
		binary = "luajit"
	}
	info := LuaJITInfo{Binary: binary, Required: s.LuaJIT}

	out, err := exec.Command(binary, "-v").CombinedOutput()
	if err != nil {
		info.Satisfied = s.LuaJIT == "" // can't verify; only "fail" if pinned
		return info
	}
	info.Found = true
	info.Version = parseLuaJITVersion(string(out))
	info.Satisfied = s.LuaJIT == "" || strings.Contains(info.Version, s.LuaJIT)
	return info
}

// parseLuaJITVersion extracts the version token from `luajit -v` output, e.g.
// "LuaJIT 2.1.1700000000 -- ..." -> "2.1.1700000000".
func parseLuaJITVersion(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.EqualFold(f, "LuaJIT") && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return strings.TrimSpace(s)
}

// filepathWalkFiles invokes fn with the slash-relative path of every regular
// file under root. A missing root is not an error (yields no calls).
func filepathWalkFiles(root string, fn func(rel string)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		fn(rel)
		return nil
	})
}
