-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/io (console and file I/O for the Novel runtime)
--
-- Purpose:
--   Standard output, input, and whole-file helpers. Console reads cooperate
--   with the green-thread scheduler: a blocked read parks just the calling
--   thread (via novel.iowait) instead of stalling the VM, so a server can read
--   stdin while other green threads keep running. Imported with `import std.io`;
--   calls lower to dot-calls (io.println(x), io.readLine()).
--
-- Key Components:
--   - println(...) / print(...): write to stdout (with / without a newline)
--   - eprintln(...) / eprint(...): write to stderr
--   - readLine(): read one line from stdin (newline stripped), nil at EOF
--   - readAll(): read all of stdin until EOF, as one string
--   - readFile(path): read a whole file, returns (contents, err)
--   - writeFile(path, data): write a whole file, returns (ok, err)
--
-- Note:
--   readLine/readAll poll stdin (fd 0) for readiness before each read so they
--   yield to other green threads while waiting, then read the bytes that are
--   ready. stdin is left in its normal blocking mode (poll guarantees the
--   following read won't block), so the parent shell's terminal is unaffected.

local novel = require("novel")
local posix = require("sys/posix")

local io_mod = {}

function io_mod.println(...)
  print(...)
end

function io_mod.print(...)
  io.write(...)
end

function io_mod.eprintln(...)
  io.stderr:write(...)
  io.stderr:write("\n")
end

function io_mod.eprint(...)
  io.stderr:write(...)
end

-- Buffered stdin reader. We accumulate bytes read from fd 0 and hand out lines
-- or the whole stream from this buffer, refilling via the reactor as needed.
local STDIN = 0
local inbuf = ""
local ineof = false

-- fill reads one more chunk from stdin into inbuf, parking the current green
-- thread until stdin is readable. Returns true if bytes were read, false at EOF.
local function fill()
  if ineof then return false end
  while true do
    novel.iowait(STDIN, novel.POLLIN)
    local data, err = posix.read(STDIN, 4096)
    if data == nil then
      if err == posix.EINTR or err == posix.EAGAIN then
        -- Spurious wakeup or no data yet: wait again.
      else
        ineof = true
        return false
      end
    elseif data == "" then
      ineof = true
      return false
    else
      inbuf = inbuf .. data
      return true
    end
  end
end

-- readLine returns the next line from stdin with the trailing newline removed,
-- or nil at end of input. A final line without a newline is still returned.
function io_mod.readLine()
  while true do
    local nl = string.find(inbuf, "\n", 1, true)
    if nl then
      local line = string.sub(inbuf, 1, nl - 1)
      inbuf = string.sub(inbuf, nl + 1)
      -- Strip a trailing CR so Windows-style CRLF input yields a clean line.
      if string.sub(line, -1) == "\r" then
        line = string.sub(line, 1, -2)
      end
      return line
    end
    if not fill() then
      if inbuf == "" then return nil end
      local line = inbuf
      inbuf = ""
      return line
    end
  end
end

-- readAll consumes the rest of stdin and returns it as one string ("" at EOF).
function io_mod.readAll()
  while fill() do end
  local all = inbuf
  inbuf = ""
  return all
end

-- readFile reads an entire file. Returns (contents, nil) or (nil, error).
function io_mod.readFile(path)
  local f, oerr = io.open(path, "rb")
  if f == nil then
    return nil, novel.error(oerr or ("cannot open " .. tostring(path)))
  end
  local data = f:read("*a")
  f:close()
  return data, nil
end

-- writeFile writes data to a file, truncating it. Returns (true, nil) or
-- (false, error).
function io_mod.writeFile(path, data)
  local f, oerr = io.open(path, "wb")
  if f == nil then
    return false, novel.error(oerr or ("cannot open " .. tostring(path)))
  end
  f:write(data)
  f:close()
  return true, nil
end

return io_mod
