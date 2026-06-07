-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/wl_ui
-- Purpose: Exposes Wayland window creation, event dispatching, and text
-- rendering APIs backed by libnovel_wayland C FFI shim.
--
-- Key Components:
--   - createWindow(): Spawns a Wayland client window using xdg-shell
--   - clear(): Paints solid color on the window memory buffer
--   - drawText(): Draws character bitmaps on coordinate offsets
--   - present(): Triggers compositor buffer commits
--   - dispatch(): Polls and dispatches client connection events
--   - close(): Shuts down display connection and releases memory
--
-- Dependencies:
--   - std.ffi: Lua FFI to invoke libnovel_wayland

local ffi = require("ffi")
local novel = require("novel")

ffi.cdef[[
struct WaylandWindow;
struct WaylandWindow* wayland_open(const char* title, int width, int height);
void wayland_clear(struct WaylandWindow* win, uint32_t color);
void wayland_draw_text(struct WaylandWindow* win, int x, int y, const char* text, uint32_t color);
void wayland_present(struct WaylandWindow* win);
int wayland_dispatch(struct WaylandWindow* win);
void wayland_close(struct WaylandWindow* win);
]]

local wl_ui = {}

-- Resolve candidates for libnovel_wayland shared library based on OS.
local function libCandidates()
  local cands = {}
  local ext = ".so"
  if ffi.os == "OSX" then
    ext = ".dylib"
  elseif ffi.os == "Windows" then
    ext = ".dll"
  end
  local novelPath = package.searchpath and package.searchpath("novel", package.path)
  if novelPath then
    local dir = novelPath:match("(.*/)") or "./"
    cands[#cands + 1] = dir .. "libnovel_wayland" .. ext
  end
  cands[#cands + 1] = "novel_wayland"
  return cands
end

local lib = nil
local function loadLib()
  if lib ~= nil then return lib, nil end
  for _, cand in ipairs(libCandidates()) do
    local ok, handle = pcall(ffi.load, cand)
    if ok then
      lib = handle
      return lib, nil
    end
  end
  return nil, novel.error(
    "std/wl_ui: libnovel_wayland not found; build it with `make wl-lib`")
end

--[[
createWindow creates a Wayland window mapping it through the desktop compositor.

    params:
          title: window title text
          width: width in pixels
          height: height in pixels
    returns:
          window: struct WaylandWindow pointer, or (nil, error)
]]
function wl_ui.createWindow(title, width, height)
  local l, err = loadLib()
  if l == nil then return nil, err end
  
  local win = l.wayland_open(title, width, height)
  if win == nil then
    return nil, novel.error("wl_ui.createWindow: failed to open Wayland display or register surfaces")
  end
  
  return win, nil
end

--[[
clear fills the window buffer with a background color.

    params:
          win: window pointer
          color: color value in XRGB format
]]
function wl_ui.clear(win, color)
  local l = loadLib()
  if l and win then
    l.wayland_clear(win, color)
  end
end

--[[
drawText renders text characters into the buffer at the coordinates specified.

    params:
          win: window pointer
          x: x-offset in pixels
          y: y-offset in pixels
          text: message string
          color: color value in XRGB format
]]
function wl_ui.drawText(win, x, y, text, color)
  local l = loadLib()
  if l and win then
    l.wayland_draw_text(win, x, y, text, color)
  end
end

--[[
present commits client frame buffer damage and requests redraws.

    params:
          win: window pointer
]]
function wl_ui.present(win)
  local l = loadLib()
  if l and win then
    l.wayland_present(win)
  end
end

--[[
dispatch non-blockingly reads events from Wayland display socket.

    params:
          win: window pointer
    returns:
          int: 0 if window is running, -1 if window was closed or connection lost
]]
function wl_ui.dispatch(win)
  local l = loadLib()
  if l and win then
    return l.wayland_dispatch(win)
  end
  return -1
end

--[[
close releases all client side and connection proxies.

    params:
          win: window pointer
]]
function wl_ui.close(win)
  local l = loadLib()
  if l and win then
    l.wayland_close(win)
  end
end

return wl_ui
