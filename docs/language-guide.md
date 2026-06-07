# Language Guide

This guide covers Novel's syntax and semantics as currently implemented. Where
the language spec describes a feature that is not built yet, it is marked
**Not yet supported** so you know to avoid it.

All examples here are runnable. The companion programs live in `examples/`.

## Contents

- [Program structure](#program-structure)
- [Comments](#comments)
- [Variables and constants](#variables-and-constants)
- [Types](#types)
- [String interpolation](#string-interpolation)
- [Functions](#functions)
- [Control flow](#control-flow)
- [Collections](#collections)
- [Structs and methods](#structs-and-methods)
- [Error handling](#error-handling)
- [Concurrency](#concurrency)
- [Modules](#modules)
- [Known limitations](#known-limitations)

## Program structure

A runnable program declares `fn main()`. Imports come before other declarations:

```nv
import std.io

fn main() {
    io.println("ready")
}
```

Identifiers are private to their file by default. Prefix a declaration with
`pub` to export it.

## Comments

```nv
// line comment

/* block
   comment */
```

## Variables and constants

`let` declares a mutable binding; `const` declares an immutable one. There is no
`var` keyword.

```nv
let x = 10        // mutable, type inferred as int
const limit = 100 // immutable
```

When you write the type explicitly, it follows the name with **no colon**:

```nv
let count int = 0
let ratio float64 = 0.5
let name string = "novel"
```

Variables are block-scoped. A `let` inside a block shadows an outer binding of
the same name for the rest of that block.

## Types

Primitive types:

| Category | Types |
|----------|-------|
| Signed integers | `int`, `int8`, `int16`, `int32`, `int64` |
| Unsigned integers | `uint`, `uint8`, `uint16`, `uint32`, `uint64` |
| Floats | `float32`, `float64` |
| Other | `bool`, `string`, `byte` (alias `uint8`), `rune` (alias `int32`) |

Composite types:

```nv
[]int            // slice (dynamic array)
map[string]int   // map
chan<int>        // channel of int
```

All numeric types lower to LuaJIT numbers at runtime, so arithmetic mixes
freely today. The type checker that will enforce the distinctions is not built
yet.

## String interpolation

A string prefixed with `$` interpolates `{...}` expressions:

```nv
let name = "world"
let n = 3
io.println($"hello {name}")
io.println($"{n} squared is {n * n}")
```

Build up strings by re-interpolating, since `+=` on strings is not supported
yet (see [Known limitations](#known-limitations)):

```nv
let line = ""
for v in [1, 0, 1] {
    if v == 1 {
        line = $"{line}#"
    } else {
        line = $"{line}."
    }
}
```

Raw strings use backticks and do no escape processing:

```nv
let path = `C:\novel\bin`
```

**Not yet supported:** a double-quoted string literal nested inside an
interpolation, for example `$"value {m["key"]}"`. Pull the value into a local
first:

```nv
let v = m["key"]
io.println($"value {v}")
```

## Functions

Parameters list their type after the name. The return type follows the
parameter list with no arrow:

```nv
fn add(a int, b int) int {
    ret a + b
}
```

A function with no return type omits it:

```nv
fn greet(name string) {
    io.println($"hi {name}")
}
```

Multiple return values use a parenthesized tuple. This is the basis of the
error pattern (see [Error handling](#error-handling)):

```nv
fn divmod(a int, b int) (int, int) {
    ret a / b, a % b
}

let q, r = divmod(17, 5)
```

**Not yet supported:** anonymous function expressions and variadic parameters.
Use named functions, including with `spawn`.

## Control flow

### If / elif / else

Use `elif`, not `else if`. Conditions are not parenthesized:

```nv
if n > 0 {
    io.println("positive")
} elif n < 0 {
    io.println("negative")
} else {
    io.println("zero")
}
```

### For loops

There is one loop keyword, `for`, in four shapes.

Classic C-style. The loop variable is scoped to the loop, never leaked:

```nv
for i = 0; i < 5; i++ {
    io.println($"{i}")
}
```

While-style, a single condition:

```nv
for n > 1 {
    n = n / 2
}
```

Infinite, exited with `break`:

```nv
for {
    let v = recv(ch)
    if v < 0 {
        break
    }
}
```

Range over a collection. With two variables you get index and value; with one
you get the value:

```nv
for i, v in items {
    io.println($"{i}: {v}")
}

for v in items {
    io.println($"{v}")
}
```

> Note: collection indices are 1-based in this build, matching the underlying
> Lua tables. The spec calls for 0-based indexing; that change is pending.

### Break and continue

`break` exits the nearest loop; `continue` skips to the next iteration. Both
work in every loop form:

```nv
for i = 0; i < 10; i++ {
    if i % 2 == 0 {
        continue
    }
    if i > 7 {
        break
    }
    io.println($"odd {i}")
}
```

**Not yet supported:** loop labels (`break outer`) and `match` expressions.

## Collections

### Slices

A slice is a dynamic array. Create one with a literal, grow it with `append`,
and read its length with `len`:

```nv
let nums = [4, 8, 15]
nums.append(16)
io.println($"count {nums.len()}")
let first = nums[1]   // 1-based index
```

Typed empty slices use the `[]T{}` form. This is how you build nested slices:

```nv
let grid = [][]int{}
let row = []int{}
row.append(0)
grid.append(row)
```

### Maps

A map associates keys with values. Use `map[K]V{}` for an empty map or with
entries, and index to read or write:

```nv
let ages = map[string]int{
    "alice": 30,
    "bob": 25,
}
ages["carol"] = 28

let a = ages["alice"]
```

**Not yet supported:** the `val, ok := m[key]` presence check and deletion.

## Structs and methods

A struct groups named fields. Fields are private unless marked `pub`:

```nv
type Rect struct {
    pub width  float64
    pub height float64
}
```

Construct with named fields in any order, or positionally in declaration order:

```nv
let a = Rect{ width: 3.0, height: 4.0 }
let b = Rect{ 2.0, 5.0 }
```

Methods attach to a type through a receiver written before the function name.
The receiver is available inside the body like any parameter:

```nv
fn (r Rect) area() float64 {
    ret r.width * r.height
}

let area = a.area()
```

Compose structs by holding one as a named field and reaching through it:

```nv
type Labeled struct {
    label string
    rect  Rect
}

let tile = Labeled{ label: "floor", rect: Rect{ width: 10.0, height: 2.0 } }
let area = tile.rect.area()
```

**Not yet supported:** embedded structs with promoted fields (writing the type
name with no field name and accessing its fields directly). Use a named field
and access through it as shown above.

## Error handling

Novel has no exceptions. A function that can fail returns `(T, error)`.
Construct an error with `error("message")`, or return `nil` for success. An
error value carries a `.msg` field:

```nv
fn safeDivide(a int, b int) (int, error) {
    if b == 0 {
        ret 0, error("division by zero")
    }
    ret a / b, nil
}

let q, err = safeDivide(10, 0)
if err != nil {
    io.println($"failed: {err.msg}")
    ret
}
io.println($"result {q}")
```

### Error propagation with `!`

A `!` suffix on a call propagates the error to the caller automatically, like
Rust's `?`. It returns early with the error when the call's last return value is
non-nil, otherwise yields the call's first value:

```nv
fn process(ok bool) (int, error) {
    let v = parse(ok)!     // returns (0, err) early if parse failed
    ret v * 2, nil
}
```

The enclosing function must return `(..., error)`; using `!` elsewhere is a
compile-time error. The early return zero-fills any non-error results, so a
function returning `(int, string, error)` returns `nil, nil, err`.

## Concurrency

Concurrency is covered in depth in [concurrency.md](concurrency.md). In brief:

```nv
let ch = chan<int>(4)   // buffered channel
spawn worker(ch)        // run a named function as a green thread
send(ch, 42)            // or: ch <- 42
let v = recv(ch)        // blocks until a value is available
```

## Modules

A program can span multiple `.nv` files. Import another file with a dotted path
(no `./` prefix, no quotes):

```nv
import std.io
import geometry

fn main() {
    io.println($"{geometry.circleArea(2.0)}")
}
```

The imported file marks the declarations it wants to expose with `pub`. Everything else stays private to that file:

```nv
const pi = 3.14159265358979   // private

pub fn circleArea(radius float64) float64 {
    ret pi * radius * radius
}
```

The module binds to a name derived from the path (here `geometry`), and its
exported functions are reached with dot-calls: `geometry.circleArea(...)`. Use
an alias if you want a different name:

```nv
import g geometry

let a = g.circleArea(2.0)
```

Imports resolve in this order: a bare path is tried relative to the importing
file and then the entry file's directory. A bare import that doesn't match a local `.nv` file is treated as a Lua module
and lowered to a runtime `require`. That is how the standard library and LuaJIT
libraries are pulled in:

```nv
import std.math   // runtime/std/math.lua
import bit        // LuaJIT's built-in bit library
```

At build time the compiler bundles every reachable `.nv` file into a single Lua
program (each module registered with `package.preload`), so there is nothing to
install or link separately. See `examples/modules/` for a runnable program.

You can construct a struct type exported by another module with the
module-qualified form `mod.Type{ ... }`, in both named and positional shapes:

```nv
import shapes

let a = shapes.Point{ x: 1.0, y: 2.0 }   // named
let b = shapes.Point{ 3.0, 4.0 }          // positional, declaration order
let s = a.sum()                            // exported methods work too
```

## Known limitations

These features appear in the language spec or examples but are not implemented
in this build. They are listed in one place so you can plan around them:

- **Limited compile-time checking.** The type checker reports undefined
  identifiers, unused variables (a hard error per the spec), assignment to a
  `const`, wrong argument counts for declared functions, unknown type names, and
  misuse of the `!` suffix. It does **not** yet check numeric/type compatibility
  (all numbers mix freely), so a genuine type mismatch still surfaces as a Lua
  runtime error.
- **0-based indexing.** Collection indices are currently 1-based (Lua tables).
- **`match`** expressions are not parsed.
- **Anonymous functions** and **variadic parameters** are not supported.
- **Embedded struct field promotion** is not supported.
- **String `+=`** lowers to numeric addition; build strings with interpolation.
- **Nested double-quoted strings inside interpolation** do not parse.
- **Map `val, ok` presence checks**, **loop labels**, and **slice ranges**
  (`xs[1:3]`) are not supported yet.

For the authoritative language design, including planned features, see
[`language-spec.md`](./language-spec.md).
