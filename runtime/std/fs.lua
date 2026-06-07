-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/fs (filesystem operations for the Novel runtime)
--
-- Purpose:
--   Exposes standard filesystem operations (readDir, stat, exists, mkdir,
--   mkdirAll, remove, removeAll, rename) in a platform-independent manner.
--   Uses std/os to dynamically load the appropriate OS backend (fs_posix
--   or fs_windows) and formats errors using Novel's standard error wrapper.
--
-- Key Components:
--   - readDir(path): returns (filenames_table, nil) or (nil, error)
--   - stat(path): returns (file_info_table, nil) or (nil, error)
--   - exists(path): returns boolean
--   - mkdir(path): returns (true, nil) or (nil, error)
--   - mkdirAll(path): returns (true, nil) or (nil, error)
--   - remove(path): returns (true, nil) or (nil, error)
--   - removeAll(path): returns (true, nil) or (nil, error)
--   - rename(oldpath, newpath): returns (true, nil) or (nil, error)

local novel = require("novel")
local os_mod = require("std/os")

-- Choose the backend implementation based on OS detection.
local backend
if os_mod.isWindows() then
  backend = require("sys/fs_windows")
else
  backend = require("sys/fs_posix")
end

local fs_mod = {}

-- readDir returns a list of files/directories inside the directory path.
function fs_mod.readDir(path)
  local files, err = backend.readDir(path)
  if files == nil then
    return nil, novel.error(err)
  end
  return files, nil
end

-- stat returns file information (name, size, is_dir, mod_time).
function fs_mod.stat(path)
  local info, err = backend.stat(path)
  if info == nil then
    return nil, novel.error(err)
  end
  return info, nil
end

-- exists returns true if path exists, false otherwise.
function fs_mod.exists(path)
  return backend.exists(path)
end

-- mkdir creates a single directory.
function fs_mod.mkdir(path)
  local ok, err = backend.mkdir(path)
  if not ok then
    return nil, novel.error(err)
  end
  return true, nil
end

-- mkdirAll recursively creates all nested directories.
function fs_mod.mkdirAll(path)
  if fs_mod.exists(path) then
    local info, err = fs_mod.stat(path)
    if info and info.is_dir then
      return true, nil
    elseif info then
      return nil, novel.error("path exists and is not a directory: " .. tostring(path))
    else
      return nil, novel.error(err)
    end
  end

  -- Find parent path by extracting everything before the last slash.
  local parent = path:match("^(.*)[/\\][^/\\]*$")
  if parent and parent ~= "" and parent ~= path then
    local ok, err = fs_mod.mkdirAll(parent)
    if not ok then
      return nil, err
    end
  end

  local ok, err = backend.mkdir(path)
  if not ok then
    return nil, novel.error(err)
  end
  return true, nil
end

-- remove deletes a file or an empty directory.
function fs_mod.remove(path)
  local info, err = fs_mod.stat(path)
  if not info then
    return nil, novel.error(err)
  end
  
  local ok, rerr
  if info.is_dir then
    ok, rerr = backend.rmdir(path)
  else
    ok, rerr = backend.unlink(path)
  end
  
  if not ok then
    return nil, novel.error(rerr)
  end
  return true, nil
end

-- removeAll recursively deletes a directory or file and all its contents.
function fs_mod.removeAll(path)
  local info, err = fs_mod.stat(path)
  if not info then
    -- If it does not exist, return success with no error (standard Go/Rust behavior).
    if not fs_mod.exists(path) then
      return true, nil
    end
    return nil, novel.error(err)
  end

  if info.is_dir then
    local files, rerr = fs_mod.readDir(path)
    if not files then
      return nil, novel.error(rerr)
    end
    for _, name in ipairs(files) do
      local sub = path .. "/" .. name
      local ok, serr = fs_mod.removeAll(sub)
      if not ok then
        return nil, serr
      end
    end
    local ok, rmdir_err = backend.rmdir(path)
    if not ok then
      return nil, novel.error(rmdir_err)
    end
  else
    local ok, unlink_err = backend.unlink(path)
    if not ok then
      return nil, novel.error(unlink_err)
    end
  end

  return true, nil
end

-- rename moves or renames a file or directory.
function fs_mod.rename(oldpath, newpath)
  local ok, err = backend.rename(oldpath, newpath)
  if not ok then
    return nil, novel.error(err)
  end
  return true, nil
end

return fs_mod
