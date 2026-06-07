# Novel Language Specification
**Version:** 0.2.8  
**Extension:** `.nv`  
**Toolchain:** Novel -> Lua 5.1 -> LuaJIT bytecode  
**Paradigm:** Statically typed, procedural + concurrent, REPL-friendly

---

## 1. Overview

Novel is a statically typed language that transpiles to Lua for execution on LuaJIT. It borrows Go's type discipline and explicit error handling, adopts keyword-rich syntax for readability, and provides lightweight concurrency via green threads built on LuaJIT coroutines.

---

## 2. Lexical Elements

### 2.1 Comments
```nv
// single-line comment
/* multi-line
   comment */
```

### 2.2 Keywords
```
fn       let      const    type     struct
if       elif     else     for      break
continue ret      spawn    chan     send
recv     import   nil      true
false    in       as       pub
```

### 2.3 Operators
```
+   -   *   /   %       // arithmetic
==  !=  <   >   <=  >=  // comparison
&&  ||  !               // logical
&   |   ^   <<  >>      // bitwise
=   :=  +=  -=  *=  /=  // assignment
<-                      // channel send (alternate sugar for send())
->                      // return type arrow (fn signatures)
```

### 2.4 Literals
```nv
42          // int
42u         // uint
42i64       // int64
3.14        // float64
3.14f32     // float32
"hello"     // string (UTF-8)
`raw\nstring`  // raw string, no escape processing
true  false
nil
```

### 2.5 String Interpolation
```nv
let name = "world"
let msg = $"hello {name}"   // -> "hello world"
let expr = $"result: {1 + 2}"
```

---

## 3. Type System

### 3.1 Primitive Types
| Type      | Description              | Size    |
|-----------|--------------------------|---------|
| `int`     | signed, platform default | 64-bit  |
| `int8`    | signed                   | 8-bit   |
| `int16`   | signed                   | 16-bit  |
| `int32`   | signed                   | 32-bit  |
| `int64`   | signed                   | 64-bit  |
| `uint`    | unsigned, default        | 64-bit  |
| `uint8`   | unsigned                 | 8-bit   |
| `uint16`  | unsigned                 | 16-bit  |
| `uint32`  | unsigned                 | 32-bit  |
| `uint64`  | unsigned                 | 64-bit  |
| `float32` | IEEE 754 single          | 32-bit  |
| `float64` | IEEE 754 double          | 64-bit  |
| `bool`    | true / false             | 1-bit   |
| `string`  | UTF-8 immutable          | dynamic |
| `byte`    | alias for `uint8`        | 8-bit   |
| `rune`    | alias for `int32` (codepoint) | 32-bit |
| `any`     | dynamic escape hatch (unchecked) | dynamic |

`any` opts a value out of static checking: it is accepted in any type position
(parameters, results, annotations, composite elements) and the checker tracks
nothing about its shape. It exists for values whose type cannot be expressed
statically — most commonly a table returned from a Lua stdlib module (an HTTP
request, a socket connection, a decoded JSON value). Like every Novel type, it
is erased in the emitted Lua; it carries no runtime cost and no runtime check.

```nv
fn handle(req any) any {     // req is an opaque table from std/http
    ret http.response(200, $"path: {req.path}")
}
```

### 3.2 Composite Types
```nv
[]int               // slice (dynamic array)
[4]int              // fixed array, length 4
map[string]int      // hash map
chan<int>            // channel of int
(int, error)        // tuple, used for multi-return only
```

### 3.3 Type Inference
```nv
let x = 5           // infers int
let y = 3.14        // infers float64
let s = "hi"        // infers string
let b = true        // infers bool
let a = [1, 2, 3]   // infers []int
```

### 3.4 Explicit Typing
```nv
let x int = 5
let y float32 = 3.14
let n uint32 = 100
let big int64 = 9999999999
```

### 3.5 Constants
```nv
const MAX int = 100
const PI = 3.14159  // inferred float64
```

### 3.6 Error Type
`error` is a built-in interface with a single field `.msg string`.  
Construct with `error("message")` or `nil` to signal no error.

```nv
// error is defined as:
type error struct {
    msg string
}
```

---

## 4. Variables

