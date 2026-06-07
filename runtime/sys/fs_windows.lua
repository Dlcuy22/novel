-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/fs_windows (low-level Windows filesystem FFI for the std/fs module)
--
-- Purpose:
--   Wraps the Win32 filesystem API (FindFirstFileA/FindNextFileA/FindClose,
--   GetFileAttributesExA, CreateDirectoryA, RemoveDirectoryA, DeleteFileA,
--   MoveFileA) using kernel32.dll.
--
-- Key Components:
--   - readDir(path): returns (filenames_table, nil) or (nil, err)
--   - stat(path): returns (file_info_table, nil) or (nil, err)
--   - exists(path): returns boolean
--   - mkdir(path): returns (true, nil) or (nil, err)
--   - rmdir(path): returns (true, nil) or (nil, err)
--   - unlink(path): returns (true, nil) or (nil, err)
--   - rename(oldpath, newpath): returns (true, nil) or (nil, err)

local ffi = require("ffi")

ffi.cdef[[
  typedef struct _FILETIME {
      unsigned long dwLowDateTime;
      unsigned long dwHighDateTime;
  } FILETIME;

  typedef struct _WIN32_FILE_ATTRIBUTE_DATA {
      unsigned long dwFileAttributes;
      FILETIME ftCreationTime;
      FILETIME ftLastAccessTime;
      FILETIME ftLastWriteTime;
      unsigned long nFileSizeHigh;
      unsigned long nFileSizeLow;
  } WIN32_FILE_ATTRIBUTE_DATA;

  typedef struct _WIN32_FIND_DATAA {
      unsigned long dwFileAttributes;
      FILETIME ftCreationTime;
      FILETIME ftLastAccessTime;
      FILETIME ftLastWriteTime;
      unsigned long nFileSizeHigh;
      unsigned long nFileSizeLow;
      unsigned long dwReserved0;
      unsigned long dwReserved1;
      char cFileName[260];
      char cAlternateFileName[14];
  } WIN32_FIND_DATAA;

  unsigned long GetFileAttributesA(const char *lpFileName);
  int GetFileAttributesExA(const char *lpFileName, int fInfoLevelId, void *lpFileInformation);
  
  void *FindFirstFileA(const char *lpFileName, WIN32_FIND_DATAA *lpFindFileData);
  int FindNextFileA(void *hFindFile, WIN32_FIND_DATAA *lpFindFileData);
  int FindClose(void *hFindFile);

  int CreateDirectoryA(const char *lpPathName, void *lpSecurityAttributes);
  int RemoveDirectoryA(const char *lpPathName);
  int DeleteFileA(const char *lpFileName);
  int MoveFileA(const char *lpExistingFileName, const char *lpNewFileName);

  unsigned long GetLastError();
]]

local kernel32 = ffi.load("kernel32")
local M = {}

local INVALID_HANDLE_VALUE = ffi.cast("void *", -1)
local INVALID_FILE_ATTRIBUTES = 0xFFFFFFFF
local FILE_ATTRIBUTE_DIRECTORY = 0x10

-- Helper to format Windows errors.
local function lastError()
  return "Windows error code " .. tostring(kernel32.GetLastError())
end

-- readDir lists filenames in a folder using FindFirstFile/FindNextFile.
function M.readDir(path)
  local pattern = path
  if pattern:sub(-1) == "/" or pattern:sub(-1) == "\\" then
    pattern = pattern .. "*"
  else
    pattern = pattern .. "/*"
  end

  local find_data = ffi.new("WIN32_FIND_DATAA")
  local h = kernel32.FindFirstFileA(pattern, find_data)
  if h == INVALID_HANDLE_VALUE then
    local err = kernel32.GetLastError()
    -- ERROR_FILE_NOT_FOUND (2) or ERROR_PATH_NOT_FOUND (3) means directory empty or missing.
    if err == 2 or err == 3 then
      -- If the directory actually exists but is empty, return empty table.
      if M.exists(path) then
        return {}, nil
      end
    end
    return nil, lastError()
  end

  local filenames = {}
  while true do
    local name = ffi.string(find_data.cFileName)
    if name ~= "." and name ~= ".." then
      table.insert(filenames, name)
    end
    if kernel32.FindNextFileA(h, find_data) == 0 then
      break
    end
  end
  kernel32.FindClose(h)

  return filenames, nil
end

-- stat retrieves FileInfo for a given path.
function M.stat(path)
  local attr_data = ffi.new("WIN32_FILE_ATTRIBUTE_DATA")
  -- GetFileExInfoStandard = 0
  if kernel32.GetFileAttributesExA(path, 0, attr_data) == 0 then
    return nil, lastError()
  end

  local attr = attr_data.dwFileAttributes
  local is_dir = bit.band(attr, FILE_ATTRIBUTE_DIRECTORY) ~= 0

  -- Size calculation from high and low parts.
  local size = attr_data.nFileSizeLow + attr_data.nFileSizeHigh * 4294967296

  -- Convert FILETIME to Unix epoch timestamp.
  -- FILETIME is number of 100-nanosecond intervals since Jan 1, 1601.
  -- Difference between 1601 and 1970 in 100-ns ticks is 116444736000000000.
  local low = attr_data.ftLastWriteTime.dwLowDateTime
  local high = attr_data.ftLastWriteTime.dwHighDateTime
  local win_time = low + high * 4294967296
  local mtime = math.floor((win_time - 116444736000000000) / 10000000)

  -- Get basename.
  local name = path:match("([^/\\]+)$") or path
  if name == "" then name = path end

  return {
    name = name,
    size = size,
    is_dir = is_dir,
    mod_time = mtime
  }, nil
end

-- exists checks path attributes.
function M.exists(path)
  return kernel32.GetFileAttributesA(path) ~= INVALID_FILE_ATTRIBUTES
end

-- mkdir creates a directory.
function M.mkdir(path)
  if kernel32.CreateDirectoryA(path, nil) == 0 then
    return nil, lastError()
  end
  return true, nil
end

-- rmdir deletes an empty directory.
function M.rmdir(path)
  if kernel32.RemoveDirectoryA(path) == 0 then
    return nil, lastError()
  end
  return true, nil
end

-- unlink deletes a file.
function M.unlink(path)
  if kernel32.DeleteFileA(path) == 0 then
    return nil, lastError()
  end
  return true, nil
end

-- rename moves a file or directory.
function M.rename(oldpath, newpath)
  if kernel32.MoveFileA(oldpath, newpath) == 0 then
    return nil, lastError()
  end
  return true, nil
end

return M
