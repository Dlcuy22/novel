// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// expr.go: expression lowering for the Lua emitter.
//
// Purpose:
//   Renders ast.Expr nodes to Lua expression strings. Maps Novel builtins and
//   method sugar onto Lua/runtime equivalents: recv/send/close/error ->
//   novel.*, xs.len() -> #xs, xs.append(v) -> table.insert. Distinguishes
//   module dot-calls (io.println) from struct colon-calls (c:area()), and
//   lowers $"..." interpolation to a tostring()-guarded concatenation.

package emit

import (
	"fmt"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/token"
)

// exprList renders comma-separated expressions (for call args, returns, etc.).
func (e *Emitter) exprList(xs []ast.Expr) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = e.expr(x)
	}
	return strings.Join(parts, ", ")
}

// expr lowers an expression to a Lua expression string.
func (e *Emitter) expr(x ast.Expr) string {
	switch n := x.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.BasicLit:
		return e.basicLit(n)
	case *ast.InterpLit:
		return e.interpLit(n)
	case *ast.BinaryExpr:
		return e.binary(n)
	case *ast.UnaryExpr:
		return e.unary(n)
	case *ast.CallExpr:
		return e.call(n)
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", e.expr(n.X), n.Name)
	case *ast.IndexExpr:
		return fmt.Sprintf("%s[%s]", e.expr(n.X), e.expr(n.Index))
	case *ast.StructLit:
		return e.structLit(n)
	case *ast.SliceLit:
		return e.sliceLit(n)
	case *ast.MapLit:
		return e.mapLit(n)
	case *ast.ChanExpr:
		if n.Cap != nil {
			return fmt.Sprintf("novel.chan(%s)", e.expr(n.Cap))
		}
		return "novel.chan()"
	}
	return "nil --[[ unhandled expr ]]"
}

func (e *Emitter) basicLit(n *ast.BasicLit) string {
	switch n.Kind {
	case token.STRING:
		return strconvQuote(n.Value)
	case token.RAWSTRING:
		// Raw strings keep contents verbatim; Lua long brackets do no escaping.
		return "[[" + n.Value + "]]"
	case token.INT:
		return stripNumSuffix(n.Value)
	case token.FLOAT:
		return stripNumSuffix(n.Value)
	case token.TRUE:
		return "true"
	case token.FALSE:
		return "false"
	case token.NIL:
		return "nil"
	}
	return n.Value
}

func (e *Emitter) binary(n *ast.BinaryExpr) string {
	op := luaBinOp(n.Op)
	return fmt.Sprintf("(%s %s %s)", e.expr(n.Left), op, e.expr(n.Right))
}

func (e *Emitter) unary(n *ast.UnaryExpr) string {
	switch n.Op {
	case token.MINUS:
		return fmt.Sprintf("-%s", e.expr(n.Operand))
	case token.NOT:
		return fmt.Sprintf("(not %s)", e.expr(n.Operand))
	}
	return e.expr(n.Operand)
}

// call lowers a call, mapping Novel builtins and the `x.len()`/`x.append()`
// method sugar onto Lua/runtime equivalents.
func (e *Emitter) call(n *ast.CallExpr) string {
	// A `!` call hoisted into a temp+check prelude renders as that temp at its
	// use site. During prelude emission the temp is not yet registered, so the
	// call itself still renders normally there. See bang.go.
	if t, ok := e.bangTemps[n]; ok {
		return t
	}

	// Method-style builtins on a receiver: xs.len(), xs.append(v).
	if sel, ok := n.Fn.(*ast.SelectorExpr); ok {
		recv := e.expr(sel.X)
		switch sel.Name {
		case "len":
			return fmt.Sprintf("#%s", recv)
		case "append":
			return fmt.Sprintf("table.insert(%s, %s)", recv, e.exprList(n.Args))
		}
		// Calls on imported modules (e.g. io.println) are plain functions, so
		// use dot-call. Calls on values are methods and use colon-call so the
		// receiver is passed as self.
		if id, ok := sel.X.(*ast.Ident); ok && e.imports[id.Name] {
			return fmt.Sprintf("%s.%s(%s)", recv, sel.Name, e.exprList(n.Args))
		}
		return fmt.Sprintf("%s:%s(%s)", recv, sel.Name, e.exprList(n.Args))
	}

	// Free-function builtins and primitive type casts.
	if id, ok := n.Fn.(*ast.Ident); ok {
		switch id.Name {
		case "recv":
			return fmt.Sprintf("novel.recv(%s)", e.exprList(n.Args))
		case "send":
			return fmt.Sprintf("novel.send(%s)", e.exprList(n.Args))
		case "close":
			return fmt.Sprintf("novel.close(%s)", e.exprList(n.Args))
		case "error":
			return fmt.Sprintf("novel.error(%s)", e.exprList(n.Args))
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"byte", "rune":
			return fmt.Sprintf("(math.modf(tonumber(%s) or 0))", e.expr(n.Args[0]))
		case "float32", "float64":
			return fmt.Sprintf("(tonumber(%s) or 0)", e.expr(n.Args[0]))
		case "string":
			return fmt.Sprintf("tostring(%s)", e.expr(n.Args[0]))
		case "bool":
			return fmt.Sprintf("(not not %s)", e.expr(n.Args[0]))
		}
	}

	return fmt.Sprintf("%s(%s)", e.expr(n.Fn), e.exprList(n.Args))
}