```nv
let x = 10              // mutable, inferred
let x int = 10          // mutable, explicit
const Y = 20            // immutable

// multiple assignment (from multi-return)
let val, err = someFunc()
```

Variables are block-scoped. Shadowing is allowed within inner blocks.  
All variables must be used, unused variables are compile-time errors.

---

## 5. Functions

### 5.1 Declaration
```nv
fn add(a int, b int) int {
    ret a + b
}

// no return value
fn greet(name string) {
    // ...
}

// multiple return values (primarily for error pattern)
fn divide(a float64, b float64) (float64, error) {
    if b == 0.0 {
        ret 0.0, error("division by zero")
    }
    ret a / b, nil
}
```

### 5.2 Calling
```nv
let sum = add(1, 2)
let result, err = divide(10.0, 3.0)
```

### 5.3 First-Class Functions
```nv
let double fn(int) int = fn(x int) int {
    ret x * 2
}

// anonymous function (immediately invoked)
let val = fn(x int) int { ret x + 1 }(5)
```

### 5.4 Variadic Functions
```nv
fn sum(nums ...int) int {
    let total = 0
    for n in nums {
        total += n
    }
    ret total
}
```

### 5.5 Methods on Structs
```nv
type Circle struct {
    radius float64
}

fn (c Circle) area() float64 {
    ret 3.14159 * c.radius * c.radius
}

// pointer receiver (mutates)
fn (c *Circle) scale(factor float64) {
    c.radius *= factor
}
```

---

## 6. Structs

```nv
type Point struct {
    x float64
    y float64
}

// instantiation
let p = Point{ x: 1.0, y: 2.0 }
let p2 = Point{ 1.0, 2.0 }     // positional (same order as declaration)

// field access
let xval = p.x
```

### 6.1 Embedded Structs
```nv
type Named struct {
    name string
}

type Player struct {
    Named               // embed, fields promoted
    score int
}

let pl = Player{ Named: Named{ name: "Alice" }, score: 0 }
let n = pl.name         // promoted field access
```

### 6.2 Public vs Private
Fields and functions are private by default. Use `pub` to export.

```nv
type Server struct {
    pub host string
    pub port uint16
    tls  bool           // private
}

pub fn newServer(host string, port uint16) Server {
    ret Server{ host: host, port: port, tls: false }
}
```

---

## 7. Control Flow

### 7.1 If / Elif / Else
```nv
if x > 0 {
    // positive
} elif x < 0 {
    // negative
} else {
    // zero
}
```

### 7.2 For Loops
```nv
// c-style
for i = 0; i < 10; i++ {
    // ...
}

// range over slice
for item in items {
    // ...
}

// range with index
for i, item in items {
    // ...
}

// range over map
for key, val in mymap {
    // ...
}

// infinite loop
for {
    break
}

// while-style
for condition {
    // ...
}
```

### 7.3 Match (Pattern Matching)
```nv
match x {
    1       => // single value
    2, 3    => // multiple values
    4..10   => // range
    _       => // default / wildcard
}

// match with binding
match err {
    nil     => // no error
    e       => // bind to e, handle error
}
```

### 7.4 Break / Continue with Labels
```nv
outer: for i = 0; i < 5; i++ {
    for j = 0; j < 5; j++ {
        if j == 2 {
            break outer
        }
    }
}
```

---

## 8. Error Handling

Novel uses explicit error returns, identical to Go's pattern.  
There is no exception mechanism.

```nv
fn readFile(path string) (string, error) {
    let f, err = io.open(path)
    if err != nil {
        ret "", err
    }
    let content, err = f.readAll()
    if err != nil {
        ret "", error($"read failed: {err.msg}")
    }
    ret content, nil
}

// call site
let text, err = readFile("config.nv")
if err != nil {
    // handle
}
```

