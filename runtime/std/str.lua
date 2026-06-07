-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/str (string operations for Novel)
--
-- Purpose:
--   String utilities beyond what interpolation provides, so .nv code can write
--   str.split(s, ","), str.trim(s), etc. Imported with `import "std/str"`;
--   calls lower to dot-calls. Indices returned are 1-based to match Novel's
--   slice convention in this build.
--
-- Key Components:
--   - len, upper, lower, repeat
--   - trim, trimLeft, trimRight
--   - contains, indexOf, startsWith, endsWith
--   - split (returns a slice), join (slice -> string)
--   - replace (all occurrences), substring

local s = {}

function s.len(str) return #str end
function s.upper(str) return string.upper(str) end
function s.lower(str) return string.lower(str) end
function s.repeat_(str, n) return string.rep(str, n) end

-- trim removes leading and trailing ASCII whitespace.
function s.trim(str)
  return (str:gsub("^%s+", ""):gsub("%s+$", ""))
end

function s.trimLeft(str)
  return (str:gsub("^%s+", ""))
end

function s.trimRight(str)
  return (str:gsub("%s+$", ""))
end

-- contains reports whether sub appears in str. The find is plain (no pattern
-- matching) so characters like '.' and '%' are treated literally.
function s.contains(str, sub)
  return string.find(str, sub, 1, true) ~= nil
end

-- indexOf returns the 1-based index of the first occurrence of sub, or 0 if
-- sub is not present.
function s.indexOf(str, sub)
  local i = string.find(str, sub, 1, true)
  if i == nil then return 0 end
  return i
end

function s.startsWith(str, prefix)
  return string.sub(str, 1, #prefix) == prefix
end

function s.endsWith(str, suffix)
  if #suffix == 0 then return true end
  return string.sub(str, -#suffix) == suffix
end

-- split divides str on the literal separator sep, returning a slice (array
-- table) of pieces. An empty separator returns the whole string as one piece.
function s.split(str, sep)
  local out = {}
  if sep == "" then
    out[1] = str
    return out
  end
  local pos = 1
  while true do
    local i = string.find(str, sep, pos, true)
    if i == nil then
      out[#out + 1] = string.sub(str, pos)
      break
    end
    out[#out + 1] = string.sub(str, pos, i - 1)
    pos = i + #sep
  end
  return out
end

-- join concatenates the pieces of a slice with sep between them.
function s.join(parts, sep)
  return table.concat(parts, sep)
end

-- replace swaps every literal occurrence of old with new.
function s.replace(str, old, new)
  if old == "" then return str end
  local out = {}
  local pos = 1
  while true do
    local i = string.find(str, old, pos, true)
    if i == nil then
      out[#out + 1] = string.sub(str, pos)
      break
    end
    out[#out + 1] = string.sub(str, pos, i - 1)
    out[#out + 1] = new
    pos = i + #old
  end
  return table.concat(out)
end

-- substring returns the 1-based inclusive slice [from, to] of str.
function s.substring(str, from, to)
  return string.sub(str, from, to)
end

return s
