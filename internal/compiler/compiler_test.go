package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files (relative path -> contents) under a fresh temp dir and
// returns that dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCompileFileBundlesNvImport(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"mathutil.nv": `pub fn add(a int, b int) int {
    ret a + b
}
fn secret() int {
    ret 1
}`,
		"main.nv": `import std.io
import mathutil
fn main() {
    let s = mathutil.add(1, 2)
    io.println($"{s}")
}`,
	})

	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	lua := res.Lua

	checks := []string{
		`package.preload["mathutil"] = function()`,
		"__novel_exports.add = add",            // pub export present
		`local mathutil = require("mathutil")`, // entry require rewritten to bundle key
		"novel.spawn(main)",                    // entry still drives main
	}
	for _, c := range checks {
		if !strings.Contains(lua, c) {
			t.Errorf("bundle missing %q in:\n%s", c, lua)
		}
	}
	if strings.Contains(lua, "__novel_exports.secret") {
		t.Errorf("private function must not be exported:\n%s", lua)
	}
}

func TestCompileFileResolvesLocalModule(t *testing.T) {
	// Importing `util` must find util.nv next to the entry file.
	dir := writeTree(t, map[string]string{
		"util.nv": `pub fn one() int {
    ret 1
}`,
		"main.nv": `import util
fn main() {
    let x = util.one()
    sink(x)
}
fn sink(n int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `package.preload["util"]`) {
		t.Errorf("local module import not resolved:\n%s", res.Lua)
	}
}

func TestCompileFileSubpackageImportResolves(t *testing.T) {
	// A dotted import `lib.math` resolves lib/math.nv relative to the entry.
	dir := writeTree(t, map[string]string{
		"lib/math.nv": `pub fn one() int {
    ret 1
}`,
		"main.nv": `import lib.math
fn main() {
    let x = math.one()
    sink(x)
}
fn sink(n int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `package.preload["lib/math"]`) {
		t.Errorf("subpackage import not resolved:\n%s", res.Lua)
	}
}

func TestCompileFileMissingImportPassesThrough(t *testing.T) {
	// With dotted imports there is no "must be a local file" marker, so a name
	// that matches no local .nv file is treated as a runtime Lua require rather
	// than a compile error (it may be a Lua library on LUA_PATH).
	dir := writeTree(t, map[string]string{
		"main.nv": `import nope
fn main() {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("a missing import should pass through, got: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `require("nope")`) {
		t.Errorf("missing import should lower to a runtime require:\n%s", res.Lua)
	}
	if strings.Contains(res.Lua, "package.preload") {
		t.Errorf("nothing should be bundled:\n%s", res.Lua)
	}
}

func TestCompileFileMissingSubpackageImportPassesThrough(t *testing.T) {
	// A multi-segment dotted import that matches no local file also passes
	// through to a runtime require of the slash-form path.
	dir := writeTree(t, map[string]string{
		"main.nv": `import lib.math
fn main() {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("a missing subpackage import should pass through, got: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `require("lib/math")`) {
		t.Errorf("missing subpackage import should lower to a runtime require:\n%s", res.Lua)
	}
	if strings.Contains(res.Lua, "package.preload") {
		t.Errorf("nothing should be bundled:\n%s", res.Lua)
	}
}

func TestCompileFileBareImportPassesThrough(t *testing.T) {
	// A bare import naming no local .nv file (e.g. a Lua module) must not error;
	// it lowers to a plain require() resolved at runtime.
	dir := writeTree(t, map[string]string{
		"main.nv": `import bit
fn main() {
    let x = bit.band(1, 1)
    sink(x)
}
fn sink(n int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("bare import should pass through, got: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `require("bit")`) {
		t.Errorf("bare import not lowered to require:\n%s", res.Lua)
	}
	if strings.Contains(res.Lua, "package.preload") {
		t.Errorf("bare import should not be bundled:\n%s", res.Lua)
	}
}

func TestCompileFileSingleFileOutputUnchanged(t *testing.T) {
	// A program with no .nv dependencies must emit exactly what the single-file
	// pipeline produces, so existing behavior never regresses.
	src := `import std.io
fn main() {
    io.println("hi")
}`
	dir := writeTree(t, map[string]string{"main.nv": src})
	fileRes := CompileFile(filepath.Join(dir, "main.nv"))
	srcRes := Compile(src)
	if fileRes.HasErrors() || srcRes.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v / %v", fileRes.Diagnostics, srcRes.Diagnostics)
	}
	if fileRes.Lua != srcRes.Lua {
		t.Errorf("single-file bundle differs from Compile output:\n--- CompileFile ---\n%s\n--- Compile ---\n%s",
			fileRes.Lua, srcRes.Lua)
	}
}

func TestCompileFileTransitiveImports(t *testing.T) {
	// a imports b imports c; all three must be bundled.
	dir := writeTree(t, map[string]string{
		"c.nv": `pub fn cval() int {
    ret 3
}`,
		"b.nv": `import c
pub fn bval() int {
    ret c.cval()
}`,
		"main.nv": `import b
fn main() {
    let x = b.bval()
    sink(x)
}
fn sink(n int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	for _, key := range []string{`package.preload["b"]`, `package.preload["c"]`} {
		if !strings.Contains(res.Lua, key) {
			t.Errorf("missing %s in bundle:\n%s", key, res.Lua)
		}
	}
}

func TestCompileFileSharedDependencyBundledOnce(t *testing.T) {
	// main imports both util and helper; helper also imports util. util must be
	// bundled exactly once.
	dir := writeTree(t, map[string]string{
		"util.nv": `pub fn one() int {
    ret 1
}`,
		"helper.nv": `import util
pub fn two() int {
    ret util.one() + util.one()
}`,
		"main.nv": `import util
import helper
fn main() {
    let x = util.one()
    let y = helper.two()
    sink(x, y)
}
fn sink(a int, b int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if n := strings.Count(res.Lua, `package.preload["util"]`); n != 1 {
		t.Errorf("util should be bundled once, got %d:\n%s", n, res.Lua)
	}
}

func TestCompileFilePropagatesModuleDiagnostics(t *testing.T) {
	// A type error inside an imported module must surface, attributed to that
	// file.
	dir := writeTree(t, map[string]string{
		"bad.nv": `pub fn oops() int {
    let unused = 5
    ret 1
}`,
		"main.nv": `import bad
fn main() {
    let x = bad.oops()
    sink(x)
}
fn sink(n int) {
}`,
	})
	res := CompileFile(filepath.Join(dir, "main.nv"))
	if !res.HasErrors() {
		t.Fatal("expected a diagnostic from the imported module")
	}
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Msg, "declared and not used") && strings.HasSuffix(d.File, "bad.nv") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unused-var diagnostic attributed to bad.nv, got: %v", res.Diagnostics)
	}
}
