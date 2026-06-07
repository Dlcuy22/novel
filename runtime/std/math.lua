-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/math (numeric utilities for Novel)
--
-- Purpose:
--   A Novel-facing wrapper over LuaJIT's math library plus a few helpers, so
--   .nv code can write math.sqrt(x), math.max(a, b), etc. Imported with
--   `import "std/math"`; calls lower to dot-calls (math.sqrt(x)).
--
-- Key Components:
--   - constants: pi, e
--   - roots/powers: sqrt, pow
--   - rounding: floor, ceil, round, abs
--   - trig: sin, cos, tan, atan2
--   - comparison: min, max, clamp
--   - logs: exp, log

local m = {}

m.pi = math.pi
m.e = math.exp(1)

function m.sqrt(x) return math.sqrt(x) end
function m.pow(x, y) return x ^ y end
function m.abs(x) return math.abs(x) end
function m.floor(x) return math.floor(x) end
function m.ceil(x) return math.ceil(x) end

-- round returns x rounded to the nearest integer, halves rounding up.
function m.round(x)
  return math.floor(x + 0.5)
end

function m.sin(x) return math.sin(x) end
function m.cos(x) return math.cos(x) end
function m.tan(x) return math.tan(x) end
function m.atan2(y, x) return math.atan2(y, x) end

function m.exp(x) return math.exp(x) end
function m.log(x) return math.log(x) end

function m.min(a, b) if a < b then return a else return b end end
function m.max(a, b) if a > b then return a else return b end end

-- clamp constrains v to the inclusive range [lo, hi].
function m.clamp(v, lo, hi)
  if v < lo then return lo end
  if v > hi then return hi end
  return v
end

return m
