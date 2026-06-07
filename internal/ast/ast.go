// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Package ast defines the Novel abstract syntax tree.
//
// Purpose:
//
//	The shared tree shape produced by the parser, walked by the type checker,
//	and lowered by the emitter. Pure data; no behavior.
//
// Key Components:
//   - Expr, Stmt, Decl: marker interfaces partitioning every node
//   - File: the root node (package name, imports, declarations)
//   - FuncDecl, StructDecl, VarDecl: top-level declarations
//   - Type: a structural type reference (slice/array/chan/map/named)
//
// Dependencies:
//   - internal/token: nodes carry token.Position for diagnostics and reuse
//     token.Type for operators
//
// Note:
//
//	The emitter type-switches over the concrete node types; see internal/emit.
package ast

import "github.com/dlcuy22/novel/internal/token"

// Expr is any expression node.
type Expr interface{ exprNode() }

// Stmt is any statement node.
type Stmt interface{ stmtNode() }

// Decl is any top-level declaration.
type Decl interface{ declNode() }

// File is the root node: one parsed .nv source file. Novel has no package
// declaration; a file is runnable when it declares a `main` function.
type File struct {
	Imports []Import
	Decls   []Decl
	Pos     token.Position
}

// Import is a single dotted import (`import std.io`), optionally aliased
// (`import std.math as m`). Path is stored slash-separated ("std/io") so the
// resolver and emitter treat it uniformly with on-disk module paths.
type Import struct {
	Alias string
	Path  string
	Pos   token.Position
}

// ReplUnit is one line of REPL input (language-spec.md §12). Exactly one field
// is set: an import, a declaration, a statement, or a bare expression. A bare
// expression is auto-printed by the REPL; the others are not.
type ReplUnit struct {
	Import *Import
	Decl   Decl
	Stmt   Stmt
	Expr   Expr
	Pos    token.Position
}

// Declarations.

// Param is a name+type pair (also used for method receivers).
type Param struct {
	Name     string
	Type     Type
	Variadic bool
}

// FuncDecl is a function or method declaration.
type FuncDecl struct {
	Public   bool
	Name     string
	Receiver *Param // nil for free functions
	Params   []Param
	Results  []Type
	Body     *Block
	Pos      token.Position
}

func (*FuncDecl) declNode() {}

// StructDecl is `type Name struct { ... }`.
type StructDecl struct {
	Public bool
	Name   string
	Fields []StructField
	Pos    token.Position
}

func (*StructDecl) declNode() {}

// StructField is one field in a struct declaration.
type StructField struct {
	Public   bool
	Name     string
	Type     Type
	Embedded bool // true when the field is an embedded struct (name == type)
}

// VarDecl is a top-level `let`/`const` declaration.
type VarDecl struct {
	Const  bool
	Public bool
	Names  []string
	Type   Type // nil when inferred
	Values []Expr
	Pos    token.Position
}

func (*VarDecl) declNode() {}

// Types.

// Type is a parsed type reference, kept structural enough for codegen.
type Type struct {
	Name   string // primitive or named type, e.g. "int", "Task"
	Slice  bool   // []Elem
	ArrLen int    // >0 for fixed [N]Elem
	Chan   bool   // chan<Elem>
	MapKey *Type  // non-nil for map[Key]Elem
	Elem   *Type  // element type for slice/array/chan/map
}

// Statements.

// Block is a brace-delimited sequence of statements.
type Block struct {
	Stmts []Stmt
	Pos   token.Position
}

func (*Block) stmtNode() {}

// LetStmt is a local `let`/`const` (possibly multi-name from a multi-return).
type LetStmt struct {
	Const  bool
	Names  []string
	Type   Type // zero value when inferred
	Values []Expr
	Pos    token.Position
}

func (*LetStmt) stmtNode() {}

// AssignStmt is `targets op= values` (op is ASSIGN, PLUSEQ, ...).
type AssignStmt struct {
	Targets []Expr
	Op      token.Type
	Values  []Expr
	Pos     token.Position
}

func (*AssignStmt) stmtNode() {}

// IncDecStmt is `target++` / `target--`.
type IncDecStmt struct {
	Target Expr
	Op     token.Type // INC or DEC
	Pos    token.Position
}

func (*IncDecStmt) stmtNode() {}

// ExprStmt wraps an expression used as a statement (typically a call).
type ExprStmt struct {
	X   Expr
	Pos token.Position
}

func (*ExprStmt) stmtNode() {}

// ReturnStmt is `return v1, v2, ...`.
type ReturnStmt struct {
	Values []Expr
	Pos    token.Position
}

func (*ReturnStmt) stmtNode() {}

