package nvlpath

import (
	"os"
	"path/filepath"
	"testing"
)

// makeStore builds a store tree under a temp dir with the given runtime version
// and module files, returning the root.
func makeStore(t *testing.T, version string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveDefaultVersionWhenUnset(t *testing.T) {
	s := Resolve(Options{NVLPath: "/store", DefaultVersion: "0.1.0-alpha"})
	if s.Version != "0.1.0-alpha" {
		t.Errorf("version: got %q, want default", s.Version)
	}
}

func TestRuntimeDirPrefersVersioned(t *testing.T) {
	root := makeStore(t, "0.1.0-alpha", map[string]string{
		"runtime/0.1.0-alpha/novel.lua": "-- core",
	})
	s := Resolve(Options{NVLPath: root, Version: "0.1.0-alpha"})
	rt, ok := s.RuntimeDir()
	if !ok {
		t.Fatal("expected to find a runtime dir")
	}
	if rt != filepath.Join(root, "runtime", "0.1.0-alpha") {
		t.Errorf("runtime dir: got %q", rt)
	}
}

func TestRuntimeDirFallsBackToWorkDir(t *testing.T) {
	// No store; a checkout's ./runtime should be found via WorkDir.
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Resolve(Options{WorkDir: wd, DefaultVersion: "0.1.0-alpha"})
	rt, ok := s.RuntimeDir()
	if !ok || rt != filepath.Join(wd, "runtime") {
		t.Errorf("expected ./runtime fallback, got %q ok=%v", rt, ok)
	}
}

func TestLuaPathIncludesRuntimeAndLuaModules(t *testing.T) {
	root := makeStore(t, "1.0.0", map[string]string{
		"runtime/1.0.0/novel.lua": "-- core",
		"modules/lua/helper.lua":  "return {}",
	})
	s := Resolve(Options{NVLPath: root, Version: "1.0.0"})
	lp := s.LuaPath()
	wantRuntime := filepath.Join(root, "runtime", "1.0.0") + "/?.lua;"
	wantLua := filepath.Join(root, "modules", "lua") + "/?.lua;"
	if !contains(lp, wantRuntime) {
		t.Errorf("LUA_PATH missing runtime entry %q in %q", wantRuntime, lp)
	}
	if !contains(lp, wantLua) {
		t.Errorf("LUA_PATH missing lua-modules entry %q in %q", wantLua, lp)
	}
	if lp[len(lp)-2:] != ";;" {
		t.Errorf("LUA_PATH should end with ;; for default expansion, got %q", lp)
	}
}

func TestFindNovelModule(t *testing.T) {
	root := makeStore(t, "1.0.0", map[string]string{
		"modules/novel/json.nv":     "pkg json",
		"modules/novel/web/http.nv": "pkg http",
	})
	s := Resolve(Options{NVLPath: root})

	if p, ok := s.FindNovelModule("json"); !ok || p != filepath.Join(root, "modules", "novel", "json.nv") {
		t.Errorf("FindNovelModule(json): got %q ok=%v", p, ok)
	}
	if _, ok := s.FindNovelModule("web/http"); !ok {
		t.Errorf("FindNovelModule(web/http) should resolve nested module")
	}
	if _, ok := s.FindNovelModule("missing"); ok {
		t.Errorf("FindNovelModule(missing) should not resolve")
	}
}

func TestFindNovelModuleNoStore(t *testing.T) {
	s := Resolve(Options{})
	if _, ok := s.FindNovelModule("json"); ok {
		t.Errorf("no store should resolve no modules")
	}
}

func TestListModules(t *testing.T) {
	root := makeStore(t, "1.0.0", map[string]string{
		"modules/novel/json.nv":     "pkg json",
		"modules/novel/web/http.nv": "pkg http",
		"modules/lua/bit32.lua":     "return {}",
	})
	s := Resolve(Options{NVLPath: root})

	nv := s.ListNovelModules()
	if len(nv) != 2 || nv[0] != "json" || nv[1] != "web/http" {
		t.Errorf("ListNovelModules: got %v, want [json web/http]", nv)
	}
	lua := s.ListLuaModules()
	if len(lua) != 1 || lua[0] != "bit32" {
		t.Errorf("ListLuaModules: got %v, want [bit32]", lua)
	}
}

func TestStoreConfigSuppliesVersionAndLuaJIT(t *testing.T) {
	root := makeStore(t, "", map[string]string{
		"config.toml": "[runtime]\nversion = \"2.0.0\"\nluajit = \"2.1\"\n",
	})
	s := Resolve(Options{NVLPath: root, DefaultVersion: "0.1.0-alpha"})
	if s.Version != "2.0.0" {
		t.Errorf("version from config: got %q, want 2.0.0", s.Version)
	}
	if s.LuaJIT != "2.1" {
		t.Errorf("luajit pin from config: got %q, want 2.1", s.LuaJIT)
	}
}

func TestExplicitVersionOverridesConfig(t *testing.T) {
	root := makeStore(t, "", map[string]string{
		"config.toml": "[runtime]\nversion = \"2.0.0\"\n",
	})
	s := Resolve(Options{NVLPath: root, Version: "3.0.0"})
	if s.Version != "3.0.0" {
		t.Errorf("explicit version should win: got %q", s.Version)
	}
}

func TestParseLuaJITVersion(t *testing.T) {
	got := parseLuaJITVersion("LuaJIT 2.1.1700000000 -- Copyright ...")
	if got != "2.1.1700000000" {
		t.Errorf("parseLuaJITVersion: got %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		(stringIndex(haystack, needle) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
