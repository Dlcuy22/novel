// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// hover.go provides AST node lookup and symbol resolution for LSP hover.
//
// Purpose:
//   Given a cursor position (0-based line, character), walk the AST produced by
//   compiler.ParseOnly to find the identifier under the cursor and resolve it
//   to a declaration. Then format a Markdown tooltip showing the signature and
//   any doc-comments above the declaration.
//
// Key Components:
//   - SymbolInfo: resolved symbol metadata (signature, doc, position range)
//   - findHoverSymbol(): top-level entry; returns a SymbolInfo or nil
//   - findIdentAt(): locates the Ident or SelectorExpr node under the cursor
//   - resolveIdent()/resolveSelector(): resolve an identifier/selector to a decl
//   - extractDocComment(): pulls the comment block immediately above a decl line
//   - formatType()/formatParams()/formatResults(): type-to-string rendering
//
// Dependencies:
//   - internal/ast: the node types walked
//   - internal/compiler: ParseOnly for obtaining the AST and source lines
package main

import (
	"fmt"
	"strings"

	"github.com/dlcuy22/novel/internal/ast"
	"github.com/dlcuy22/novel/internal/compiler"
)

// SymbolInfo holds the resolved hover information for a symbol.
type SymbolInfo struct {
	Signature string // e.g. "fn createWindow(title string, w int, h int) any"
	Doc       string // extracted doc-comment, may be empty
	// Range of the identifier in the source (0-based, for the LSP response).
	StartLine int
	StartChar int
	EndLine   int
	EndChar   int
}

/*
findHoverSymbol resolves the symbol under the cursor and returns its info.

    params:
          src:  full document text
          line: 0-based cursor line
          col:  0-based cursor character
    returns:
          *SymbolInfo: nil when no symbol is found at the position
*/
func findHoverSymbol(src string, line, col int) *SymbolInfo {
	pr, _ := compiler.ParseOnly(src)
	if pr.File == nil {
		return nil
	}

	// Convert from 0-based (LSP) to 1-based (AST positions).
	astLine := line + 1
	astCol := col + 1

	// Try to find an identifier or selector at the cursor position.
	name, module, identRange := findIdentAt(pr.File, pr.Lines, astLine, astCol)
	if name == "" {
		return nil
	}

	var info *SymbolInfo

	if module != "" {
		// Qualified access: module.name (e.g., io.println, wl.createWindow).
		info = resolveSelector(pr, module, name)
	} else {
		// Unqualified identifier: check local declarations.
		info = resolveIdent(pr, name)
	}

	if info == nil {
		return nil
	}

	// Attach the identifier range so the client can highlight it.
	info.StartLine = identRange[0]
	info.StartChar = identRange[1]
	info.EndLine = identRange[2]
	info.EndChar = identRange[3]

	return info
}

// findIdentAt walks the AST looking for an Ident or SelectorExpr whose position
// overlaps (astLine, astCol). Returns (name, module, [startLine, startChar,
// endLine, endChar]) where module is empty for plain identifiers. All returned
// positions are 0-based for LSP.
func findIdentAt(file *ast.File, lines []string, astLine, astCol int) (name, module string, identRange [4]int) {
	// Walk all declarations looking for identifiers in the right location.
	for _, d := range file.Decls {
		n, m, r, ok := findIdentInDecl(d, lines, astLine, astCol)
		if ok {
			return n, m, r
		}
	}
	return "", "", [4]int{}
}

