package main

import (
	"testing"
)

// TestHoverLocalFunc verifies that hovering over a locally declared function
// resolves its signature and doc-comment.
func TestHoverLocalFunc(t *testing.T) {
	src := `// add sums two integers.
fn add(a int, b int) int {
    ret a + b
}

fn main() {
    let x = add(1, 2)
}
`
	// Hover over "add" in the call at line 6 (0-based), col ~12.
	info := findHoverSymbol(src, 6, 12)
	if info == nil {
		t.Fatal("expected hover info for 'add', got nil")
	}
	if info.Signature != "fn add(a int, b int) int" {
		t.Errorf("unexpected signature: %q", info.Signature)
	}
	if info.Doc != "add sums two integers." {
		t.Errorf("unexpected doc: %q", info.Doc)
	}
}

// TestHoverStdlibSelector verifies that hovering over a qualified stdlib call
// like io.println resolves to the known stdlib signature.
func TestHoverStdlibSelector(t *testing.T) {
	src := `import std.io

fn main() {
    io.println("hello")
}
`
	// "println" starts after "io." at col 7 on line 3 (0-based).
	info := findHoverSymbol(src, 3, 7)
	if info == nil {
		t.Fatal("expected hover info for 'io.println', got nil")
	}
	if info.Signature != "fn io.println(args ...any)" {
		t.Errorf("unexpected signature: %q", info.Signature)
	}
}

// TestHoverImportAlias verifies that hovering over an import alias shows the
// import statement.
func TestHoverImportAlias(t *testing.T) {
	src := `import std.io

fn main() {
    io.println("hello")
}
`
	// "io" identifier at line 3, col 4 (0-based).
	info := findHoverSymbol(src, 3, 4)
	if info == nil {
		t.Fatal("expected hover info for 'io', got nil")
	}
	if info.Signature != "import std.io" {
		t.Errorf("unexpected signature: %q", info.Signature)
	}
}

// TestHoverBuiltin verifies that hovering over a builtin function name returns
// its signature and documentation.
func TestHoverBuiltin(t *testing.T) {
	src := `fn main() {
    let e = error("oops")
}
`
	// "error" at line 1 (0-based), col 12.
	info := findHoverSymbol(src, 1, 12)
	if info == nil {
		t.Fatal("expected hover info for 'error', got nil")
	}
	if info.Signature != `fn error(msg string) error` {
		t.Errorf("unexpected signature: %q", info.Signature)
	}
}

// TestHoverNoSymbol verifies that hovering over a blank line returns nil.
func TestHoverNoSymbol(t *testing.T) {
	src := `fn main() {

}
`
	// Line 1 is blank (0-based).
	info := findHoverSymbol(src, 1, 0)
	if info != nil {
		t.Errorf("expected nil for empty position, got: %+v", info)
	}
}

// TestExtractDocComment verifies doc-comment extraction above a declaration.
func TestExtractDocComment(t *testing.T) {
	lines := []string{
		"// First line of doc.",
		"// Second line of doc.",
		"fn foo() {",
		"}",
	}
	doc := extractDocComment(lines, 3) // declLine is 1-based
	expected := "First line of doc.\nSecond line of doc."
	if doc != expected {
		t.Errorf("expected %q, got %q", expected, doc)
	}
}

// TestFormatFuncSig verifies signature formatting for functions with receivers.
func TestFormatFuncSig(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "simple function",
			src:  `fn greet(name string) { }`,
			want: "fn greet(name string)",
		},
		{
			name: "function with return",
			src:  `fn add(a int, b int) int { ret a + b }`,
			want: "fn add(a int, b int) int",
		},
		{
			name: "public function",
			src:  `pub fn hello() string { ret "hi" }`,
			want: "pub fn hello() string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hover over the function name (starts around col 3 for "fn X").
			// Find the function name position by searching after "fn ".
			info := findHoverSymbol(tt.src, 0, 4)
			if info == nil {
				// Try col 7 for "pub fn X"
				info = findHoverSymbol(tt.src, 0, 7)
			}
			if info == nil {
				t.Fatalf("expected hover info, got nil for src: %s", tt.src)
			}
			if info.Signature != tt.want {
				t.Errorf("got %q, want %q", info.Signature, tt.want)
			}
		})
	}
}

// TestHoverWaylandSelector verifies hover on wl_ui functions via alias.
func TestHoverWaylandSelector(t *testing.T) {
	src := `import std.wl_ui as wl

fn main() {
    let win = wl.createWindow("Demo", 640, 480)
}
`
	// "createWindow" starts after "wl." at col 18 on line 3 (0-based).
	info := findHoverSymbol(src, 3, 18)
	if info == nil {
		t.Fatal("expected hover info for 'wl.createWindow', got nil")
	}
	if info.Signature != "fn wl_ui.createWindow(title string, width int, height int) any" {
		t.Errorf("unexpected signature: %q", info.Signature)
	}
}
