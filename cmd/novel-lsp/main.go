// Copyright (c) 2026. Asrian Putra. All rights reserved.
// Use of this source code is governed by an MIT license
// that can be found in the LICENSE file.

// Command novel-lsp is the Novel language server.
//
// Purpose:
//
//	Speaks the Language Server Protocol over stdio and reuses
//	internal/compiler so editor diagnostics match the CLI exactly. On open and
//	on every edit it compiles the document and publishes diagnostics.
//
// Key Components:
//   - main(): the stdio JSON-RPC read/dispatch loop
//   - readMessage()/writeMessage(): LSP Content-Length framing
//   - publishDiagnostics(): runs compiler.Compile and emits the results
//
// Dependencies:
//   - internal/compiler: the source -> Lua pipeline and its diagnostics
//
// Note:
//
//	Implements the minimum protocol surface for diagnostics: initialize,
//	initialized, textDocument/didOpen, didChange, didClose, shutdown, exit.
//	Full document sync (the whole text is sent on every change).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dlcuy22/novel/internal/compiler"
	"github.com/dlcuy22/novel/internal/types"
)

var version = types.NovelVersion

// server holds the open documents keyed by URI so edits can be recompiled.
type server struct {
	out  *bufio.Writer
	docs map[string]string
	// root is the workspace root directory (from initialize), used to resolve
	// the Novel store and any novel.toml for the novel/environment request.
	root string
}

func main() {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" || a == "version" {
			fmt.Println("novel-lsp", version)
			return
		}
	}

	s := &server{
		out:  bufio.NewWriter(os.Stdout),
		docs: map[string]string{},
	}
	r := bufio.NewReader(os.Stdin)

	for {
		method, id, params, err := readMessage(r)
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "novel-lsp: read error:", err)
			return
		}
		if s.dispatch(method, id, params) {
			return // exit requested
		}
	}
}

// dispatch handles one message and reports whether the server should exit.
func (s *server) dispatch(method string, id json.RawMessage, params json.RawMessage) bool {
	switch method {
	case "initialize":
		s.root = initializeRoot(params)
		s.reply(id, map[string]any{
			"capabilities": map[string]any{
				// 1 = full document sync: the client sends the whole text on
				// every change, which keeps the server stateless and simple.
				"textDocumentSync": 1,
				"hoverProvider":    true,
			},
			"serverInfo": map[string]any{"name": "novel-lsp", "version": version},
		})
	case "initialized":
		// Notification, nothing to do.
	case "novel/environment":
		// Custom request: report the resolved store, runtime, LuaJIT, and the
		// importable global modules so the extension can show the environment.
		s.reply(id, compiler.DescribeEnvironment(s.workdir()))
	case "textDocument/didOpen":
		uri, text := openParams(params)
		if uri != "" {
			s.docs[uri] = text
			s.publishDiagnostics(uri, text)
		}
	case "textDocument/didChange":
		uri, text, ok := changeParams(params)
		if ok {
			s.docs[uri] = text
			s.publishDiagnostics(uri, text)
		}
	case "textDocument/hover":
		s.handleHover(id, params)
	case "textDocument/didClose":
		if uri := uriParam(params); uri != "" {
			delete(s.docs, uri)
			// Clear diagnostics for the closed file.
			s.publish(uri, []map[string]any{})
		}
	case "shutdown":
		s.reply(id, nil)
	case "exit":
		return true
	}
	return false
}

// publishDiagnostics compiles text and sends the resulting diagnostics, or an
// empty list to clear stale ones when the document is clean.
func (s *server) publishDiagnostics(uri, text string) {
	res := compiler.Compile(text)
	diags := make([]map[string]any, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		// Compiler positions are 1-based; LSP positions are 0-based.
		line := d.Line - 1
		if line < 0 {
			line = 0
		}
		char := d.Column - 1
		if char < 0 {
			char = 0
		}
		diags = append(diags, map[string]any{
			"range": map[string]any{
				"start": map[string]any{"line": line, "character": char},
				"end":   map[string]any{"line": line, "character": char + 1},
			},
			"severity": 1, // Error
			"source":   "novel/" + d.Stage,
			"message":  d.Msg,
		})
	}
	s.publish(uri, diags)
}

func (s *server) publish(uri string, diags []map[string]any) {
	s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": diags,
	})
}

