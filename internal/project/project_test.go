package project

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFullManifest(t *testing.T) {
	dir := writeManifest(t, `
# a project manifest
[package]
name = "myapp"
version = "0.1.0"

[runtime]
version = "0.1.0-alpha"
luajit  = "2.1"   # pin

[deps]
json  = "1.0.0"
utils = { path = "./vendor/utils" }
http  = { version = "2.0" }
`)
	m, ok, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatal("expected a manifest")
	}
	if m.PackageName != "myapp" || m.PackageVersion != "0.1.0" {
		t.Errorf("package: got %q %q", m.PackageName, m.PackageVersion)
	}
	if m.RuntimeVersion != "0.1.0-alpha" || m.LuaJIT != "2.1" {
		t.Errorf("runtime: got version=%q luajit=%q", m.RuntimeVersion, m.LuaJIT)
	}
	if len(m.Deps) != 3 {
		t.Fatalf("deps: got %d, want 3: %+v", len(m.Deps), m.Deps)
	}
	byName := map[string]Dep{}
	for _, d := range m.Deps {
		byName[d.Name] = d
	}
	if byName["json"].Version != "1.0.0" {
		t.Errorf("json dep: %+v", byName["json"])
	}
	if byName["utils"].Path != "./vendor/utils" {
		t.Errorf("utils dep: %+v", byName["utils"])
	}
	if byName["http"].Version != "2.0" {
		t.Errorf("http dep: %+v", byName["http"])
	}
}

func TestLoadWalksUp(t *testing.T) {
	dir := writeManifest(t, "[package]\nname = \"root\"\n")
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m, ok, err := Load(sub)
	if err != nil || !ok {
		t.Fatalf("load from subdir: ok=%v err=%v", ok, err)
	}
	if m.PackageName != "root" {
		t.Errorf("expected to find parent manifest, got %q", m.PackageName)
	}
}

func TestLoadNoManifestIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	m, ok, err := Load(dir)
	if err != nil {
		t.Fatalf("missing manifest should not error: %v", err)
	}
	if ok || m != nil {
		t.Errorf("expected no manifest, got ok=%v m=%v", ok, m)
	}
}

func TestCommentsAndQuotesHandled(t *testing.T) {
	dir := writeManifest(t, `
[package]
name = "has # hash in string"  # trailing comment
`)
	m, _, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m.PackageName != "has # hash in string" {
		t.Errorf("name with hash mishandled: got %q", m.PackageName)
	}
}

func TestBareVersionDep(t *testing.T) {
	dir := writeManifest(t, "[deps]\nfoo = \"3.1.4\"\n")
	m, _, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(m.Deps) != 1 || m.Deps[0].Version != "3.1.4" {
		t.Errorf("bare version dep: %+v", m.Deps)
	}
}
