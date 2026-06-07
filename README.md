# Novel

Novel is a statically typed language that transpiles to Lua 5.1 and runs on LuaJIT.
Novel borrows Go's type discipline and explicit error handling, adds keyword-rich
syntax, and provides lightweight concurrency via green threads on LuaJIT coroutines.

The language design lives in [`docs/language-spec.md`](./docs/language-spec.md).

## Status

Early bootstrap, but the full pipeline runs real programs end to end:
`bin/novel run examples/hello.nv` compiles to Lua and prints. The lexer, parser,
emitter, and runtime shim are working; the type checker is still a no-op walk
(the next milestone).

## Repository layout

```
cmd/novel/          the `novel` CLI (build / run / repl / version)
cmd/novel-lsp/      the language server (LSP over stdio), stub
internal/token/     token definitions
internal/lexer/     source -> tokens (working)
internal/ast/       AST node definitions
internal/parser/    tokens -> AST (decls, statements, Pratt expressions)
internal/types/     compile-time type checker (no-op walk for now)
internal/emit/      AST -> Lua 5.1 source (working)
internal/compiler/  pipeline driver shared by CLI, REPL, and LSP
runtime/novel.lua   runtime shim: cooperative scheduler + channels
editors/vscode/     VS Code extension (grammar + LSP client)
examples/           sample .nv programs
docs/               language and toolchain documentation
```

## Prerequisites

- Go 1.26+ (transpiler and language server)
- LuaJIT 2.1+ (runtime; `novel run` and `make test-runtime` shell out to `luajit`)
- Node 20+ / npm (only to build the VS Code extension)

## Quick start

```sh
make build          # builds bin/novel and bin/novel-lsp
make test           # Go tests + Lua runtime smoke test

bin/novel build examples/hello.nv   # transpile to examples/hello.lua
bin/novel run   examples/hello.nv   # transpile + execute via luajit
bin/novel repl                      # interactive prompt
```

Run `make help` for all targets.

## How it fits together

`internal/compiler.Compile` runs the full pipeline (lex -> parse -> type-check ->
emit) and is the single source of truth used by the CLI, the REPL, and the
language server, so diagnostics stay consistent everywhere. Emitted Lua does
`require("novel")` to pull in the runtime shim, which lowers `spawn`/`chan`/
`send`/`recv` onto LuaJIT coroutines (see `docs/language-spec.md` §13.2).

## Quick syntax reference

- **No package declarations**: a file is runnable when it declares `fn main()`.
- **Dotted imports**: `import std.io`, `import geometry` (no quotes, no `./` prefix).
- **`ret` keyword**: `ret a + b` (not `return`).
- **Error handling**: `(T, error)` returns; `!` suffix propagates errors.
- **Concurrency**: `spawn`, `chan<T>()`, `send(ch, v)`, `recv(ch)`.

We don't try to replace Lua, we just want to elevate the developer experience. By bringing static typing and explicit error handling to prevent runtime errors, Novel turns Lua from a simple scripting tool into a robust language for building full-scale applications.