// IfStmt is `if cond { } elif cond { } else { }`. Elifs are nested via Else.
type IfStmt struct {
	Cond Expr
	Then *Block
	Else Stmt // *Block or *IfStmt (for elif chains), or nil
	Pos  token.Position
}

func (*IfStmt) stmtNode() {}

// ForKind distinguishes the four for-loop shapes.
type ForKind int

const (
	ForInfinite ForKind = iota // for { }
	ForWhile                   // for cond { }
	ForClassic                 // for init; cond; post { }
	ForRange                   // for k, v in iter { }
)

// ForStmt covers all loop forms (spec §7.2).
type ForStmt struct {
	Kind ForKind
	Init Stmt   // ForClassic
	Cond Expr   // ForWhile, ForClassic
	Post Stmt   // ForClassic
	Key  string // ForRange (may be "_" or "")
	Val  string // ForRange (empty if only key)
	Iter Expr   // ForRange
	Body *Block
	Pos  token.Position
}

func (*ForStmt) stmtNode() {}

// SpawnStmt is `spawn call(...)`.
type SpawnStmt struct {
	Call Expr
	Pos  token.Position
}

func (*SpawnStmt) stmtNode() {}

// SendStmt is the `ch <- v` channel-send sugar.
type SendStmt struct {
	Chan Expr
	Val  Expr
	Pos  token.Position
}

func (*SendStmt) stmtNode() {}

// BreakStmt / ContinueStmt (labels not yet supported).
type BreakStmt struct{ Pos token.Position }

func (*BreakStmt) stmtNode() {}

type ContinueStmt struct{ Pos token.Position }

func (*ContinueStmt) stmtNode() {}

// Expressions.

// Ident is a name reference.
type Ident struct {
	Name string
	Pos  token.Position
}

func (*Ident) exprNode() {}

// BasicLit is an int/float/string/raw-string/bool/nil literal.
type BasicLit struct {
	Kind  token.Type // INT, FLOAT, STRING, RAWSTRING, TRUE, FALSE, NIL
	Value string
	Pos   token.Position
}

func (*BasicLit) exprNode() {}

// InterpLit is a `$"..."` string. Parts alternate literal text and exprs.
type InterpLit struct {
	Parts []InterpPart
	Pos   token.Position
}

func (*InterpLit) exprNode() {}

// InterpPart is either a literal chunk (Expr == nil) or an interpolated expr.
type InterpPart struct {
	Lit  string
	Expr Expr
}

// BinaryExpr is `left op right`.
type BinaryExpr struct {
	Op    token.Type
	Left  Expr
	Right Expr
	Pos   token.Position
}

func (*BinaryExpr) exprNode() {}

// UnaryExpr is `op operand` (e.g. -x, !ok).
type UnaryExpr struct {
	Op      token.Type
	Operand Expr
	Pos     token.Position
}

func (*UnaryExpr) exprNode() {}

// CallExpr is `fn(args)`. Bang marks the `!` error-propagation suffix.
type CallExpr struct {
	Fn   Expr
	Args []Expr
	Bang bool
	Pos  token.Position
}

func (*CallExpr) exprNode() {}

// SelectorExpr is `x.field`.
type SelectorExpr struct {
	X    Expr
	Name string
	Pos  token.Position
}

func (*SelectorExpr) exprNode() {}

// IndexExpr is `x[index]`.
type IndexExpr struct {
	X     Expr
	Index Expr
	Pos   token.Position
}

func (*IndexExpr) exprNode() {}

// StructLit is `TypeName{ field: val, ... }` or positional `TypeName{ v1, v2 }`.
// Module is the importing alias for a qualified literal `mod.TypeName{ ... }`
// (empty for a local type).
type StructLit struct {
	Module     string
	Type       string
	Fields     []FieldInit // named form
	Positional []Expr      // positional form
	Pos        token.Position
}

func (*StructLit) exprNode() {}

// FieldInit is one `name: value` in a struct literal.
type FieldInit struct {
	Name  string
	Value Expr
}

// SliceLit is `[]Elem{ e1, e2, ... }` or a bare `[e1, e2]` list.
type SliceLit struct {
	ElemType *Type // nil for bare [..] literals
	Elems    []Expr
	Pos      token.Position
}

func (*SliceLit) exprNode() {}

// MapLit is `map[K]V{ k1: v1, k2: v2 }` (empty when no entries).
type MapLit struct {
	Key     Type
	Val     Type
	Entries []MapEntry
	Pos     token.Position
}

func (*MapLit) exprNode() {}

// MapEntry is one `key: value` pair in a map literal.
type MapEntry struct {
	Key   Expr
	Value Expr
}

// ChanExpr is `chan<Elem>(cap?)`.
type ChanExpr struct {
	Elem Type
	Cap  Expr // nil for unbuffered
	Pos  token.Position
}

func (*ChanExpr) exprNode() {}
