-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/exec (subprocess execution for Novel)
--
-- Purpose:
--   Spawns external processes, writes/reads pipes, and reaps process exit
--   status codes. Designed to behave similarly to Go's os/exec package.
--
-- Key Components:
--   - command(path, args): builds a Cmd execution controller.
--   - Cmd:run(): runs command, blocks asynchronously, and returns exit code.
--   - Cmd:output(): runs command and returns stdout.
--   - Cmd:combinedOutput(): runs command and returns stdout + stderr.
--
-- Dependencies:
--   - sys/exec_posix or sys/exec_windows: FFI backend selected at runtime.
--   - std/os: platform detection and env var access.
--   - std/fs: filesystem checks for lookPath.
--
-- Error Types:
--   - novel.error returned on execution failures or non-zero exit codes.
--

local os = require("std/os")
local fs = require("std/fs")
local novel = require("novel")

local backend
if os.isWindows() then
  backend = require("sys/exec_windows")
else
  backend = require("sys/exec_posix")
end

local exec = {}

local Cmd = {}
Cmd.__index = Cmd

-- lookPath searches for an executable named file in the directories
-- named by the PATH environment variable. If file contains a slash,
-- it is tried directly and the PATH is not searched.
local function lookPath(file)
  if file:find("/") or (os.isWindows() and (file:find("\\") or file:find(":"))) then
    if fs.exists(file) then
      return file, nil
    end
    return nil, novel.error("executable file not found: " .. file)
  end

  local path = os.getenv("PATH")
  if not path then
    return nil, novel.error("PATH environment variable not set")
  end

  local sep = os.isWindows() and ";" or ":"
  for dir in path:gmatch("[^" .. sep .. "]+") do
    local fpath = dir .. "/" .. file
    if os.isWindows() then
      fpath = dir .. "\\" .. file
      -- On Windows, also check common extensions if not specified
      if not file:lower():find("%.exe$") and not file:lower():find("%.bat$") and not file:lower():find("%.cmd$") then
        for _, ext in ipairs({".exe", ".bat", ".cmd"}) do
          local fext = fpath .. ext
          if fs.exists(fext) then
            return fext, nil
          end
        end
      end
    end
    if fs.exists(fpath) then
      return fpath, nil
    end
  end

  return nil, novel.error("executable file not found in PATH: " .. file)
end

-- run executes the command and waits for it to complete.
--
-- returns:
--   code: process exit status code (0 on success, non-zero on failure)
--   err: error object if the command failed to execute or exited non-zero
function Cmd:run()
  if self.err then
    return -1, self.err
  end
  local stdout, stderr, code, err = backend.execute(self.path, self.args, false, false)
  if err ~= nil then
    return -1, err
  end
  if code ~= 0 then
    return code, novel.error("exit status " .. tostring(code))
  end
  return code, nil
end

-- output executes the command and returns its standard output.
--
-- returns:
--   stdout: captured stdout string
--   err: error object if the command failed or exited non-zero
function Cmd:output()
  if self.err then
    return nil, self.err
  end
  local stdout, stderr, code, err = backend.execute(self.path, self.args, true, false)
  if err ~= nil then
    return nil, err
  end
  if code ~= 0 then
    return stdout or "", novel.error("exit status " .. tostring(code))
  end
  return stdout or "", nil
end

-- combinedOutput executes the command and returns its combined stdout and stderr.
--
-- returns:
--   output: combined stdout and stderr string
--   err: error object if the command failed or exited non-zero
function Cmd:combinedOutput()
  if self.err then
    return nil, self.err
  end
  local stdout, stderr, code, err = backend.execute(self.path, self.args, true, true)
  if err ~= nil then
    return nil, err
  end
  local combined = (stdout or "") .. (stderr or "")
  if code ~= 0 then
    return combined, novel.error("exit status " .. tostring(code))
  end
  return combined, nil
end

-- command prepares the Cmd execution controller for a binary.
--
-- params:
--   path: absolute or relative path to the binary (or binary name resolved via PATH)
--   args: array of command line arguments
-- returns:
--   cmd: Cmd instance
function exec.command(path, args)
  local resolved, err = lookPath(path)
  return setmetatable({
    path = resolved or path,
    args = args or {},
    err = err,
  }, Cmd)
end

return exec