// findIdentInDecl searches within a single declaration for an ident at (line, col).
func findIdentInDecl(d ast.Decl, lines []string, line, col int) (name, module string, r [4]int, ok bool) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		// d.Pos is at the `fn` keyword, not the name. Compute the real column
		// from the source line so we match the correct span.
		if d.Pos.Line == line && d.Pos.Line-1 < len(lines) {
			srcLine := lines[d.Pos.Line-1]
			nameIdx := strings.Index(srcLine, d.Name+"(")
			if nameIdx < 0 {
				// Fallback: find the name anywhere in the line.
				nameIdx = strings.Index(srcLine, d.Name)
			}
			if nameIdx >= 0 {
				nameCol := nameIdx + 1 // 1-based
				if col >= nameCol && col < nameCol+len(d.Name) {
					return d.Name, "", identRange(line, nameCol, d.Name), true
				}
			}
		}
		// Walk the function body.
		if d.Body != nil {
			n, m, r, ok := findIdentInBlock(d.Body, lines, line, col)
			if ok {
				return n, m, r, true
			}
		}
	case *ast.StructDecl:
		if posOverlaps(d.Pos.Line, d.Pos.Column, d.Name, line, col) {
			return d.Name, "", identRange(d.Pos.Line, d.Pos.Column, d.Name), true
		}
	case *ast.VarDecl:
		for _, vn := range d.Names {
			if posOverlaps(d.Pos.Line, d.Pos.Column, vn, line, col) {
				return vn, "", identRange(d.Pos.Line, d.Pos.Column, vn), true
			}
		}
	}
	return "", "", [4]int{}, false
}

// findIdentInBlock searches a statement block for identifiers at (line, col).
func findIdentInBlock(block *ast.Block, lines []string, line, col int) (name, module string, r [4]int, ok bool) {
	for _, stmt := range block.Stmts {
		n, m, rng, ok := findIdentInStmt(stmt, lines, line, col)
		if ok {
			return n, m, rng, true
		}
	}
	return "", "", [4]int{}, false
}

