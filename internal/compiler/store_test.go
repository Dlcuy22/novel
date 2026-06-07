package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dlcuy22/novel/internal/nvlpath"
	"github.com/dlcuy22/novel/internal/project"
)

// storeWith builds an NVLPATH store containing the given global Novel modules
// (relative path under modules/novel -> source) and returns a resolved Store.
func storeWith(t *testing.T, novelModules map[string]string) *nvlpath.Store {
	t.Helper()
	root := t.TempDir()
	for rel, src := range novelModules {
		abs := filepath.Join(root, "modules", "novel", rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return nvlpath.Resolve(nvlpath.Options{NVLPath: root})
}

func TestGlobalNovelModuleIsBundled(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.nv": `import std.io
import json
fn main() {
    let s = json.stringify(1)
    io.println(s)
}`,
	})
	store := storeWith(t, map[string]string{
		"json.nv": `pub fn stringify(n int) string {
    ret "1"
}`,
	})

	res := CompileFileWith(filepath.Join(dir, "main.nv"), resolveConfig{store: store})
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `package.preload["@json"]`) {
		t.Errorf("global module not bundled under @json key:\n%s", res.Lua)
	}
	if !strings.Contains(res.Lua, `require("@json")`) {
		t.Errorf("entry require not rewritten to @json:\n%s", res.Lua)
	}
	// std/io is not in the store, so it stays a runtime require.
	if !strings.Contains(res.Lua, `require("std/io")`) {
		t.Errorf("std/io should remain a runtime require:\n%s", res.Lua)
	}
}

func TestLocalFileWinsOverGlobalModule(t *testing.T) {
	// A local json.nv must take precedence over a global module named json.
	dir := writeTree(t, map[string]string{
		"json.nv": `pub fn fromLocal() int {
    ret 1
}`,
		"main.nv": `import json
fn main() {
    let x = json.fromLocal()
    sink(x)
}
fn sink(n int) {
}`,
	})
	store := storeWith(t, map[string]string{
		"json.nv": `pub fn fromGlobal() int {
    ret 2
}`,
	})
	res := CompileFileWith(filepath.Join(dir, "main.nv"), resolveConfig{store: store})
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, "fromLocal") {
		t.Errorf("local module should win; expected fromLocal in:\n%s", res.Lua)
	}
	if strings.Contains(res.Lua, "fromGlobal") || strings.Contains(res.Lua, "@json") {
		t.Errorf("global module should not be used when a local file exists:\n%s", res.Lua)
	}
}

func TestPathDependencyResolves(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"vendor/utils.nv": `pub fn helper() int {
    ret 7
}`,
		"main.nv": `import utils
fn main() {
    let x = utils.helper()
    sink(x)
}
fn sink(n int) {
}`,
	})
	cfg := resolveConfig{
		deps:        map[string]project.Dep{"utils": {Name: "utils", Path: "./vendor/utils"}},
		manifestDir: dir,
	}
	res := CompileFileWith(filepath.Join(dir, "main.nv"), cfg)
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `package.preload["@dep/utils"]`) {
		t.Errorf("path dependency not bundled under @dep/utils:\n%s", res.Lua)
	}
}

func TestCrossModuleStructConstruction(t *testing.T) {
	// A struct declared in one module can be constructed (named and positional)
	// from another via mod.Type{...}; positional fields resolve from the
	// dependency's declaration order.
	dir := writeTree(t, map[string]string{
		"shapes.nv": `pub type Point struct {
    pub x float64
    pub y float64
}
pub fn (p Point) sum() float64 {
    ret p.x + p.y
}`,
		"main.nv": `import shapes
fn main() {
    let a = shapes.Point{ x: 1.0, y: 2.0 }
    let b = shapes.Point{ 3.0, 4.0 }
    sink(a.sum(), b.sum())
}
fn sink(a float64, b float64) {
}`,
	})
	res := CompileFileWith(filepath.Join(dir, "main.nv"), resolveConfig{})
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, "{ x = 1.0, y = 2.0 }, shapes.Point") {
		t.Errorf("named cross-module literal not lowered correctly:\n%s", res.Lua)
	}
	if !strings.Contains(res.Lua, "{ x = 3.0, y = 4.0 }, shapes.Point") {
		t.Errorf("positional cross-module literal not field-mapped:\n%s", res.Lua)
	}
}

func TestUnknownBareImportStillPassesThrough(t *testing.T) {
	// With a store configured but no matching module, a bare import remains a
	// runtime require (it may be a Lua library).
	dir := writeTree(t, map[string]string{
		"main.nv": `import bit
fn main() {
    let x = bit.band(1, 1)
    sink(x)
}
fn sink(n int) {
}`,
	})
	store := storeWith(t, map[string]string{})
	res := CompileFileWith(filepath.Join(dir, "main.nv"), resolveConfig{store: store})
	if res.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	if !strings.Contains(res.Lua, `require("bit")`) {
		t.Errorf("unknown bare import should stay a runtime require:\n%s", res.Lua)
	}
	if strings.Contains(res.Lua, "package.preload") {
		t.Errorf("nothing should be bundled:\n%s", res.Lua)
	}
}
