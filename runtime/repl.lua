-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: repl (persistent REPL driver for LuaJIT)
--
-- Purpose:
--   Backs `novel repl`. The Go front end transpiles each input line to a Lua
--   chunk and streams it here; this driver executes the chunk in a persistent
--   session so variables, functions, and imports survive across lines, the way
--   Python's REPL does. Without this, recompiling the whole session each line
--   would re-run side effects and lose mutations.
--
-- Protocol (line-framed over stdin/stdout):
--   Go writes a chunk, then a line equal to EOF_MARK. This driver runs it, lets
--   the program's own output go to stdout, then writes DONE_MARK so Go knows the
--   line finished and can prompt again. Reading EOF on stdin exits.
--
-- Design:
--   * A session table with __index = _G holds user globals and imports, so a
--     chunk's `x = 5` or `io = require("std/io")` persists without clobbering the
--     real Lua globals the driver relies on (print, io, string, novel).
--   * Each chunk runs as a spawned green thread then novel.run() drains the
--     scheduler, so a top-level recv/send can yield (the bare main thread can't).
--   * novel.run() is pcall-guarded; a crashing line prints its error, the
--     scheduler is reset, and the session continues.

local novel = require("novel")

-- Keep private handles to the real globals the driver needs, so a user `import`
-- that rebinds `io` (to std/io) in the session can never break framing.
local read = io.read
local write = io.write
local flush = io.flush

local EOF_MARK = "__NOVEL_REPL_EOF_8f3a__"
local DONE_MARK = "__NOVEL_REPL_DONE_8f3a__"

-- The session environment: user-defined names land here; everything else (Lua
-- built-ins, the novel runtime) is read through __index from _G.
local session = setmetatable({ novel = novel }, { __index = _G })

-- readChunk reads framed lines until EOF_MARK and returns the joined source, or
-- nil at end of input.
local function readChunk()
  local lines = {}
  while true do
    local line = read("*l")
    if line == nil then
      return nil
    end
    if line == EOF_MARK then
      return table.concat(lines, "\n")
    end
    lines[#lines + 1] = line
  end
end

-- runChunk loads and executes one chunk in the session, reporting any compile or
-- runtime error on stdout (so the Go front end relays it like normal output).
local function runChunk(src)
  if src == "" then
    return
  end
  local fn, lerr = loadstring(src, "=repl")
  if not fn then
    write("error: " .. tostring(lerr) .. "\n")
    return
  end
  setfenv(fn, session)
  novel.spawn(fn)
  local ok, rerr = pcall(novel.run)
  if not ok then
    -- novel.run wraps a crashed green thread; surface the inner message.
    write("error: " .. tostring(rerr) .. "\n")
    novel.reset()
  end
end

while true do
  local src = readChunk()
  if src == nil then
    break
  end
  runChunk(src)
  write(DONE_MARK, "\n")
  flush()
end