// findIdentInStmt searches a statement (and its sub-expressions) for identifiers.
func findIdentInStmt(stmt ast.Stmt, lines []string, line, col int) (name, module string, r [4]int, ok bool) {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return findIdentInExpr(s.X, lines, line, col)
	case *ast.LetStmt:
		for _, v := range s.Values {
			n, m, rng, ok := findIdentInExpr(v, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.AssignStmt:
		for _, t := range s.Targets {
			n, m, rng, ok := findIdentInExpr(t, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
		for _, v := range s.Values {
			n, m, rng, ok := findIdentInExpr(v, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.ReturnStmt:
		for _, v := range s.Values {
			n, m, rng, ok := findIdentInExpr(v, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.IfStmt:
		n, m, rng, ok := findIdentInExpr(s.Cond, lines, line, col)
		if ok {
			return n, m, rng, true
		}
		if s.Then != nil {
			n, m, rng, ok = findIdentInBlock(s.Then, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
		if s.Else != nil {
			n, m, rng, ok = findIdentInStmt(s.Else, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.ForStmt:
		if s.Cond != nil {
			n, m, rng, ok := findIdentInExpr(s.Cond, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
		if s.Iter != nil {
			n, m, rng, ok := findIdentInExpr(s.Iter, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
		if s.Body != nil {
			n, m, rng, ok := findIdentInBlock(s.Body, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.SpawnStmt:
		return findIdentInExpr(s.Call, lines, line, col)
	case *ast.SendStmt:
		n, m, rng, ok := findIdentInExpr(s.Chan, lines, line, col)
		if ok {
			return n, m, rng, true
		}
		return findIdentInExpr(s.Val, lines, line, col)
	case *ast.Block:
		return findIdentInBlock(s, lines, line, col)
	}
	return "", "", [4]int{}, false
}

// findIdentInExpr searches an expression tree for an ident at (line, col).
func findIdentInExpr(expr ast.Expr, lines []string, line, col int) (name, module string, r [4]int, ok bool) {
	if expr == nil {
		return "", "", [4]int{}, false
	}
	switch e := expr.(type) {
	case *ast.Ident:
		if posOverlaps(e.Pos.Line, e.Pos.Column, e.Name, line, col) {
			return e.Name, "", identRange(e.Pos.Line, e.Pos.Column, e.Name), true
		}
	case *ast.SelectorExpr:
		// Check if cursor is on the .Name part of x.Name.
		// The selector name starts after the dot; we approximate its position
		// from the source line.
		if e.Pos.Line == line {
			selName := e.Name
			srcLine := ""
			if e.Pos.Line-1 < len(lines) {
				srcLine = lines[e.Pos.Line-1]
			}
			// Find ".<Name>" in the source line near the expression position.
			dotIdx := strings.Index(srcLine, "."+selName)
			if dotIdx >= 0 {
				nameStart := dotIdx + 1 // 0-based in the source line
				nameEnd := nameStart + len(selName)
				// col is 1-based AST column.
				if col >= nameStart+1 && col <= nameEnd {
					// Resolve the module from the X side.
					modName := ""
					if ident, ok := e.X.(*ast.Ident); ok {
						modName = ident.Name
					}
					rng := [4]int{
						e.Pos.Line - 1,     // 0-based line
						nameStart,           // 0-based char
						e.Pos.Line - 1,
						nameEnd,
					}
					return selName, modName, rng, true
				}
			}
		}
		// Also check if cursor is on the X part (the module/receiver).
		return findIdentInExpr(e.X, lines, line, col)
	case *ast.CallExpr:
		n, m, rng, ok := findIdentInExpr(e.Fn, lines, line, col)
		if ok {
			return n, m, rng, true
		}
		for _, arg := range e.Args {
			n, m, rng, ok = findIdentInExpr(arg, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.BinaryExpr:
		n, m, rng, ok := findIdentInExpr(e.Left, lines, line, col)
		if ok {
			return n, m, rng, true
		}
		return findIdentInExpr(e.Right, lines, line, col)
	case *ast.UnaryExpr:
		return findIdentInExpr(e.Operand, lines, line, col)
	case *ast.IndexExpr:
		n, m, rng, ok := findIdentInExpr(e.X, lines, line, col)
		if ok {
			return n, m, rng, true
		}
		return findIdentInExpr(e.Index, lines, line, col)
	case *ast.StructLit:
		for _, f := range e.Fields {
			n, m, rng, ok := findIdentInExpr(f.Value, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
		for _, p := range e.Positional {
			n, m, rng, ok := findIdentInExpr(p, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.SliceLit:
		for _, el := range e.Elems {
			n, m, rng, ok := findIdentInExpr(el, lines, line, col)
			if ok {
				return n, m, rng, true
			}
		}
	case *ast.InterpLit:
		for _, part := range e.Parts {
			if part.Expr != nil {
				n, m, rng, ok := findIdentInExpr(part.Expr, lines, line, col)
				if ok {
					return n, m, rng, true
				}
			}
		}
	}
	return "", "", [4]int{}, false
}

// posOverlaps checks whether cursor position (line, col) falls within the
// identifier name starting at (posLine, posCol). All values are 1-based.
func posOverlaps(posLine, posCol int, name string, cursorLine, cursorCol int) bool {
	if posLine != cursorLine {
		return false
	}
	// The identifier spans [posCol, posCol+len(name)).
	return cursorCol >= posCol && cursorCol < posCol+len(name)
}

// identRange builds a 0-based range from 1-based line/col and a name length.
func identRange(line, col int, name string) [4]int {
	return [4]int{line - 1, col - 1, line - 1, col - 1 + len(name)}
}

// resolveIdent searches the file's declarations for a matching name and builds
// a SymbolInfo from the declaration's signature and doc-comments.
func resolveIdent(pr compiler.ParseResult, name string) *SymbolInfo {
	for _, d := range pr.File.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Name == name {
				sig := formatFuncSig(d)
				doc := extractDocComment(pr.Lines, d.Pos.Line)
				return &SymbolInfo{Signature: sig, Doc: doc}
			}
		case *ast.StructDecl:
			if d.Name == name {
				sig := formatStructSig(d)
				doc := extractDocComment(pr.Lines, d.Pos.Line)
				return &SymbolInfo{Signature: sig, Doc: doc}
			}
		case *ast.VarDecl:
			for _, vn := range d.Names {
				if vn == name {
					sig := formatVarSig(d, vn)
					doc := extractDocComment(pr.Lines, d.Pos.Line)
					return &SymbolInfo{Signature: sig, Doc: doc}
				}
			}
		}
	}

	// Check if it is an import alias.
	for _, imp := range pr.File.Imports {
		alias := imp.Alias
		if alias == "" {
			// Default alias is the last segment of the path.
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		if alias == name {
			sig := fmt.Sprintf("import %s", strings.ReplaceAll(imp.Path, "/", "."))
			if imp.Alias != "" {
				sig += " as " + imp.Alias
			}
			return &SymbolInfo{Signature: sig}
		}
	}

	// Builtins.
	if bi := builtinInfo(name); bi != nil {
		return bi
	}

	return nil
}

// resolveSelector resolves a qualified access like module.name by looking up
// the import alias to determine what module is referenced. For stdlib modules
// we provide known signatures; for local .nv imports we could parse the target
// file (not yet implemented).
func resolveSelector(pr compiler.ParseResult, module, name string) *SymbolInfo {
	// Resolve import path from alias.
	importPath := ""
	for _, imp := range pr.File.Imports {
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, "/")
			alias = parts[len(parts)-1]
		}
		if alias == module {
			importPath = imp.Path
			break
		}
	}

	if importPath == "" {
		return nil
	}

	// Check known stdlib signatures.
	if sig := stdlibSignature(importPath, name); sig != nil {
		return sig
	}

	// Fallback: generic module.name info.
	return &SymbolInfo{
		Signature: fmt.Sprintf("(%s) %s.%s", strings.ReplaceAll(importPath, "/", "."), module, name),
	}
}

// builtinInfo returns hover info for Novel builtin functions.
func builtinInfo(name string) *SymbolInfo {
	switch name {
	case "spawn":
		return &SymbolInfo{
			Signature: "fn spawn(f fn())",
			Doc:       "Spawns a new green thread running f concurrently.",
		}
	case "send":
		return &SymbolInfo{
			Signature: "fn send(ch chan<T>, val T)",
			Doc:       "Sends val into the channel. Blocks if the channel is full.",
		}
	case "recv":
		return &SymbolInfo{
			Signature: "fn recv(ch chan<T>) T",
			Doc:       "Receives a value from the channel. Blocks if the channel is empty.",
		}
	case "chan":
		return &SymbolInfo{
			Signature: "fn chan<T>(cap? int) chan<T>",
			Doc:       "Creates a new channel. Optional cap sets the buffer size (default 0, unbuffered).",
		}
	case "error":
		return &SymbolInfo{
			Signature: "fn error(msg string) error",
			Doc:       "Constructs an error value with the given message.",
		}
	case "len":
		return &SymbolInfo{
			Signature: "fn len(x []T | string | map[K]V) int",
			Doc:       "Returns the length of a slice, string, or map.",
		}
	case "append":
		return &SymbolInfo{
			Signature: "fn append(xs []T, val T)",
			Doc:       "Appends val to slice xs in place.",
		}
	case "println":
		return &SymbolInfo{
			Signature: "fn println(args ...any)",
			Doc:       "Prints arguments to stdout followed by a newline.",
		}
	}
	return nil
}

// stdlibSignature returns known signatures for stdlib module functions.
// This is a static registry; a full implementation would parse the Lua source.
func stdlibSignature(importPath, name string) *SymbolInfo {
	key := importPath + "." + name
	sigs := map[string]*SymbolInfo{
		// std/io
		"std/io.println": {
			Signature: "fn io.println(args ...any)",
			Doc:       "Prints arguments to stdout followed by a newline.",
		},
		"std/io.print": {
			Signature: "fn io.print(args ...any)",
			Doc:       "Prints arguments to stdout without a trailing newline.",
		},
		"std/io.readLine": {
			Signature: "fn io.readLine() string",
			Doc:       "Reads one line from stdin (blocks until Enter).",
		},
		"std/io.readAll": {
			Signature: "fn io.readAll() string",
			Doc:       "Reads all of stdin until EOF.",
		},
		"std/io.open": {
			Signature: "fn io.open(path string, mode? string) (any, error)",
			Doc:       "Opens a file. Mode defaults to \"r\". Returns a file handle or error.",
		},
		// std/str
		"std/str.split": {
			Signature: "fn str.split(s string, sep string) []string",
			Doc:       "Splits s by separator sep and returns the parts.",
		},
		"std/str.join": {
			Signature: "fn str.join(parts []string, sep string) string",
			Doc:       "Joins the string slice with separator.",
		},
		"std/str.contains": {
			Signature: "fn str.contains(s string, sub string) bool",
			Doc:       "Reports whether s contains the substring sub.",
		},
		"std/str.trim": {
			Signature: "fn str.trim(s string) string",
			Doc:       "Trims leading and trailing whitespace.",
		},
		// std/math
		"std/math.abs": {
			Signature: "fn math.abs(x float) float",
			Doc:       "Returns the absolute value of x.",
		},
		"std/math.sqrt": {
			Signature: "fn math.sqrt(x float) float",
			Doc:       "Returns the square root of x.",
		},
		"std/math.floor": {
			Signature: "fn math.floor(x float) int",
			Doc:       "Returns the largest integer <= x.",
		},
		"std/math.ceil": {
			Signature: "fn math.ceil(x float) int",
			Doc:       "Returns the smallest integer >= x.",
		},
		// std/time
		"std/time.sleep": {
			Signature: "fn time.sleep(seconds float)",
			Doc:       "Suspends the current green thread for the given duration. Cooperative; other threads run while sleeping.",
		},
		"std/time.now": {
			Signature: "fn time.now() float",
			Doc:       "Returns the current time as a Unix timestamp (seconds since epoch).",
		},
		// std/os
		"std/os.args": {
			Signature: "fn os.args() []string",
			Doc:       "Returns the command-line arguments passed after the .nv file.",
		},
		"std/os.exit": {
			Signature: "fn os.exit(code? int)",
			Doc:       "Exits the process with the given exit code (default 0).",
		},
		"std/os.getenv": {
			Signature: "fn os.getenv(key string) string",
			Doc:       "Returns the value of the environment variable key.",
		},
		// std/json
		"std/json.encode": {
			Signature: "fn json.encode(value any) string",
			Doc:       "Serializes a Novel value to a JSON string.",
		},
		"std/json.decode": {
			Signature: "fn json.decode(s string) any",
			Doc:       "Parses a JSON string into a Novel value.",
		},
		// std/rand
		"std/rand.int": {
			Signature: "fn rand.int(min int, max int) int",
			Doc:       "Returns a random integer in [min, max].",
		},
		"std/rand.float": {
			Signature: "fn rand.float() float",
			Doc:       "Returns a random float in [0.0, 1.0).",
		},
		// std/wl_ui
		"std/wl_ui.createWindow": {
			Signature: "fn wl_ui.createWindow(title string, width int, height int) any",
			Doc:       "Opens a Wayland window with the given title and dimensions. Returns nil on failure.",
		},
		"std/wl_ui.clear": {
			Signature: "fn wl_ui.clear(win any, color int)",
			Doc:       "Fills the window buffer with the given 24-bit RGB color.",
		},
		"std/wl_ui.drawText": {
			Signature: "fn wl_ui.drawText(win any, x int, y int, text string, color int)",
			Doc:       "Draws text at (x,y) using the built-in 8x8 bitmap font.",
		},
		"std/wl_ui.present": {
			Signature: "fn wl_ui.present(win any)",
			Doc:       "Commits the pixel buffer to the Wayland compositor (makes drawing visible).",
		},
		"std/wl_ui.dispatch": {
			Signature: "fn wl_ui.dispatch(win any) int",
			Doc:       "Dispatches pending Wayland events. Returns -1 when the window is closed.",
		},
		"std/wl_ui.close": {
			Signature: "fn wl_ui.close(win any)",
			Doc:       "Destroys the window and releases Wayland resources.",
		},
		// std/net
		"std/net.listen": {
			Signature: "fn net.listen(addr string) (any, error)",
			Doc:       "Listens for TCP connections on addr (e.g. \":8080\"). Async over the reactor.",
		},
		"std/net.dial": {
			Signature: "fn net.dial(addr string) (any, error)",
			Doc:       "Opens a TCP connection to addr. Async over the reactor.",
		},
		// std/http
		"std/http.listenAndServe": {
			Signature: "fn http.listenAndServe(addr string, handler fn(req any, res any))",
			Doc:       "Starts an HTTP server on addr with the given request handler. Blocks.",
		},
		"std/http.get": {
			Signature: "fn http.get(url string) (any, error)",
			Doc:       "Performs an HTTP GET request and returns the response.",
		},
	}

	if info, ok := sigs[key]; ok {
		return info
	}
	return nil
}

// ---- Formatting helpers ----

// formatFuncSig produces a human-readable Novel function signature from a FuncDecl.
func formatFuncSig(d *ast.FuncDecl) string {
	var b strings.Builder
	if d.Public {
		b.WriteString("pub ")
	}
	b.WriteString("fn ")
	if d.Receiver != nil {
		b.WriteString("(")
		b.WriteString(d.Receiver.Name)
		b.WriteString(" ")
		b.WriteString(formatType(d.Receiver.Type))
		b.WriteString(") ")
	}
	b.WriteString(d.Name)
	b.WriteString("(")
	b.WriteString(formatParams(d.Params))
	b.WriteString(")")
	if len(d.Results) > 0 {
		b.WriteString(" ")
		b.WriteString(formatResults(d.Results))
	}
	return b.String()
}

// formatStructSig produces a human-readable struct signature.
func formatStructSig(d *ast.StructDecl) string {
	var b strings.Builder
	if d.Public {
		b.WriteString("pub ")
	}
	b.WriteString("type ")
	b.WriteString(d.Name)
	b.WriteString(" struct {\n")
	for _, f := range d.Fields {
		b.WriteString("    ")
		if f.Public {
			b.WriteString("pub ")
		}
		b.WriteString(f.Name)
		b.WriteString(" ")
		b.WriteString(formatType(f.Type))
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

// formatVarSig produces a human-readable variable declaration signature.
func formatVarSig(d *ast.VarDecl, name string) string {
	var b strings.Builder
	if d.Public {
		b.WriteString("pub ")
	}
	if d.Const {
		b.WriteString("const ")
	} else {
		b.WriteString("let ")
	}
	b.WriteString(name)
	if d.Type.Name != "" {
		b.WriteString(" ")
		b.WriteString(formatType(d.Type))
	}
	return b.String()
}

// formatType renders an ast.Type to its Novel source form.
func formatType(t ast.Type) string {
	if t.Slice {
		if t.Elem != nil {
			return "[]" + formatType(*t.Elem)
		}
		return "[]any"
	}
	if t.ArrLen > 0 {
		if t.Elem != nil {
			return fmt.Sprintf("[%d]%s", t.ArrLen, formatType(*t.Elem))
		}
		return fmt.Sprintf("[%d]any", t.ArrLen)
	}
	if t.Chan {
		if t.Elem != nil {
			return "chan<" + formatType(*t.Elem) + ">"
		}
		return "chan<any>"
	}
	if t.MapKey != nil {
		elem := "any"
		if t.Elem != nil {
			elem = formatType(*t.Elem)
		}
		return "map[" + formatType(*t.MapKey) + "]" + elem
	}
	if t.Name == "" {
		return "any"
	}
	return t.Name
}

// formatParams renders a parameter list to its Novel source form.
func formatParams(params []ast.Param) string {
	parts := make([]string, len(params))
	for i, p := range params {
		s := p.Name
		if p.Type.Name != "" || p.Type.Slice || p.Type.Chan || p.Type.MapKey != nil {
			s += " " + formatType(p.Type)
		}
		if p.Variadic {
			s += "..."
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}

// formatResults renders the return type list.
func formatResults(types []ast.Type) string {
	if len(types) == 1 {
		return formatType(types[0])
	}
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = formatType(t)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// extractDocComment pulls consecutive single-line comments (`//`) directly
// above the declaration at declLine (1-based). Strips the `// ` prefix.
func extractDocComment(lines []string, declLine int) string {
	if declLine <= 1 || declLine-1 >= len(lines) {
		return ""
	}

	// Walk backwards from the line above the declaration.
	var commentLines []string
	for i := declLine - 2; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "//") {
			// Strip the "//" prefix and optional leading space.
			text := strings.TrimPrefix(trimmed, "//")
			text = strings.TrimPrefix(text, " ")
			commentLines = append([]string{text}, commentLines...)
		} else {
			break
		}
	}

	return strings.Join(commentLines, "\n")
}
