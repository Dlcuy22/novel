package parser

import (
	"testing"

	"github.com/dlcuy22/novel/internal/ast"
)

func TestParseImports(t *testing.T) {
	src := `import std.io
import std.math as m
import geometry`

	f := New(src).ParseFile()
	if len(f.Imports) != 3 {
		t.Fatalf("imports: got %d, want 3", len(f.Imports))
	}
	if f.Imports[0].Path != "std/io" || f.Imports[0].Alias != "" {
		t.Errorf("import 0: got %+v", f.Imports[0])
	}
	if f.Imports[1].Alias != "m" || f.Imports[1].Path != "std/math" {
		t.Errorf("import 1: got %+v", f.Imports[1])
	}
	if f.Imports[2].Path != "geometry" || f.Imports[2].Alias != "" {
		t.Errorf("import 2: got %+v", f.Imports[2])
	}
}

func TestParseNoPackageHeader(t *testing.T) {
	// Files no longer carry a `pkg` header; one parses cleanly without it.
	p := New(`import std.io`)
	p.ParseFile()
	if len(p.Errors()) != 0 {
		t.Errorf("unexpected errors for a header-less file: %v", p.Errors())
	}
}

func TestParseLegacyPkgReported(t *testing.T) {
	// A leftover `pkg main` line from older code is reported as removed.
	p := New(`pkg main
import std.io`)
	p.ParseFile()
	if len(p.Errors()) == 0 {
		t.Error("expected a migration error for a `pkg` declaration")
	}
}

func parseOK(t *testing.T, src string) *ast.File {
	t.Helper()
	p := New(src)
	f := p.ParseFile()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %v", p.Errors())
	}
	return f
}

func TestParseFuncDecl(t *testing.T) {
	f := parseOK(t, `fn add(a int, b int) int {
    ret a + b
}`)
	if len(f.Decls) != 1 {
		t.Fatalf("decls: got %d, want 1", len(f.Decls))
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatalf("decl 0: got %T, want *ast.FuncDecl", f.Decls[0])
	}
	if fn.Name != "add" || len(fn.Params) != 2 || len(fn.Results) != 1 {
		t.Errorf("func shape wrong: %+v", fn)
	}
	if len(fn.Body.Stmts) != 1 {
		t.Fatalf("body stmts: got %d, want 1", len(fn.Body.Stmts))
	}
	if _, ok := fn.Body.Stmts[0].(*ast.ReturnStmt); !ok {
		t.Errorf("stmt 0: got %T, want *ast.ReturnStmt", fn.Body.Stmts[0])
	}
}

func TestParseStructAndMultiReturn(t *testing.T) {
	f := parseOK(t, `type Point struct {
    pub x float64
    y float64
}
fn divide(a float64, b float64) (float64, error) {
    ret a / b, nil
}`)
	if len(f.Decls) != 2 {
		t.Fatalf("decls: got %d, want 2", len(f.Decls))
	}
	sd, ok := f.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf("decl 0: got %T, want *ast.StructDecl", f.Decls[0])
	}
	if len(sd.Fields) != 2 || !sd.Fields[0].Public || sd.Fields[1].Public {
		t.Errorf("struct fields wrong: %+v", sd.Fields)
	}
	fn := f.Decls[1].(*ast.FuncDecl)
	if len(fn.Results) != 2 {
		t.Errorf("results: got %d, want 2", len(fn.Results))
	}
}

func TestParseForRangeNotStructLit(t *testing.T) {
	// `for _, t in tasks {` must open a loop body, not parse `tasks{...}`.
	f := parseOK(t, `fn main() {
    for _, t in tasks {
        work(t)
    }
}`)
	fn := f.Decls[0].(*ast.FuncDecl)
	fs, ok := fn.Body.Stmts[0].(*ast.ForStmt)
	if !ok {
		t.Fatalf("stmt 0: got %T, want *ast.ForStmt", fn.Body.Stmts[0])
	}
	if fs.Kind != ast.ForRange || fs.Key != "_" || fs.Val != "t" {
		t.Errorf("range loop shape wrong: %+v", fs)
	}
}

func TestParseClassicForAndSendSugar(t *testing.T) {
	f := parseOK(t, `fn main() {
    for i = 0; i < 4; i++ {
        ch <- i
    }
}`)
	fn := f.Decls[0].(*ast.FuncDecl)
	fs := fn.Body.Stmts[0].(*ast.ForStmt)
	if fs.Kind != ast.ForClassic {
		t.Fatalf("kind: got %v, want ForClassic", fs.Kind)
	}
	if _, ok := fs.Body.Stmts[0].(*ast.SendStmt); !ok {
		t.Errorf("body stmt 0: got %T, want *ast.SendStmt", fs.Body.Stmts[0])
	}
}

func TestParseLocalStructLit(t *testing.T) {
	f := parseOK(t, `fn main() {
    let p = Point{ x: 1.0, y: 2.0 }
}`)
	fn := f.Decls[0].(*ast.FuncDecl)
	ls := fn.Body.Stmts[0].(*ast.LetStmt)
	lit, ok := ls.Values[0].(*ast.StructLit)
	if !ok {
		t.Fatalf("value: got %T, want *ast.StructLit", ls.Values[0])
	}
	if lit.Module != "" || lit.Type != "Point" || len(lit.Fields) != 2 {
		t.Errorf("local struct lit: got %+v", lit)
	}
}

func TestParseQualifiedStructLit(t *testing.T) {
	// mod.Type{ ... } must parse as a module-qualified struct literal, not a
	// selector followed by a block.
	f := parseOK(t, `fn main() {
    let p = shapes.Point{ x: 1.0, y: 2.0 }
    let q = shapes.Point{ 3.0, 4.0 }
}`)
	fn := f.Decls[0].(*ast.FuncDecl)

	named := fn.Body.Stmts[0].(*ast.LetStmt).Values[0].(*ast.StructLit)
	if named.Module != "shapes" || named.Type != "Point" || len(named.Fields) != 2 {
		t.Errorf("named qualified lit: got %+v", named)
	}

	pos := fn.Body.Stmts[1].(*ast.LetStmt).Values[0].(*ast.StructLit)
	if pos.Module != "shapes" || pos.Type != "Point" || len(pos.Positional) != 2 {
		t.Errorf("positional qualified lit: got %+v", pos)
	}
}

/*
TestParseKeywordSelectors verifies that keyword tokens can be parsed
as selector fields.
*/
func TestParseKeywordSelectors(t *testing.T) {
	f := parseOK(t, `fn main() {
    let a = ffi.fn
    let b = event.type
}`)
	fn := f.Decls[0].(*ast.FuncDecl)

	letA := fn.Body.Stmts[0].(*ast.LetStmt)
	selA, ok := letA.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("value 0: got %T, want *ast.SelectorExpr", letA.Values[0])
	}
	if selA.Name != "fn" {
		t.Errorf("selector 0: got %s, want fn", selA.Name)
	}

	letB := fn.Body.Stmts[1].(*ast.LetStmt)
	selB, ok := letB.Values[0].(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("value 1: got %T, want *ast.SelectorExpr", letB.Values[0])
	}
	if selB.Name != "type" {
		t.Errorf("selector 1: got %s, want type", selB.Name)
	}
}

