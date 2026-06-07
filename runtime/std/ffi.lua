-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/ffi (direct C interop for Novel, over LuaJIT's FFI)
--
-- Purpose:
--   Lets Novel call C libraries directly. Thin pass-through to LuaJIT's `ffi`
--   (cdef/load/new/cast/sizeof/string/C) plus flat helpers that work around
--   Novel's call-lowering rule. Imported with `import std.ffi as ffi`; calls
--   lower to dot-calls (ffi.cdef(...), ffi.load(...)).
--
-- Key Components:
--   - cdef(decls): declare C types/functions for the FFI
--   - load(name, global?): load a shared library, returns its namespace
--   - C: the default C namespace (libc + whatever cdef declared)
--   - fn(lib, symbol): return a PLAIN callable for a C function (see note)
--   - new/cast/sizeof/typeof/string/cstr: the common ffi value helpers
--   - os(): the build OS ("Linux"/"OSX"/"Windows"/...) for cdef branching
--
-- Note on calling C from Novel:
--   Novel lowers `m.foo(x)` where `m` is a local value to a method call
--   `m:foo(x)`, passing m as an implicit self. A raw ffi library handle is not
--   an object, so a colon-call would pass the wrong first argument. Use
--   `ffi.fn(lib, "foo")` to get a plain function value and call it directly:
--
--     import std.ffi as ffi
--     ffi.cdef("double cos(double x);")
--     let cos = ffi.fn(ffi.C, "cos")   // or ffi.fn("m", "cos") to load libm
--     let y = cos(3.14159)
--
--   ffi.fn accepts either a loaded namespace or a library name (which it loads).

local ffi = require("ffi")

local M = {}

-- Pass-through to the underlying ffi. cdef declares C signatures/types; load
-- opens a shared library and returns its symbol namespace.
function M.cdef(decls) return ffi.cdef(decls) end
function M.load(name, global) return ffi.load(name, global) end

-- The default C namespace: libc plus anything cdef has declared.
M.C = ffi.C

-- Value helpers, forwarded verbatim.
function M.new(ct, ...) return ffi.new(ct, ...) end
function M.cast(ct, v) return ffi.cast(ct, v) end
function M.sizeof(ct, ...) return ffi.sizeof(ct, ...) end
function M.typeof(ct) return ffi.typeof(ct) end
function M.alignof(ct) return ffi.alignof(ct) end
function M.offsetof(ct, field) return ffi.offsetof(ct, field) end

-- string converts a C pointer (and optional length) to a Lua string. cstr is an
-- alias reading a NUL-terminated C string.
function M.string(ptr, len)
  if len ~= nil then return ffi.string(ptr, len) end
  return ffi.string(ptr)
end
M.cstr = M.string

-- errno reads/sets the C errno (useful after a failed C call).
function M.errno(newval)
  if newval ~= nil then return ffi.errno(newval) end
  return ffi.errno()
end

-- os returns the FFI target OS ("Linux", "OSX", "Windows", "BSD", ...) so
-- callers can branch their cdef declarations. arch returns "x64"/"arm64"/...
function M.os() return ffi.os end
function M.arch() return ffi.arch end

-- fn returns a plain, directly-callable function value for a C symbol, so Novel
-- code can invoke it without the colon-call self problem (see the module note).
-- `lib` is either a loaded namespace (e.g. ffi.C or the result of ffi.load) or a
-- library name to load. Raises if the symbol is missing.
function M.fn(lib, symbol)
  local ns = lib
  if type(lib) == "string" then
    ns = ffi.load(lib)
  end
  local f = ns[symbol]
  if f == nil then
    error("std/ffi: symbol not found: " .. tostring(symbol), 2)
  end
  -- Wrap so the returned value is a Lua function (not a cdata callable bound to
  -- a namespace), which Novel calls cleanly as a free function.
  return function(...) return f(...) end
end

return M
