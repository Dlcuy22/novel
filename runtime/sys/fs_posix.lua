-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/fs_posix (low-level POSIX filesystem FFI for the std/fs module)
--
-- Purpose:
--   Wraps the POSIX filesystem API (opendir/readdir/closedir, stat, access,
--   mkdir, rmdir, unlink, rename) for Linux and macOS/BSD systems.
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
local posix = require("sys/posix")

local isBSD = (ffi.os == "OSX" or ffi.os == "BSD")

-- 1. Declare C structures depending on OS target.
if ffi.os == "Linux" then
  ffi.cdef[[
    struct dirent {
      uint64_t       d_ino;
      int64_t        d_off;
      unsigned short d_reclen;
      unsigned char  d_type;
      char           d_name[256];
    };
    struct stat {
      unsigned long  st_dev;
      unsigned long  st_ino;
      unsigned long  st_nlink;
      unsigned int   st_mode;
      unsigned int   st_uid;
      unsigned int   st_gid;
      int            __pad0;
      unsigned long  st_rdev;
      long           st_size;
      long           st_blksize;
      long           st_blocks;
      long           st_atime;
      unsigned long  st_atime_nsec;
      long           st_mtime;
      unsigned long  st_mtime_nsec;
      long           st_ctime;
      unsigned long  st_ctime_nsec;
      long           __unused[3];
    };
  ]]
elseif isBSD then
  ffi.cdef[[
    struct dirent {
      uint32_t       d_fileno;
      uint16_t       d_reclen;
      uint8_t        d_type;
      uint8_t        d_namlen;
      char           d_name[1024];
    };
    struct stat {
      int32_t   st_dev;
      uint32_t  st_mode;
      uint16_t  st_nlink;
      uint64_t  st_ino;
      uint32_t  st_uid;
      uint32_t  st_gid;
      int32_t   st_rdev;
      int64_t   st_atimespec_sec;
      int64_t   st_atimespec_nsec;
      int64_t   st_mtimespec_sec;
      int64_t   st_mtimespec_nsec;
      int64_t   st_ctimespec_sec;
      int64_t   st_ctimespec_nsec;
      int64_t   st_birthtimespec_sec;
      int64_t   st_birthtimespec_nsec;
      int64_t   st_size;
      int64_t   st_blocks;
      int32_t   st_blksize;
      uint32_t  st_flags;
      uint32_t  st_gen;
      int32_t   st_lspare;
      int64_t   st_qspare[2];
    };
  ]]
end

ffi.cdef[[
  typedef struct DIR DIR;
  DIR *opendir(const char *name);
  struct dirent *readdir(DIR *dirp);
  int closedir(DIR *dirp);

  int stat(const char *path, struct stat *buf);
  int access(const char *pathname, int mode);
  int mkdir(const char *pathname, unsigned int mode);
  int rmdir(const char *pathname);
  int unlink(const char *pathname);
  int rename(const char *oldpath, const char *newpath);
]]

local C = ffi.C
local M = {}

-- Helper to extract errno and get strerror.
local function lastError()
  local e = ffi.errno()
  return posix.strerror(e)
end

-- readDir returns a list of filenames in a directory.
function M.readDir(path)
  local dir = C.opendir(path)
  if dir == nil then
    return nil, lastError()
  end
  local filenames = {}
  while true do
    local entry = C.readdir(dir)
    if entry == nil then
      break
    end
    local name = ffi.string(entry.d_name)
    if name ~= "." and name ~= ".." then
      table.insert(filenames, name)
    end
  end
  C.closedir(dir)
  return filenames, nil
end

-- stat retrieves FileInfo for a given path.
function M.stat(path)
  local buf = ffi.new("struct stat")
  if C.stat(path, buf) ~= 0 then
    return nil, lastError()
  end

  local mode = buf.st_mode
  local is_dir = bit.band(mode, 0xF000) == 0x4000 -- S_ISDIR
  
  -- Mod time extraction differs slightly by OS struct field name.
  local mtime
  if isBSD then
    mtime = tonumber(buf.st_mtimespec_sec)
  else
    mtime = tonumber(buf.st_mtime)
  end

  -- Extract just the basename from path for the name field.
  local name = path:match("^.*/([^/]+)$") or path
  if name == "" then name = path end

  return {
    name = name,
    size = tonumber(buf.st_size),
    is_dir = is_dir,
    mod_time = mtime
  }, nil
end

-- exists checks path existence using access(F_OK = 0).
function M.exists(path)
  return C.access(path, 0) == 0
end

-- mkdir creates a directory. Default mode is 0777 (511 decimal).
function M.mkdir(path)
  if C.mkdir(path, 511) ~= 0 then
    return nil, lastError()
  end
  return true, nil
end

-- rmdir removes an empty directory.
function M.rmdir(path)
  if C.rmdir(path) ~= 0 then
    return nil, lastError()
  end
  return true, nil
end

-- unlink deletes a file.
function M.unlink(path)
  if C.unlink(path) ~= 0 then
    return nil, lastError()
  end
  return true, nil
end

-- rename moves or renames a file/directory.
function M.rename(oldpath, newpath)
  if C.rename(oldpath, newpath) ~= 0 then
    return nil, lastError()
  end
  return true, nil
end

return M
