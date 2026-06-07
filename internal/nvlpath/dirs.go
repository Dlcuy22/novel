// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// dirs.go: store directory resolution and the assembled LUA_PATH.

package nvlpath

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// RuntimeDir returns the directory holding the core runtime (novel.lua,
// repl.lua) and whether it was found. It prefers the store's versioned runtime
// ($NVLPATH/runtime/<version>), then an unversioned $NVLPATH/runtime, then the
// fallback locations (next to the executable, ./runtime in a checkout).
func (s *Store) RuntimeDir() (string, bool) {
	for _, d := range s.runtimeCandidates() {
		if isDir(d) {
			return d, true
		}
	}
	return "", false
}

// runtimeCandidates lists runtime directories in priority order.
func (s *Store) runtimeCandidates() []string {
	var dirs []string
	if s.Root != "" {
		if s.Version != "" {
			dirs = append(dirs, filepath.Join(s.Root, runtimeDir, s.Version))
		}
		dirs = append(dirs, filepath.Join(s.Root, runtimeDir))
	}
	return append(dirs, s.fallbacks...)
}

// NovelModulesDir returns $NVLPATH/modules/novel and whether the store exists.
// Global .nv modules here are bundled at compile time.
func (s *Store) NovelModulesDir() (string, bool) {
	if s.Root == "" {
		return "", false
	}
	return filepath.Join(s.Root, modulesDir, novelModules), true
}

// LuaModulesDir returns $NVLPATH/modules/lua and whether the store exists.
// Global .lua modules here are resolved at runtime via LUA_PATH.
func (s *Store) LuaModulesDir() (string, bool) {
	if s.Root == "" {
		return "", false
	}
	return filepath.Join(s.Root, modulesDir, luaModules), true
}

// LuaPath assembles the LUA_PATH the toolchain runs luajit with. It includes
// the core runtime directory and the store's Lua module dir, then ends with
// ";;" so Lua appends its built-in default path. This is the "built-in
// LUA_PATH" that makes a packaged store self-contained.
func (s *Store) LuaPath() string {
	var dirs []string
	if rt, ok := s.RuntimeDir(); ok {
		dirs = append(dirs, rt)
	} else {
		// No runtime found; still include the candidates so a freshly created
		// store works once populated.
		dirs = append(dirs, s.runtimeCandidates()...)
	}
	if lm, ok := s.LuaModulesDir(); ok {
		dirs = append(dirs, lm)
	}

	var b strings.Builder
	seen := map[string]bool{}
	for _, d := range dirs {
		d = filepath.Clean(d)
		if seen[d] {
			continue
		}
		seen[d] = true
		// ?.lua finds module.lua; ?/init.lua finds package-style modules.
		b.WriteString(d + "/?.lua;")
		b.WriteString(d + "/?/init.lua;")
	}
	b.WriteString(";") // ;; -> Lua appends its default path
	return b.String()
}

// storeConfigData is the subset of $NVLPATH/config.toml this package reads.
type storeConfigData struct {
	version string // [runtime] version
	luajit  string // [runtime] luajit
}

// readStoreConfig parses the store's config.toml for the active runtime version
// and luajit pin. It uses a tiny line scanner rather than a TOML dependency,
// matching internal/project; only the [runtime] table's version/luajit keys are
// recognized. Returns ok=false if the file is absent.
func readStoreConfig(path string) (storeConfigData, bool) {
	f, err := os.Open(path)
	if err != nil {
		return storeConfigData{}, false
	}
	defer f.Close()

	var cfg storeConfigData
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := splitKeyValue(line)
		if !ok || section != "runtime" {
			continue
		}
		switch key {
		case "version":
			cfg.version = val
		case "luajit":
			cfg.luajit = val
		}
	}
	return cfg, true
}

// splitKeyValue parses `key = "value"` (or unquoted), trimming a trailing
// comment. Shared shape with internal/project's parser.
func splitKeyValue(line string) (key, val string, ok bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	val = strings.TrimSpace(line[eq+1:])
	if i := strings.IndexByte(val, '#'); i >= 0 {
		val = strings.TrimSpace(val[:i])
	}
	val = strings.Trim(val, `"'`)
	return key, val, key != ""
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
