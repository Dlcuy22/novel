// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Command novel is the Novel toolchain CLI.
//
// Purpose:
//
//	Transpiles .nv source to Lua, runs it through LuaJIT, and provides a REPL.
//	All compilation goes through internal/compiler so behavior matches the LSP.
//
// Key Components:
//   - cmdBuild(): `novel build <file.nv>` writes <file>.lua
//   - cmdRun():   `novel run <file.nv>` transpiles then executes via luajit
//   - cmdRepl():  `novel repl` transpiles and runs each line
//   - runLua(), luaPath(): pipe Lua to luajit with LUA_PATH set to the runtime
//
// Dependencies:
//   - internal/compiler: the source -> Lua pipeline
//   - external `luajit` binary on PATH for run/repl
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dlcuy22/novel/internal/compiler"
	"github.com/dlcuy22/novel/internal/nvlpath"
	"github.com/dlcuy22/novel/internal/project"
	"github.com/dlcuy22/novel/internal/types"
)

var version = types.NovelVersion

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		os.Exit(cmdBuild(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "repl":
		os.Exit(cmdRepl())
	case "env":
		os.Exit(cmdEnv())
	case "version", "-v", "--version":
		fmt.Println("novel", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `novel - the Novel language toolchain

usage:
  novel build <file.nv>    transpile to Lua next to the source
  novel run   <file.nv>    transpile and execute with luajit
  novel repl               start the interactive REPL
  novel env                show the resolved store, runtime, and LuaJIT info
  novel version            print the version
`)
}

func cmdBuild(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: novel build <file.nv>")
		return 2
	}
	src := args[0]
	lua, ok := transpileFile(src)
	if !ok {
		return 1
	}
	out := strings.TrimSuffix(src, filepath.Ext(src)) + ".lua"
	if err := os.WriteFile(out, []byte(lua), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		return 1
	}
	fmt.Println("wrote", out)
	return 0
}

func cmdRun(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: novel run <file.nv> [args...]")
		return 2
	}
	lua, ok := transpileFile(args[0])
	if !ok {
		return 1
	}
	warnLuaJITPin()
	// Everything after the .nv file is forwarded to the program as its
	// arguments (reachable via std/os args()).
	if err := runLua(lua, args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "luajit:", err)
		return 1
	}
	return 0
}

// warnLuaJITPin prints a non-fatal warning when the project/store pins a LuaJIT
// version the available luajit does not satisfy. The program still runs: the
// pin is advisory, not enforced, in this early state.
func warnLuaJITPin() {
	lj := novelStore().CheckLuaJIT("luajit")
	if lj.Found && lj.Required != "" && !lj.Satisfied {
		fmt.Fprintf(os.Stderr, "warning: luajit %s does not satisfy pinned version %s\n",
			lj.Version, lj.Required)
	}
}

// runLua executes generated Lua under luajit with LUA_PATH pointed at the Novel
// runtime so `require("novel")` resolves. The Lua is written to a temp file and
// passed as an argument (rather than piped over stdin) so the program inherits
// the real stdin: std/io readLine/readAll read fd 0, which would otherwise be
// consumed by the Lua source stream. Trailing progArgs are forwarded to the
// program (luajit places them in the `arg` table, which std/os args() reads).
func runLua(lua string, progArgs []string) error {
	f, err := os.CreateTemp("", "novel-*.lua")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(lua); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	cmdArgs := append([]string{tmp}, progArgs...)
	cmd := exec.Command("luajit", cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "LUA_PATH="+luaPath())
	return cmd.Run()
}

// novelStore resolves the global store (NVLPATH) for the current process,
// honoring a project manifest's runtime/luajit pin when one is found above the
// working directory.
func novelStore() *nvlpath.Store {
	wd, _ := os.Getwd()
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	opts := nvlpath.Options{
		NVLPath:        os.Getenv("NVLPATH"),
		ExeDir:         exeDir,
		WorkDir:        wd,
		DefaultVersion: version,
	}
	if m, ok, _ := project.Load(wd); ok {
		opts.Version = m.RuntimeVersion
		opts.LuaJIT = m.LuaJIT
	}
	return nvlpath.Resolve(opts)
}

// luaPath builds the LUA_PATH for running emitted Lua. It comes from the
// resolved store (core runtime dir + global Lua modules, ending in ";;" so Lua
// appends its default path). $NOVEL_RUNTIME is honored as a prepended override
// for backward compatibility with the pre-store layout.
func luaPath() string {
	base := novelStore().LuaPath()
	if rt := os.Getenv("NOVEL_RUNTIME"); rt != "" {
		rt = filepath.Clean(rt)
		return rt + "/?.lua;" + rt + "/?/init.lua;" + base
	}
	return base
}