// reply sends a JSON-RPC response carrying result for the given request id.
func (s *server) reply(id json.RawMessage, result any) {
	if id == nil {
		return
	}
	writeMessage(s.out, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (s *server) notify(method string, params any) {
	writeMessage(s.out, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

// readMessage reads one LSP-framed JSON-RPC message and returns its method, id
// (nil for notifications), and raw params.
func readMessage(r *bufio.Reader) (method string, id json.RawMessage, params json.RawMessage, err error) {
	contentLen := -1
	for {
		var line string
		line, err = r.ReadString('\n')
		if err != nil {
			return "", nil, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // end of headers
		}
		if cl, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			contentLen, err = strconv.Atoi(strings.TrimSpace(cl))
			if err != nil {
				return "", nil, nil, err
			}
		}
	}
	if contentLen < 0 {
		return "", nil, nil, fmt.Errorf("missing Content-Length header")
	}

	body := make([]byte, contentLen)
	if _, err = io.ReadFull(r, body); err != nil {
		return "", nil, nil, err
	}

	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err = json.Unmarshal(body, &msg); err != nil {
		return "", nil, nil, err
	}
	return msg.Method, msg.ID, msg.Params, nil
}

// writeMessage serializes payload and writes it with LSP Content-Length framing.
func writeMessage(w *bufio.Writer, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "novel-lsp: marshal error:", err)
		return
	}
	fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
	w.Write(data)
	w.Flush()
}

// ---- params decoding ----

// workdir returns the directory the store and novel.toml are resolved against:
// the workspace root from initialize, or the process working directory.
func (s *server) workdir() string {
	if s.root != "" {
		return s.root
	}
	wd, _ := os.Getwd()
	return wd
}

// initializeRoot extracts the workspace root directory from initialize params,
// preferring rootUri, then the first workspace folder, then rootPath.
func initializeRoot(params json.RawMessage) string {
	var p struct {
		RootURI          string `json:"rootUri"`
		RootPath         string `json:"rootPath"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
	}
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	if p.RootURI != "" {
		return uriToPath(p.RootURI)
	}
	if len(p.WorkspaceFolders) > 0 && p.WorkspaceFolders[0].URI != "" {
		return uriToPath(p.WorkspaceFolders[0].URI)
	}
	return p.RootPath
}

// uriToPath converts a file:// URI to a local filesystem path.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}

func openParams(params json.RawMessage) (uri, text string) {
	var p struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) != nil {
		return "", ""
	}
	return p.TextDocument.URI, p.TextDocument.Text
}

func changeParams(params json.RawMessage) (uri, text string, ok bool) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if json.Unmarshal(params, &p) != nil || len(p.ContentChanges) == 0 {
		return "", "", false
	}
	// Full sync: the last change holds the entire new document text.
	return p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text, true
}

// handleHover resolves the symbol under the cursor and replies with a
// Markdown tooltip. If no symbol is found, replies with null (no hover).
func (s *server) handleHover(id, params json.RawMessage) {
	uri, line, character := hoverParams(params)
	text, ok := s.docs[uri]
	if !ok {
		s.reply(id, nil)
		return
	}

	info := findHoverSymbol(text, line, character)
	if info == nil {
		s.reply(id, nil)
		return
	}

	// Build Markdown content: signature in a novel code fence, then doc text.
	var content strings.Builder
	content.WriteString("```novel\n")
	content.WriteString(info.Signature)
	content.WriteString("\n```")
	if info.Doc != "" {
		content.WriteString("\n\n---\n\n")
		content.WriteString(info.Doc)
	}

	s.reply(id, map[string]any{
		"contents": map[string]any{
			"kind":  "markdown",
			"value": content.String(),
		},
		"range": map[string]any{
			"start": map[string]any{"line": info.StartLine, "character": info.StartChar},
			"end":   map[string]any{"line": info.EndLine, "character": info.EndChar},
		},
	})
}

func hoverParams(params json.RawMessage) (uri string, line, character int) {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"position"`
	}
	if json.Unmarshal(params, &p) != nil {
		return "", 0, 0
	}
	return p.TextDocument.URI, p.Position.Line, p.Position.Character
}

func uriParam(params json.RawMessage) string {
	var p struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	return p.TextDocument.URI
}
