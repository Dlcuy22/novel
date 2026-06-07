// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package project loads the optional per-project novel.toml manifest.
//
// Purpose:
//
//	novel.toml lets a project pin the runtime/LuaJIT versions it expects and
//	declare dependencies (Lua-compatible: a dep is a global store module or a
//	local path). The file is optional; a project without one still builds and
//	runs. This package parses the manifest and resolves dependency names to
//	on-disk paths the module resolver can bundle.
//
// Format (a small, fixed subset of TOML, no external dependency):
//
//	[package]
//	name = "myapp"
//	version = "0.1.0"
//
//	[runtime]
//	version = "0.1.0-alpha"   # core runtime version to use
//	luajit  = "2.1"           # required LuaJIT version
//
//	[deps]
//	json  = "1.0.0"                       # a global store module (modules/novel/json)
//	utils = { path = "./vendor/utils" }   # a local module directory or file
//	http  = { version = "2.0" }           # explicit version form
//
// Key Components:
//   - Manifest: the parsed file (package/runtime metadata + deps)
//   - Load(): find and parse novel.toml by walking up from a start directory
//   - Dep: one dependency (name -> version or path)
package project

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the manifest's fixed name.
const FileName = "novel.toml"

// Dep is one declared dependency. Exactly one of Version or Path is typically
// set: Version names a global store module, Path points at a local module.
type Dep struct {
	Name    string
	Version string
	Path    string // relative to the manifest's directory when set
}

// Manifest is a parsed novel.toml.
type Manifest struct {
	Dir            string // directory containing the manifest
	PackageName    string
	PackageVersion string
	RuntimeVersion string // [runtime] version ("" if unset)
	LuaJIT         string // [runtime] luajit  ("" if unset)
	Deps           []Dep
}

// Load searches for novel.toml starting at dir and walking up to the filesystem
// root, returning the parsed manifest and true if found. A project without a
// manifest is valid: ok is false and err is nil.
func Load(dir string) (*Manifest, bool, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, false, err
	}
	for {
		candidate := filepath.Join(abs, FileName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			m, parseErr := parseFile(candidate)
			return m, true, parseErr
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, false, nil // reached the root
		}
		abs = parent
	}
}

// parseFile parses a manifest at path.
func parseFile(path string) (*Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &Manifest{Dir: filepath.Dir(path)}
	section := ""
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := stripComment(strings.TrimSpace(sc.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := splitKeyValue(line)
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected key = value", FileName, lineNo)
		}
		if err := m.apply(section, key, val); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", FileName, lineNo, err)
		}
	}
	return m, sc.Err()
}

// apply records one key/value pair into the manifest based on its section.
func (m *Manifest) apply(section, key, val string) error {
	switch section {
	case "package":
		switch key {
		case "name":
			m.PackageName = unquote(val)
		case "version":
			m.PackageVersion = unquote(val)
		}
	case "runtime":
		switch key {
		case "version":
			m.RuntimeVersion = unquote(val)
		case "luajit":
			m.LuaJIT = unquote(val)
		}
	case "deps", "dependencies":
		dep, err := parseDep(key, val)
		if err != nil {
			return err
		}
		m.Deps = append(m.Deps, dep)
	}
	return nil
}

// parseDep parses one [deps] entry: a bare version string, or an inline table
// with `version` and/or `path` keys.
func parseDep(name, val string) (Dep, error) {
	dep := Dep{Name: name}
	val = strings.TrimSpace(val)
	if strings.HasPrefix(val, "{") {
		inner := strings.TrimSpace(strings.Trim(val, "{}"))
		for _, field := range splitTopLevel(inner, ',') {
			fk, fv, ok := splitKeyValue(strings.TrimSpace(field))
			if !ok {
				continue
			}
			switch fk {
			case "version":
				dep.Version = unquote(fv)
			case "path":
				dep.Path = unquote(fv)
			}
		}
		if dep.Version == "" && dep.Path == "" {
			return dep, fmt.Errorf("dependency %q needs a version or path", name)
		}
		return dep, nil
	}
	dep.Version = unquote(val)
	return dep, nil
}

// splitKeyValue splits `key = value`, returning ok=false without an '='.
func splitKeyValue(line string) (key, val string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:eq]), strings.TrimSpace(line[eq+1:]), true
}

// splitTopLevel splits s on sep, ignoring separators inside quotes.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	var b strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
			b.WriteByte(c)
		case c == '"' || c == '\'':
			inQuote = c
			b.WriteByte(c)
		case c == sep:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		parts = append(parts, b.String())
	}
	return parts
}

// stripComment removes a trailing # comment that is not inside a quoted string.
func stripComment(line string) string {
	inQuote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '#':
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func unquote(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }
