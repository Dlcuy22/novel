package parser

import (
	"testing"

	"github.com/dlcuy22/novel/internal/ast"
)

func parseReplOK(t *testing.T, src string) *ast.ReplUnit {
	t.Helper()
	p := New(src)
	u := p.ParseRepl()
	if len(p.Errors()) != 0 {
		t.Fatalf("unexpected parse errors for %q: %v", src, p.Errors())
	}
	return u
}

func TestParseReplBareExpression(t *testing.T) {
	u := parseReplOK(t, "1 + 1")
	if u.Expr == nil {
		t.Fatalf("expected a bare expression, got %+v", u)
	}
	if _, ok := u.Expr.(*ast.BinaryExpr); !ok {
		t.Errorf("expected BinaryExpr, got %T", u.Expr)
	}
}

func TestParseReplCallExpression(t *testing.T) {
	u := parseReplOK(t, "double(21)")
	if u.Expr == nil {
		t.Fatalf("expected a bare call expression, got %+v", u)
	}
	if _, ok := u.Expr.(*ast.CallExpr); !ok {
		t.Errorf("expected CallExpr, got %T", u.Expr)
	}
}

func TestParseReplLetIsDeclaration(t *testing.T) {
	// let/const must parse as a top-level decl (persisted as a session global),
	// not a statement.
	u := parseReplOK(t, "let x = 10")
	if u.Decl == nil {
		t.Fatalf("expected a declaration, got %+v", u)
	}
	if _, ok := u.Decl.(*ast.VarDecl); !ok {
		t.Errorf("expected VarDecl, got %T", u.Decl)
	}
}

func TestParseReplFuncIsDeclaration(t *testing.T) {
	u := parseReplOK(t, "fn double(n int) int { ret n * 2 }")
	if u.Decl == nil {
		t.Fatalf("expected a declaration, got %+v", u)
	}
	if _, ok := u.Decl.(*ast.FuncDecl); !ok {
		t.Errorf("expected FuncDecl, got %T", u.Decl)
	}
}

func TestParseReplImport(t *testing.T) {
	u := parseReplOK(t, `import std.math`)
	if u.Import == nil {
		t.Fatalf("expected an import, got %+v", u)
	}
	if u.Import.Path != "std/math" {
		t.Errorf("import path: got %q, want std/math", u.Import.Path)
	}
}

func TestParseReplAssignmentIsStatement(t *testing.T) {
	u := parseReplOK(t, "x = 5")
	if u.Stmt == nil {
		t.Fatalf("expected a statement, got %+v", u)
	}
	if _, ok := u.Stmt.(*ast.AssignStmt); !ok {
		t.Errorf("expected AssignStmt, got %T", u.Stmt)
	}
}

func TestParseReplControlFlowIsStatement(t *testing.T) {
	u := parseReplOK(t, "if x > 0 { f(x) }")
	if u.Stmt == nil {
		t.Fatalf("expected a statement, got %+v", u)
	}
	if _, ok := u.Stmt.(*ast.IfStmt); !ok {
		t.Errorf("expected IfStmt, got %T", u.Stmt)
	}
}

func TestParseReplEmptyInput(t *testing.T) {
	u := parseReplOK(t, "")
	if u.Import != nil || u.Decl != nil || u.Stmt != nil || u.Expr != nil {
		t.Errorf("expected an empty unit, got %+v", u)
	}
}
