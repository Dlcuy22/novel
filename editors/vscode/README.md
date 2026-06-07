# Novel VS Code Extension

Syntax highlighting and language-server support for Novel (`.nv`) files.

## What you get

- **Syntax highlighting**: keywords, types, strings, interpolation, numbers,
  comments. Purely declarative (a TextMate grammar), so it works as soon as the
  extension loads, with no build or server.
- **Snippets**: tab-completed scaffolds for the common constructs (`program`,
  `fn`, `fnerr`, `struct`, `method`, the four `for` forms, `iferr`, channel
  send/recv, `map`, and more). Also declarative, no server needed. Type a prefix
  and pick it from the completion list. See [Snippets](#snippets).
- **Editor smarts**: auto-indent after `{`/`(`/`[`, block-comment continuation
  (`/* ... */` and `/** ... */`), bracket matching, and `// region` /
  `// endregion` folding markers.
- **Live diagnostics**: parse and type errors reported as you type, served by
  the `novel-lsp` language server, which reuses the same compiler the CLI uses,
  so editor errors match `novel build` exactly.

## Layout

```
package.json                      extension manifest (language, grammar, snippets, settings)
language-configuration.json       comments, brackets, auto-closing, indent & folding rules
syntaxes/novel.tmLanguage.json    TextMate grammar (highlighting)
snippets/novel.json               completion snippets for common constructs
src/extension.ts                  LSP client; launches novel-lsp over stdio
out/extension.js                  compiled client (generated, gitignored)
```

## Build

The language server is part of the Go toolchain. Build both binaries from the
repository root:

```sh
make build          # produces bin/novel and bin/novel-lsp
```

Then build the extension client:

```sh
cd editors/vscode
npm install         # once (or: make ext-install from the repo root)
npm run compile     # produces out/extension.js (or: make ext-build)
```

## Run it during development

The fastest loop is the Extension Development Host:

1. Open the `editors/vscode` folder in VS Code.
2. Press F5 (Run > Start Debugging). A second VS Code window opens with the
   extension loaded.
3. In that window, open this repository and any `.nv` file, for example
   `examples/basics.nv`.

You should see syntax highlighting immediately. Introduce an error (write
`let =`, or call an undefined function) and a red squiggle with the compiler
message appears once you stop typing.

## Snippets

Snippets are declarative completions defined in `snippets/novel.json`; they need
no build and no server. In a `.nv` file, start typing a prefix and accept the
completion (Tab or Enter), then Tab through the placeholders.

| Prefix | Expands to |
|--------|------------|
| `program` | full `import std.io` + `fn main` skeleton |
| `main` | `fn main() { }` |
| `fn` / `fnv` | function with / without a return type |
| `fnerr` | fallible function returning `(T, error)` with the guard |
| `method` | method with a receiver |
| `struct` | struct type declaration |
| `let` / `lett` / `const` | bindings (inferred, typed, immutable) |
| `letm` | multi-return destructuring |
| `if` / `ifelse` / `elif` | conditionals |
| `iferr` | the explicit `if err != nil` check |
| `for` / `forin` / `forix` / `forw` / `forever` | the four loop forms |
| `chan` / `spawn` / `send` / `recv` / `recvok` | concurrency |
| `map` / `slice` / `interp` / `println` | literals and output |

The set deliberately covers only features implemented in this build; constructs
the Language Guide marks unsupported (`match`, the `!` suffix beyond parsing,
anonymous and variadic functions) are intentionally left out.

## How the server is found

`src/extension.ts` resolves the `novel-lsp` binary in this order:

1. the `novel.server.path` setting, if set;
2. `bin/novel-lsp` under any open workspace folder (the default dev layout);
3. the bare name `novel-lsp` on your `PATH`.

So if you ran `make build` and opened the repo, the client finds
`bin/novel-lsp` with no extra configuration. To use a server installed
elsewhere, set in your settings:

```json
{ "novel.server.path": "/absolute/path/to/novel-lsp" }
```

## Install permanently (optional)

To use the extension outside the dev host, package it into a `.vsix`:

```sh
cd editors/vscode
npx vsce package        # produces novel-<version>.vsix
code --install-extension novel-0.2.8.vsix
```

Make sure `novel-lsp` is reachable: either keep working inside the repo (so
`bin/novel-lsp` resolves) or copy it onto your `PATH`, for example
`cp bin/novel-lsp ~/.local/bin/`.

## What the server does today

`novel-lsp` implements the minimum protocol for diagnostics: `initialize`,
`textDocument/didOpen`, `didChange`, `didClose`, `shutdown`, and `exit`, with
full document sync. On open and on every edit it runs the compiler and
publishes diagnostics. Completion, hover, go-to-definition, and formatting are
not implemented yet.

Lexer, parser, and type-checker errors are all reported, since the server
surfaces every compiler diagnostic. The type checker flags undefined
identifiers, unused variables, assignment to a `const`, wrong argument counts,
unknown type names, and misuse of the `!` suffix. Numeric type mismatches are
not yet checked (see the Language Guide).