func (e *Emitter) interpLit(n *ast.InterpLit) string {
	if len(n.Parts) == 0 {
		return `""`
	}
	parts := make([]string, 0, len(n.Parts))
	for _, p := range n.Parts {
		if p.Expr != nil {
			// tostring() so numbers/structs concatenate without error.
			parts = append(parts, fmt.Sprintf("tostring(%s)", e.expr(p.Expr)))
		} else {
			parts = append(parts, strconvQuote(p.Lit))
		}
	}
	return "(" + strings.Join(parts, " .. ") + ")"
}

func (e *Emitter) structLit(n *ast.StructLit) string {
	// The metatable is the struct's method table: a local type name, or a
	// module-qualified `mod.Type` for an imported type (the imported module's
	// exported struct table carries its methods).
	meta := n.Type
	if n.Module != "" {
		meta = n.Module + "." + n.Type
	}

	// Named form: setmetatable({ field = val, ... }, meta). Needs no field-order
	// knowledge, so it works for imported types too.
	if len(n.Positional) == 0 {
		fields := make([]string, len(n.Fields))
		for i, f := range n.Fields {
			fields[i] = fmt.Sprintf("%s = %s", f.Name, e.expr(f.Value))
		}
		return fmt.Sprintf("setmetatable({ %s }, %s)", strings.Join(fields, ", "), meta)
	}

	// Positional form: map each value to its field name in declaration order so
	// the runtime sees the same named table either way. For an imported type the
	// field order comes from the dependency module (threaded in by the bundler).
	names := e.structFields[n.Type]
	if n.Module != "" {
		names = e.importedStructFields[n.Module+"."+n.Type]
	}
	fields := make([]string, len(n.Positional))
	for i, v := range n.Positional {
		if i < len(names) {
			fields[i] = fmt.Sprintf("%s = %s", names[i], e.expr(v))
		} else {
			// Unknown field order (struct not visible here): fall back to a
			// positional slot so we at least emit valid Lua.
			fields[i] = e.expr(v)
		}
	}
	return fmt.Sprintf("setmetatable({ %s }, %s)", strings.Join(fields, ", "), meta)
}

func (e *Emitter) sliceLit(n *ast.SliceLit) string {
	return fmt.Sprintf("{ %s }", e.exprList(n.Elems))
}

// mapLit lowers `map[K]V{ k: v, ... }` to a Lua table with bracketed keys, so
// arbitrary key expressions (not just identifiers) work: { [k] = v, ... }.
func (e *Emitter) mapLit(n *ast.MapLit) string {
	if len(n.Entries) == 0 {
		return "{}"
	}
	parts := make([]string, len(n.Entries))
	for i, ent := range n.Entries {
		parts[i] = fmt.Sprintf("[%s] = %s", e.expr(ent.Key), e.expr(ent.Value))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// helpers.

func luaBinOp(tt token.Type) string {
	switch tt {
	case token.PLUS:
		return "+"
	case token.MINUS:
		return "-"
	case token.STAR:
		return "*"
	case token.SLASH:
		return "/"
	case token.PERCENT:
		return "%"
	case token.EQ:
		return "=="
	case token.NEQ:
		return "~="
	case token.LT:
		return "<"
	case token.GT:
		return ">"
	case token.LE:
		return "<="
	case token.GE:
		return ">="
	case token.AND:
		return "and"
	case token.OR:
		return "or"
	}
	return "--[[op]]"
}

// strconvQuote renders a Lua double-quoted string literal.
func strconvQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

// stripNumSuffix drops Novel's numeric type suffixes (u, i64, f32, ...) since
// LuaJIT numbers are untyped at the value level.
func stripNumSuffix(s string) string {
	// Hex literals (0x...) have no Novel suffix and must pass through intact;
	// Lua accepts the same 0x syntax.
	if len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s
	}
	end := len(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9') && c != '.' {
			end = i
			break
		}
	}
	return s[:end]
}
