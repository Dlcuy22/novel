// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package nvlpath resolves Novel's global store: the directory tree pointed to
// by the NVLPATH environment variable.
//
// Purpose:
//
//	The store makes a Novel install self-contained and distributable. It holds
//	the core runtime (versioned), global modules (Novel and Lua), and a
//	built-in Lua module path, so a packaged store runs without any external Lua
//	setup. This package is the single source of truth for that layout; the CLI,
//	resolver, and LSP all consult it.
//
// Layout:
//
//	$NVLPATH/
//	  config.toml            store defaults (active runtime version, luajit pin)
//	  runtime/<version>/      core runtime: novel.lua, repl.lua
//	  modules/novel/          global .nv modules  (bundled at compile time)
//	  modules/lua/            global .lua modules  (resolved at runtime)
//
// Key Components:
//   - Store: a resolved store with helpers for runtime dir, module roots, and
//     the assembled LUA_PATH
//   - Resolve(): build a Store from explicit inputs (env, exe dir, cwd)
//   - Discover(): convenience wrapper reading the real environment
//
// Note:
//
//	A source checkout has no store; its flat ./runtime directory is supported as
//	a fallback so the toolchain runs from the repo without NVLPATH set.
package nvlpath

import (
	"os"
	"path/filepath"
	"strings"
)

// DirName constants for the store layout.
const (
	runtimeDir    = "runtime"
	modulesDir    = "modules"
	novelModules  = "novel"
	luaModules    = "lua"
	storeConfig   = "config.toml"
	luaExtensions = ".lua"
)

// Store is a resolved Novel global store plus the fallback runtime locations
// used when no store is configured (a source checkout).
type Store struct {
	// Root is the store root ($NVLPATH), or "" when none is configured.
	Root string
	// Version is the active core-runtime version (from config/project, or the
	// toolchain's built-in version when unset).
	Version string
	// LuaJIT is the required LuaJIT version string, or "" when unpinned.
	LuaJIT string
	// fallbacks are extra runtime directories searched when the store has no
	// runtime/<version> dir: runtime/ next to the executable, ./runtime in a
	// checkout. They keep the toolchain working without a store.
	fallbacks []string
}

// Options are the inputs to Resolve, kept explicit so resolution is testable
// without touching the real environment.
type Options struct {
	NVLPath        string // value of $NVLPATH ("" if unset)
	ExeDir         string // directory of the running executable ("" if unknown)
	WorkDir        string // current working directory ("" to skip ./runtime)
	Version        string // requested runtime version ("" -> store/default)
	LuaJIT         string // required LuaJIT version ("" -> store/unpinned)
	DefaultVersion string // toolchain's built-in runtime version
}

// Resolve builds a Store from explicit inputs. It never fails: an absent store
// yields a Store with only fallback runtime directories.
func Resolve(o Options) *Store {
	s := &Store{
		Root:    strings.TrimSpace(o.NVLPath),
		Version: o.Version,
		LuaJIT:  o.LuaJIT,
	}

	// Store-level config can supply the active version and luajit pin when the
	// caller (project file) did not.
	if s.Root != "" {
		if cfg, ok := readStoreConfig(filepath.Join(s.Root, storeConfig)); ok {
			if s.Version == "" {
				s.Version = cfg.version
			}
			if s.LuaJIT == "" {
				s.LuaJIT = cfg.luajit
			}
		}
	}
	if s.Version == "" {
		s.Version = o.DefaultVersion
	}

	// Fallback runtime dirs (used when the store lacks runtime/<version>).
	if o.ExeDir != "" {
		s.fallbacks = append(s.fallbacks,
			filepath.Join(o.ExeDir, runtimeDir),
			// installed as <prefix>/bin/novel -> <prefix>/share/novel/runtime
			filepath.Join(filepath.Dir(o.ExeDir), "share", "novel", runtimeDir),
		)
	}
	if o.WorkDir != "" {
		s.fallbacks = append(s.fallbacks, filepath.Join(o.WorkDir, runtimeDir))
	} else {
		s.fallbacks = append(s.fallbacks, runtimeDir)
	}
	return s
}

// Discover resolves the store from the real process environment.
func Discover(defaultVersion string) *Store {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	wd, _ := os.Getwd()
	return Resolve(Options{
		NVLPath:        os.Getenv("NVLPATH"),
		ExeDir:         exeDir,
		WorkDir:        wd,
		DefaultVersion: defaultVersion,
	})
}

// HasStore reports whether a store root is configured and exists on disk.
func (s *Store) HasStore() bool {
	if s.Root == "" {
		return false
	}
	info, err := os.Stat(s.Root)
	return err == nil && info.IsDir()
}
