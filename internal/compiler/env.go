// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// env.go: environment description shared by the CLI and the language server.
//
// Purpose:
//   Resolves the active global store (NVLPATH) honoring a project's novel.toml
//   runtime/luajit pin, and reports it as a serializable struct. The CLI prints
//   this for `novel env`; the LSP returns it to the editor over a custom
//   `novel/environment` request so the extension can show where modules resolve.

package compiler

import (
	"os"
	"path/filepath"

	"github.com/dlcuy22/novel/internal/nvlpath"
	"github.com/dlcuy22/novel/internal/project"
	"github.com/dlcuy22/novel/internal/types"
)

// Environment is a serializable snapshot of the resolved Novel environment.
type Environment struct {
	NovelVersion   string   `json:"novelVersion"`
	StoreRoot      string   `json:"storeRoot"`      // NVLPATH, "" if unset
	RuntimeVersion string   `json:"runtimeVersion"` // active core-runtime version
	RuntimeDir     string   `json:"runtimeDir"`     // resolved runtime dir, "" if not found
	NovelModuleDir string   `json:"novelModuleDir"` // $NVLPATH/modules/novel, "" if no store
	LuaModuleDir   string   `json:"luaModuleDir"`   // $NVLPATH/modules/lua, "" if no store
	NovelModules   []string `json:"novelModules"`   // importable global .nv modules
	LuaModules     []string `json:"luaModules"`     // global .lua modules
	LuaJITBinary   string   `json:"luajitBinary"`
	LuaJITVersion  string   `json:"luajitVersion"`  // "" if luajit not found
	LuaJITRequired string   `json:"luajitRequired"` // pin from store/project, "" if none
	LuaJITOK       bool     `json:"luajitOk"`       // version satisfies the pin
	LuaPath        string   `json:"luaPath"`        // assembled LUA_PATH
	ProjectName    string   `json:"projectName"`    // novel.toml package name, "" if none
	ProjectDir     string   `json:"projectDir"`     // novel.toml directory, "" if none
}

// ResolveStore resolves the global store for work done from workdir, applying a
// novel.toml runtime/luajit pin found above workdir. It is the single store
// resolver shared by every front end so paths never diverge.
func ResolveStore(workdir string) *nvlpath.Store {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	opts := nvlpath.Options{
		NVLPath:        os.Getenv("NVLPATH"),
		ExeDir:         exeDir,
		WorkDir:        workdir,
		DefaultVersion: types.NovelVersion,
	}
	if m, ok, _ := project.Load(workdir); ok {
		opts.Version = m.RuntimeVersion
		opts.LuaJIT = m.LuaJIT
	}
	return nvlpath.Resolve(opts)
}

// DescribeEnvironment resolves and snapshots the environment for workdir.
func DescribeEnvironment(workdir string) Environment {
	store := ResolveStore(workdir)
	env := Environment{
		NovelVersion:   types.NovelVersion,
		StoreRoot:      store.Root,
		RuntimeVersion: store.Version,
		NovelModules:   store.ListNovelModules(),
		LuaModules:     store.ListLuaModules(),
		LuaPath:        store.LuaPath(),
	}
	if rt, ok := store.RuntimeDir(); ok {
		env.RuntimeDir = rt
	}
	if d, ok := store.NovelModulesDir(); ok {
		env.NovelModuleDir = d
	}
	if d, ok := store.LuaModulesDir(); ok {
		env.LuaModuleDir = d
	}
	lj := store.CheckLuaJIT("luajit")
	env.LuaJITBinary = lj.Binary
	env.LuaJITVersion = lj.Version
	env.LuaJITRequired = lj.Required
	env.LuaJITOK = lj.Satisfied

	if m, ok, _ := project.Load(workdir); ok {
		env.ProjectName = m.PackageName
		env.ProjectDir = m.Dir
	}
	return env
}