### 8.1 Error Propagation Shorthand
`!` suffix on a call propagates the error to the caller automatically (like Rust's `?` operator).

```nv
fn process(path string) (string, error) {
    let content = readFile(path)!   // returns early with error if err != nil
    ret transform(content), nil
}
```

The caller must have a matching `(T, error)` return signature, otherwise it is a compile-time error.

---

## 9. Slices and Maps

### 9.1 Slices
```nv
let nums = [1, 2, 3, 4]        // []int literal
let empty []int = []

nums.append(5)                  // mutates in place
let length = nums.len()
let sub = nums[1:3]             // slice -> [2, 3]
let copy = nums[:]
```

### 9.2 Maps
```nv
let scores map[string]int = {}
scores["alice"] = 100

let val, ok = scores["bob"]     // ok is bool
if !ok {
    // key not present
}

// map literal
let m = map[string]int{
    "a": 1,
    "b": 2,
}
```

---

## 10. Concurrency

Novel's concurrency model is built on LuaJIT coroutines, exposed as green threads.  
The model is deliberately simplified: `spawn`, `chan`, `send`, `recv`.

### 10.1 Spawning a Green Thread
```nv
spawn fn() {
    // runs concurrently
}()

// or a named function
fn worker(id int) {
    // ...
}

spawn worker(1)
```

### 10.2 Channels
```nv
// unbuffered channel
let ch = chan<int>()

// buffered channel, capacity 16
let bch = chan<string>(16)
```

### 10.3 Send and Receive
```nv
// send (blocks if unbuffered and no receiver)
send(ch, 42)
ch <- 42        // sugar for send(ch, 42)

// receive (blocks until value is available)
let val = recv(ch)
let val int = recv(ch)   // with explicit type
```

### 10.4 Channel Close and Done Signal
```nv
close(ch)

// recv on closed channel returns zero value + false ok flag
let val, ok = recv(ch)
if !ok {
    // channel closed
}
```

### 10.5 Fan-out Pattern Example
```nv
fn main() {
    let ch = chan<int>(4)

    for i = 0; i < 4; i++ {
        spawn fn(id int) {
            send(ch, id * 2)
        }(i)
    }

    for i = 0; i < 4; i++ {
        let result = recv(ch)
        // use result
    }
}
```

---

## 11. Modules and Packages

### 11.1 Entry Point
A runnable program is simply a file that declares `fn main()`. There is no package declaration.

```nv
fn main() {
    // program starts here
}
```

### 11.2 Imports
Imports use dotted paths. A path either names another Novel source file
(a local module) or a Lua module reachable on the runtime path.

```nv
// standard library / Lua modules
import std.io
import std.math

// alias
import std.math as m

// local module: another .nv file, relative to this one
import geometry
import sub.mod
```

An import binds to a name derived from the path's final segment (`std.math` ->
`math`, `geometry` -> `geometry`). Use an alias to choose a different name. Exported members are reached with dot access:

```nv
import geometry

let a = geometry.circleArea(2.0)
```

### 11.3 Import Resolution
An import path is resolved in this order:

1. A **bare path** is resolved against, in order:
    a. a local `.nv` file (relative to the importing file, then the entry
       directory);
    b. a dependency declared in `novel.toml` (a `path` dependency resolves to a
       local file; a version dependency resolves to a global store module);
    c. a global Novel module in the store, `$NVLPATH/modules/novel/<path>.nv`.
    A match found by any of these is bundled into the program (§11.7).
2. If still unresolved, the path is treated as a **Lua module** and lowered to a
   runtime `require`, resolved on the Lua module path. This covers the standard
   library, global Lua modules (`$NVLPATH/modules/lua`), and LuaJIT libraries
   such as `bit`. A bare import that resolves to nothing is left for the runtime
   to handle.

### 11.4 Exports and Visibility
Top-level declarations are private to their file by default. Prefix a function,
struct, or variable with `pub` to export it; only exported members are visible
to importers.

```nv
const pi = 3.14159              // private

pub fn circleArea(r float64) float64 {
    ret pi * r * r
}

fn helper(w float64) float64 {  // private, callable only within this file
    ret w * w
}
```

Exporting a struct also exports its methods.

### 11.5 The Global Store (NVLPATH)
The environment variable `NVLPATH` points at a global store: a self-contained,
distributable tree holding the core runtime, global modules, and a built-in Lua
module path. With a populated store, a Novel install runs without any external
Lua setup.

```
$NVLPATH/
  config.toml             store defaults (active runtime version, luajit pin)
  runtime/<version>/      core runtime: novel.lua, repl.lua
  modules/novel/          global .nv modules  (bundled at compile time)
  modules/lua/            global .lua modules  (resolved at runtime)
```

