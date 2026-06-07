-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/test (a tiny unit-test runner for Novel programs)
--
-- Purpose:
--   Lets Novel programs test themselves: register named cases, make assertions,
--   and exit non-zero when any fail (so CI can gate on `novel run tests.nv`).
--   Pure Lua. Imported with `import std.test`; calls lower to dot-calls
--   (test.run("name", fn), test.eq(a, b)).
--
-- Key Components:
--   - run(name, fn): run fn as a test case; a failed assertion marks it failed
--   - assert(cond, msg?): fail the current case if cond is falsy
--   - eq(a, b, msg?): fail unless a == b (shows both values)
--   - neq(a, b, msg?): fail unless a ~= b
--   - summary(): print pass/fail counts; returns the failure count
--   - done(): print the summary and exit (0 all-pass, 1 otherwise)
--
-- Usage:
--   import std.test
--   fn main() {
--       test.run("math", checkMath)
--       test.done()
--   }
--   fn checkMath() {
--       test.eq(2 + 2, 4)
--   }

local test = {}

local passed = 0
local failed = 0
local currentFailed = false

-- A failed assertion in the current case sets a flag rather than throwing, so a
-- case keeps checking subsequent assertions and reports them all.
local function fail(msg)
  currentFailed = true
  io.write("    FAIL: " .. tostring(msg) .. "\n")
end

-- repr renders a value for assertion messages (quotes strings).
local function repr(v)
  if type(v) == "string" then return string.format("%q", v) end
  return tostring(v)
end

-- run executes one named test case. The case is a function taking no arguments;
-- it uses test.assert/eq/neq to check conditions. A case with no failed
-- assertion passes.
function test.run(name, fn)
  currentFailed = false
  local ok, err = pcall(fn)
  if not ok then
    -- An unexpected Lua error (not an assertion) fails the case too.
    fail("error: " .. tostring(err))
  end
  if currentFailed then
    failed = failed + 1
    io.write("FAIL - " .. name .. "\n")
  else
    passed = passed + 1
    io.write("ok   - " .. name .. "\n")
  end
end

-- assert fails the current case if cond is falsy (nil or false).
function test.assert(cond, msg)
  if not cond then
    fail(msg or "assertion failed")
    return false
  end
  return true
end

-- eq fails unless a == b, showing both values on failure.
function test.eq(a, b, msg)
  if a ~= b then
    fail((msg and (msg .. ": ") or "") ..
         "expected " .. repr(b) .. ", got " .. repr(a))
    return false
  end
  return true
end

-- neq fails unless a ~= b.
function test.neq(a, b, msg)
  if a == b then
    fail((msg and (msg .. ": ") or "") ..
         "expected values to differ, both were " .. repr(a))
    return false
  end
  return true
end

-- summary prints the pass/fail tally and returns the number of failures.
function test.summary()
  io.write(string.format("\n%d passed, %d failed\n", passed, failed))
  return failed
end

-- done prints the summary and exits the process: status 0 if every case
-- passed, 1 otherwise. Call it at the end of a test program.
function test.done()
  local n = test.summary()
  os.exit(n == 0 and 0 or 1)
end

return test
