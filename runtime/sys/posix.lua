-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/posix (low-level file descriptor I/O FFI backend dispatcher)
--
-- Purpose:
--   Selects between the POSIX and Windows implementations of sys/posix.

local os_mod = require("std/os")

if os_mod.isWindows() then
  return require("sys/posix_windows")
else
  return require("sys/posix_posix")
end