- **runtime/<version>/** holds the core runtime, versioned so multiple runtimes
  coexist. The active version comes from `novel.toml` (§11.6), else the store's
  `config.toml`, else the toolchain's built-in version. A source checkout with no
  store falls back to its `./runtime` directory.
- **modules/novel/** holds global Novel modules; importing one bundles it like a
  local module.
- **modules/lua/** holds global Lua modules, resolved at runtime via the
  built-in `LUA_PATH` the toolchain assembles from the store.
- **LuaJIT pinning**: `config.toml` (or `novel.toml`) may record a required
  LuaJIT version; the toolchain checks the available `luajit -v` against it and
  warns on a mismatch. The pin is advisory in this version, not enforced.

`novel env` prints the resolved store, runtime, LuaJIT, and the importable
global modules. The language server exposes the same information over the custom
`novel/environment` request so editors can show it.

### 11.6 Project Manifest (novel.toml)
A project may include an optional `novel.toml` at its root. It is discovered by
walking up from the file being compiled. A project without one still builds.

```toml
[package]
name = "myapp"
version = "0.1.0"

[runtime]
version = "0.1.0-alpha"   # core runtime version to use
luajit  = "2.1"           # required LuaJIT version

[deps]
json  = "1.0.0"                       # a global store module (modules/novel/json)
utils = { path = "./vendor/utils" }   # a local module file or directory
http  = { version = "2.0" }           # explicit version form
```

Dependencies are Lua-compatible: a `path` dependency names a local module; a
version dependency names a global store module. Declared dependency names take
part in import resolution (§11.3). No network fetching is performed in this
version; dependencies must already be present locally or in the store.

### 11.7 Bundling
At build time the compiler walks the import graph from the entry file and
bundles every reachable Novel module — local files, global store modules, and
`novel.toml` dependencies — into a single Lua program. Each module is registered
once (shared dependencies are not duplicated) and evaluated lazily on first
import. Global modules use a `@`-prefixed bundle key so they never collide with
local module names. There is no separate link or install step, and the produced
Lua runs as-is on LuaJIT.

> Cross-module types: a struct type exported by another module is constructed
> with the qualified form `mod.Type{ ... }` (named or positional). The literal's
> metatable is the imported module's exported struct table, so its methods apply
> normally.

---

## 12. REPL Behavior

The REPL (`novel repl`) accepts any single import, declaration, statement, or
expression. There is no `fn main` required.

```
novel> 1 + 1
2
novel> let x = 10
novel> x * 2
20
novel> fn double(n int) int { ret n * 2 }
novel> double(7)
14
novel> import std.math
novel> math.sqrt(144.0)
12
```

### 12.1 Semantics
- A bare expression prints its value. Strings are shown quoted; an `error`
  value prints as `error("...")`. A statement or a void call prints nothing.
- Variables, functions, structs, and imports persist across lines: the session
  keeps state the way Python's REPL does.
- Multi-line input is read until brackets balance, so a struct or a function
  body can span several lines (the prompt changes to `...>`).
- A runtime error (for example, sending on a closed channel) is reported and the
  session continues; it does not abort the REPL.

### 12.2 Commands
Lines beginning with `.` are REPL commands rather than Novel code:

| Command | Effect |
|---------|--------|
| `.exit`, `.quit`, `.q` | leave the REPL (Ctrl-D also works) |
| `.reset` | clear all session state (variables, functions, imports) |
| `.help`, `.h` | show available commands |

### 12.3 Implementation
Each line is transpiled to a Lua chunk and streamed to a persistent LuaJIT
process running the REPL driver (`runtime/repl.lua`). The driver executes every
chunk in one shared environment, so session globals accumulate, and drives the
chunk through the green-thread scheduler so top-level `recv`/`send` can block.
Because input arrives a line at a time, the whole-program type checks
(§13.3)—unused variables, arity over a closed program—do not apply; the REPL
surfaces parse errors and runtime errors instead.

---

## 13. Transpilation Model

```
.nv source
    |
    v
Novel Parser  (produces AST)
    |
    v
Type Checker  (compile-time errors, type annotations on AST)
    |
    v
Lua Emitter   (AST -> valid Lua 5.1 source)
    |
    v
LuaJIT        (luajit -b -> bytecode, or direct execution)
```

### 13.1 Type Mapping to Lua
| Novel      | Lua representation                      |
|------------|-----------------------------------------|
| `int*`     | `number` (LuaJIT 64-bit integer)        |
| `uint*`    | `number` (LuaJIT 64-bit unsigned)       |
| `float64`  | `number` (Lua default)                  |
| `float32`  | `number` (truncated precision)          |
| `bool`     | `boolean`                               |
| `string`   | `string`                                |
| `[]T`      | `table` (array-indexed)                 |
| `map[K]V`  | `table` (hash)                          |
| `struct`   | `table` (with metatable for methods)    |
| `chan<T>`  | coroutine + table queue                 |
| `error`    | `table { msg = "..." }` or `nil`        |
| `nil`      | `nil`                                   |

### 13.2 Green Thread to Coroutine Mapping
`spawn` wraps the function in a `coroutine.wrap` call, scheduled by a cooperative scheduler injected by the Novel runtime shim. Channels are implemented as shared tables with coroutine yield/resume for blocking semantics.

### 13.3 Compile-time Guarantees
These checks run in the type-checker stage before emission; the emitted Lua is
untyped, so they are the only place these errors are caught. Emission is skipped
when any check fails.

**Enforced today (hard errors):**
- Undefined identifiers (use of an unbound name)
- Unused variables (a `let`/`const` local that is never read)
- Assignment to a `const`
- Wrong argument count in a call to a declared function (variadic-aware)
- Unknown type names (in annotations, struct literals, and composite types)
- Use of `!` propagation in a function that does not return `(..., error)`

**Planned (not yet enforced):**
- Type mismatches. All numeric types currently lower to LuaJIT numbers and mix
  freely; full type compatibility checking needs a complete type lattice and is
  deferred. A genuine mismatch surfaces as a Lua runtime error for now.
- Missing error checks (warning, not a hard error; opt-in `strict` mode).

---

## 14. Standard Library

Implemented modules (see `docs/stdlib.md` for full APIs):

| Package        | Contents                                                |
|----------------|---------------------------------------------------------|
| `std/io`       | stdout/stderr, async stdin reads, whole-file read/write |
| `std/str`      | string manipulation: split, join, trim, replace         |
| `std/math`     | numeric utilities, trigonometry                         |
| `std/os`       | platform/arch detection, env vars, args, exit           |
| `std/time`     | CPU/wall/monotonic clocks, cooperative `sleep`          |
| `std/rand`     | PRNG plus secure bytes                                   |
| `std/crypto`   | SHA-256, FNV-1a                                          |
| `std/json`     | encode/decode JSON                                       |
| `std/encoding` | base64 and hex codecs                                    |
| `std/ffi`      | direct C interop over LuaJIT's FFI                       |
| `std/net`      | async TCP/UDP sockets (POSIX), integrated with `spawn`   |
| `std/http`     | minimal HTTP/1.1 server and client over `std/net`        |
| `std/image`    | PNG/JPEG load/edit/save via stb (needs `make image-lib`) |
| `std/test`     | unit test runner                                         |

`std/net` and `std/http` do non-blocking I/O on the cooperative scheduler's
`poll(2)` reactor, so one connection never stalls the others (POSIX only for
now). `std/image` requires a native helper library built with `make image-lib`.

Still planned:

| Package    | Contents                                   |
|------------|--------------------------------------------|
| `std/chan` | channel utilities (merge, fan-out helpers) |
| TLS        | encrypted transport for `std/net`/`std/http` |
| async DNS  | non-blocking hostname resolution           |
| Windows    | Winsock2 backend for `std/net`             |

---

## 15. Example Program

```nv
import std.io

type Task struct {
    pub id   int
    pub name string
}

fn runTask(t Task, results chan<string>) {
    // simulate work
    send(results, $"task {t.id}: {t.name} done")
}

fn main() {
    let tasks = []Task{
        Task{ id: 1, name: "parse" },
        Task{ id: 2, name: "compile" },
        Task{ id: 3, name: "link" },
    }

    let results = chan<string>(tasks.len())

    for _, t in tasks {
        spawn runTask(t, results)
    }

    for i = 0; i < tasks.len(); i++ {
        let msg = recv(results)
        io.println(msg)
    }
}
```

---

*Novel v0.2.8 spec, subject to revision.*
