-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: sys/poll (low-level event loop polling FFI backend dispatcher)
--
-- Purpose:
--   Selects between the POSIX and Windows implementations of sys/poll.

local os_mod = require("std/os")

if os_mod.isWindows() then
  return require("sys/poll_windows")
else
  return require("sys/poll_posix")
end
