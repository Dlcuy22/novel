-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/os (operating system and process info for Novel)
--
-- Purpose:
--   Platform detection, environment access, process arguments, and exit. The
--   platform/arch helpers are the canonical way Novel programs and other stdlib
--   modules branch on the host OS (e.g. networking and image use them to pick an
--   implementation). Imported with `import std.os`; calls lower to dot-calls
--   (os.platform()).
--
-- Key Components:
--   - platform(): "linux" | "macos" | "windows" | "bsd" | "other"
--   - arch(): CPU architecture string ("x64", "arm64", ...)
--   - getenv(name): environment variable value, or nil if unset
--   - setenv(name, value): best-effort set (no-op return on platforms w/o it)
--   - args(): array of command-line arguments passed to the program
--   - exit(code): terminate the process with an integer status
--
-- Note:
--   platform()/arch() come from LuaJIT's ffi.os/ffi.arch, so they report the
--   build target, which on these platforms matches the running host.

local ffi = require("ffi")

local os_mod = {}

-- platform maps ffi.os to a lowercase Novel platform name. ffi.os is one of
-- "Linux", "OSX", "Windows", "BSD", "POSIX", "Other".
local platformName
do
  local m = {
    Linux = "linux",
    OSX = "macos",
    Windows = "windows",
    BSD = "bsd",
  }
  platformName = m[ffi.os] or "other"
end

function os_mod.platform()
  return platformName
end

-- isWindows / isUnix are convenience predicates other modules use to branch.
function os_mod.isWindows()
  return platformName == "windows"
end

function os_mod.isUnix()
  return platformName == "linux" or platformName == "macos" or platformName == "bsd"
end

-- arch returns the CPU architecture LuaJIT was built for ("x64", "x86",
-- "arm", "arm64", "mips", ...), lowercased.
function os_mod.arch()
  return string.lower(ffi.arch)
end

-- getenv returns the value of an environment variable, or nil if it is unset.
function os_mod.getenv(name)
  return os.getenv(name)
end

-- args returns the program's command-line arguments as a 1-based array. Under
-- `novel run` the LuaJIT `arg` table holds whatever followed the script; a copy
-- is returned so callers cannot mutate the global.
function os_mod.args()
  local out = {}
  if type(arg) == "table" then
    for i = 1, #arg do
      out[i] = arg[i]
    end
  end
  return out
end

-- exit terminates the process with the given status code (default 0). It does
-- NOT drain the scheduler; pending green threads are abandoned, matching the
-- semantics of an explicit process exit.
function os_mod.exit(code)
  os.exit(code or 0)
end

return os_mod