// cmdEnv prints the resolved Novel environment: the store, the active runtime,
// the LuaJIT version and whether it satisfies any pin, and the global modules
// available for import. Useful for debugging path resolution and for the LSP's
// equivalent report.
func cmdEnv() int {
	store := novelStore()

	fmt.Println("novel", version)
	if store.Root != "" {
		fmt.Println("NVLPATH:          " + store.Root)
	} else {
		fmt.Println("NVLPATH:          (unset; using fallback runtime)")
	}
	fmt.Println("runtime version:  " + store.Version)
	if rt, ok := store.RuntimeDir(); ok {
		fmt.Println("runtime dir:      " + rt)
	} else {
		fmt.Println("runtime dir:      (not found)")
	}
	if d, ok := store.NovelModulesDir(); ok {
		fmt.Println("novel modules:    " + d)
	}
	if d, ok := store.LuaModulesDir(); ok {
		fmt.Println("lua modules:      " + d)
	}

	lj := store.CheckLuaJIT("luajit")
	switch {
	case !lj.Found:
		fmt.Println("luajit:           not found on PATH")
	case lj.Required == "":
		fmt.Println("luajit:           " + lj.Version)
	case lj.Satisfied:
		fmt.Printf("luajit:           %s (satisfies pin %s)\n", lj.Version, lj.Required)
	default:
		fmt.Printf("luajit:           %s (does NOT satisfy pin %s)\n", lj.Version, lj.Required)
	}

	if mods := store.ListNovelModules(); len(mods) > 0 {
		fmt.Println("global .nv modules:")
		for _, m := range mods {
			fmt.Println("  " + m)
		}
	}
	if mods := store.ListLuaModules(); len(mods) > 0 {
		fmt.Println("global .lua modules:")
		for _, m := range mods {
			fmt.Println("  " + m)
		}
	}
	return 0
}

// cmdRepl runs the interactive REPL (language-spec.md §12). Each input line is
// transpiled to a Lua chunk and streamed to a persistent luajit process running
// the repl.lua driver, so variables, functions, and imports persist across
// lines and bare expressions echo their value. There is no pkg/fn main: input
// is one statement, declaration, import, or expression at a time.
func cmdRepl() int {
	fmt.Println("Novel REPL", version)
	fmt.Println(`type ".help" for commands, ".exit" or Ctrl-D to quit`)

	r, err := startReplDriver()
	if err != nil {
		fmt.Fprintln(os.Stderr, "repl:", err)
		return 1
	}
	defer r.close()

	session := compiler.NewReplSession()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var buf strings.Builder // accumulates a multi-line input
	prompt := "novel> "
	for {
		fmt.Print(prompt)
		if !sc.Scan() {
			fmt.Println()
			return 0
		}
		line := sc.Text()

		// Dot-commands only apply at the start of a fresh input.
		if buf.Len() == 0 {
			trimmed := strings.TrimSpace(line)
			switch {
			case trimmed == "":
				continue
			case strings.HasPrefix(trimmed, "."):
				if quit := r.runCommand(trimmed, session); quit {
					return 0
				}
				continue
			}
		}

		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(line)
		src := buf.String()

		// Keep reading continuation lines while brackets are unbalanced.
		if compiler.NeedsMoreInput(src) {
			prompt = "  ...> "
			continue
		}
		prompt = "novel> "
		buf.Reset()

		res := session.Compile(src)
		if res.HasErrors() {
			fmt.Print(compiler.FormatDiagnostics("<repl>", res.Diagnostics))
			continue
		}
		if strings.TrimSpace(res.Lua) == "" {
			continue
		}
		if err := r.eval(res.Lua); err != nil {
			fmt.Fprintln(os.Stderr, "repl:", err)
			return 1
		}
	}
}

// replDriver is a handle to the persistent luajit process executing REPL chunks.
type replDriver struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// startReplDriver launches luajit running the repl.lua driver, with LUA_PATH set
// so require("repl") and require("novel") resolve against the runtime directory.
func startReplDriver() (*replDriver, error) {
	cmd := exec.Command("luajit", "-e", `require("repl")`)
	cmd.Env = append(os.Environ(), "LUA_PATH="+luaPath())
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &replDriver{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

// eval sends one Lua chunk to the driver and relays its output up to the done
// marker. The chunk is framed by a trailing EOF marker line.
func (r *replDriver) eval(lua string) error {
	if _, err := io.WriteString(r.in, lua+"\n"+compiler.ReplEOFMark+"\n"); err != nil {
		return err
	}
	for {
		line, err := r.out.ReadString('\n')
		if err != nil {
			if line != "" {
				fmt.Print(line)
			}
			return fmt.Errorf("driver exited")
		}
		if strings.TrimRight(line, "\r\n") == compiler.ReplDoneMark {
			return nil
		}
		fmt.Print(line)
	}
}

// runCommand handles a REPL dot-command and reports whether the REPL should
// quit.
func (r *replDriver) runCommand(cmd string, session *compiler.ReplSession) bool {
	switch cmd {
	case ".exit", ".quit", ".q":
		return true
	case ".help", ".h":
		fmt.Print(`commands:
  .exit, .quit   leave the REPL
  .reset         clear all session state (vars, functions, imports)
  .help          show this help

enter any Novel statement, declaration, import, or expression. a bare
expression prints its value:

  novel> 1 + 1
  2
  novel> let x = 10
  novel> x * 2
  20
`)
	case ".reset":
		session.Reset()
		if err := r.restart(); err != nil {
			fmt.Fprintln(os.Stderr, "repl:", err)
			return true
		}
		fmt.Println("session reset")
	default:
		fmt.Printf("unknown command %q (try .help)\n", cmd)
	}
	return false
}

// restart replaces the driver process with a fresh one, discarding all Lua-side
// session state. Pairs with ReplSession.Reset on the Go side.
func (r *replDriver) restart() error {
	r.close()
	nr, err := startReplDriver()
	if err != nil {
		return err
	}
	*r = *nr
	return nil
}

// close shuts the driver down: closing stdin makes the driver loop read EOF and
// exit, then we reap the process.
func (r *replDriver) close() {
	if r.in != nil {
		_ = r.in.Close()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Wait()
	}
}

func transpileFile(path string) (string, bool) {
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		return "", false
	}
	res := compiler.CompileFile(path)
	if res.HasErrors() {
		fmt.Fprint(os.Stderr, compiler.FormatDiagnostics(path, res.Diagnostics))
		return "", false
	}
	return res.Lua, true
}
