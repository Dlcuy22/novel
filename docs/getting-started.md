# Getting Started

Novel is a statically typed language that transpiles to Lua 5.1 and runs on
LuaJIT. It borrows Go's type discipline and explicit error handling, adds
keyword-rich syntax, and provides lightweight concurrency with green threads.

This guide gets you from a clean checkout to a running program.

## Prerequisites

- Go 1.26 or newer (builds the transpiler and language server)
- LuaJIT 2.1 or newer (runs the generated Lua; `novel run` shells out to it)
- Node 20 and npm (only needed to build the VS Code extension)

Check what you have:

```sh
go version
luajit -v
```

## Build the toolchain

From the repository root:

```sh
make build
```

This produces two binaries in `bin/`:

- `bin/novel`: the CLI (build, run, repl)
- `bin/novel-lsp`: the language server (diagnostics over LSP)

Run the test suite to confirm everything works:

```sh
make test
```

## Your first program

A runnable Novel file declares `fn main()`. Create `hello.nv`:

```nv
import std.io

fn main() {
    io.println("hello, Novel")
}
```

Run it:

```sh
bin/novel run hello.nv
```

You should see `hello, Novel`.

## Build versus run

`novel run` transpiles to Lua and executes it in one step. `novel build` writes
the Lua next to your source so you can inspect it:

```sh
bin/novel build hello.nv   # writes hello.lua
```

Looking at the generated Lua is the fastest way to understand how a Novel
construct lowers. Every file requires the `novel` runtime shim, which provides
the scheduler and channels.

## The REPL

`novel repl` starts an interactive prompt. Unlike a source file, it needs no
`fn main`: type one import, declaration, statement, or expression at a
time. A bare expression prints its value, and definitions persist across lines.

```sh
bin/novel repl
```

```
novel> 1 + 1
2
novel> let x = 10
novel> x * 2
20
novel> fn double(n int) int { ret n * 2 }
novel> double(x)
20
novel> import std.math
novel> math.sqrt(144.0)
12
```

Input that leaves brackets open continues on the next line (the prompt becomes
`...>`), so a struct or function body can span several lines. Lines starting
with `.` are commands: `.help`, `.reset` (clear all session state), and `.exit`
(Ctrl-D also quits). A runtime error is reported without ending the session.

## How it runs

The pipeline is: your `.nv` source goes through the lexer, parser, type checker,
and Lua emitter, then LuaJIT executes the result.

```
source.nv  ->  lexer  ->  parser  ->  type checker  ->  Lua emitter  ->  luajit
```

`main` is launched as a green thread and the scheduler drains all spawned work
before the program exits, so concurrent code "just works" without extra setup.
See [concurrency.md](concurrency.md) for the details.

## Try the examples

The `examples/` directory has small, runnable programs. Each one names the
command to run it in a comment at the top.

```sh
bin/novel run examples/basics.nv
bin/novel run examples/control_flow.nv
bin/novel run examples/collections.nv
bin/novel run examples/structs.nv
bin/novel run examples/errors.nv
bin/novel run examples/concurrency.nv
bin/novel run examples/channels.nv
bin/novel run examples/worker_pool.nv
bin/novel run examples/fizzbuzz.nv
bin/novel run examples/game_of_life.nv
```

`game_of_life.nv` is a small terminal simulation that ties together slices,
structs of logic, nested loops, and string building.

## Modules and the global store

A program can span several `.nv` files. Import a sibling file with a dotted path
(no `./` prefix, no quotes):

```nv
import geometry
```

For modules you want available to every project, use the global store. Point the
`NVLPATH` environment variable at a directory laid out like this:

```
$NVLPATH/
  runtime/<version>/   core runtime (novel.lua, repl.lua)
  modules/novel/       global .nv modules, imported by bare name and bundled
  modules/lua/         global .lua modules, resolved at runtime
```

With `NVLPATH` set, `import json` finds `$NVLPATH/modules/novel/json.nv` and
bundles it; `import somelib` finds `$NVLPATH/modules/lua/somelib.lua` and
loads it at runtime. The store also provides the `LUA_PATH` the toolchain runs
LuaJIT with, so a packaged store needs no extra Lua setup. A local file always
wins over a global module of the same name.

Run `novel env` to see exactly what is resolved:

```sh
bin/novel env
```

It prints the store root, the active runtime directory, the LuaJIT version, and
the global modules available for import.

A project can pin versions and declare dependencies in an optional `novel.toml`
at its root:

```toml
[package]
name = "myapp"

[runtime]
luajit = "2.1"          # warn if the available LuaJIT differs

[deps]
json  = "1.0.0"                     # a global store module
utils = { path = "./vendor/utils" } # a local module
```

The manifest is optional; a project without one builds the same way.

## Where to go next

- [Language Guide](language-guide.md): the full syntax, types, functions,
  structs, control flow, collections, and error handling.
- [Concurrency](concurrency.md): green threads, channels, and common patterns.

## Current status

Novel runs real programs end to end: lexer, parser, type checker, emitter, and
runtime are all working, and `.nv` files can import each other (see the
[Language Guide](language-guide.md#modules)). The type checker reports undefined
identifiers, unused variables, const reassignment, arity errors, unknown types,
and `!` misuse, though it does not yet check numeric type compatibility. A few
documented features are still unimplemented; the Language Guide flags them inline
under "Not yet supported".
