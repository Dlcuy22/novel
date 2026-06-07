-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/image (PNG/JPEG image load, edit, and save for Novel)
--
-- Purpose:
--   Decode and encode images via stb_image / stb_image_write through a small C
--   shim (libnovel_image), loaded with the FFI. stb is setjmp-free, so it is
--   safe across the FFI boundary (unlike libjpeg/libpng directly). Pixels are
--   always 8-bit RGBA, row-major, so the API is uniform regardless of the
--   source format. Imported with `import std.image`; calls lower to dot-calls
--   (image.load(path)), and the returned image is an object used with method
--   syntax (img.get(x, y) -> img:get(x, y)).
--
-- Key Components:
--   - load(path) -> (img, err): decode PNG/JPEG/BMP/TGA/GIF/... to RGBA
--   - new(w, h) -> img: a blank (transparent) RGBA image
--   - img.width / img.height: dimensions (fields, not methods)
--   - img:get(x, y) -> r, g, b, a: read a pixel (coords are 0-based)
--   - img:set(x, y, r, g, b, a): write a pixel (a defaults to 255)
--   - img:savePNG(path) -> (ok, err) ; img:saveJPG(path, quality) -> (ok, err)
--
-- Coordinates:
--   0-based, origin top-left: x in 0..width-1, y in 0..height-1 (the standard
--   image convention, not Novel's 1-based slice indexing).
--
-- Availability:
--   Requires libnovel_image, built by `make image-lib`. If the library is not
--   found, load/new return an error explaining how to build it, rather than
--   crashing, so programs that never touch images still run.

local ffi = require("ffi")
local novel = require("novel")

ffi.cdef[[
unsigned char *novel_image_load(const char *path, int *w, int *h, int *channels);
unsigned char *novel_image_load_mem(const unsigned char *buf, int len, int *w, int *h, int *channels);
const char *novel_image_failure(void);
void novel_image_free(unsigned char *pixels);
unsigned char *novel_image_encode_png(const unsigned char *pixels, int w, int h, int *out_len);
unsigned char *novel_image_encode_jpg(const unsigned char *pixels, int w, int h, int quality, int *out_len);
void novel_image_free_mem(unsigned char *buf);
]]

local image = {}

-- libCandidates lists shared-library paths to try, derived from where the Lua
-- modules resolve: the C shim is built next to novel.lua in the runtime dir.
local function libCandidates()
  local cands = {}
  local ext = ".so"
  if ffi.os == "OSX" then ext = ".dylib"
  elseif ffi.os == "Windows" then ext = ".dll" end
  -- The directory novel.lua resolves from (the runtime dir).
  local novelPath = package.searchpath and package.searchpath("novel", package.path)
  if novelPath then
    local dir = novelPath:match("(.*/)") or "./"
    cands[#cands + 1] = dir .. "libnovel_image" .. ext
  end
  -- Bare name: let the system dynamic linker search its default paths.
  cands[#cands + 1] = "novel_image"
  return cands
end

-- lib is loaded lazily on first use so importing std.image never fails just
-- because the shared library hasn't been built. loadLib returns (lib, err).
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
    "std/image: libnovel_image not found; build it with `make image-lib`")
end

-- Image object: width, height, and a flat RGBA pixel buffer (uint8_t[w*h*4]).
-- The buffer is FFI cdata, so per-pixel access is fast and memory-compact
-- rather than a giant Lua table.
local Image = {}
Image.__index = Image

-- writeBytes writes a binary string to a file. Returns (true, nil) or
-- (nil, err). Shared by the save methods.
local function writeBytes(path, data)
  local f, oerr = io.open(path, "wb")
  if f == nil then
    return nil, novel.error(oerr or ("cannot open " .. tostring(path)))
  end
  f:write(data)
  f:close()
  return true, nil
end

local function newImage(w, h, pixels)
  local buf = pixels or ffi.new("uint8_t[?]", w * h * 4) -- zero-filled = transparent
  return setmetatable({ width = w, height = h, _buf = buf }, Image)
end

-- offset returns the buffer index of pixel (x, y)'s red component, or nil if
-- the coordinate is out of bounds. Coordinates are 0-based, top-left origin.
function Image:offset(x, y)
  if x < 0 or y < 0 or x >= self.width or y >= self.height then
    return nil
  end
  return (y * self.width + x) * 4
end

-- get reads pixel (x, y), returning r, g, b, a (each 0..255). Out-of-bounds
-- reads return 0, 0, 0, 0.
function Image:get(x, y)
  local o = self:offset(x, y)
  if o == nil then return 0, 0, 0, 0 end
  local b = self._buf
  return b[o], b[o + 1], b[o + 2], b[o + 3]
end

-- set writes pixel (x, y). Alpha defaults to 255 (opaque). Out-of-bounds writes
-- are ignored. Component values are masked to 0..255.
function Image:set(x, y, r, g, b, a)
  local o = self:offset(x, y)
  if o == nil then return end
  local buf = self._buf
  buf[o]     = r % 256
  buf[o + 1] = g % 256
  buf[o + 2] = b % 256
  buf[o + 3] = (a == nil) and 255 or (a % 256)
end

-- savePNG encodes the image as PNG and writes it to path. Returns (true, nil)
-- or (nil, err).
function Image:savePNG(path)
  local l, lerr = loadLib()
  if l == nil then return nil, lerr end
  local outLen = ffi.new("int[1]")
  local enc = l.novel_image_encode_png(self._buf, self.width, self.height, outLen)
  if enc == nil then
    return nil, novel.error("image: PNG encode failed")
  end
  local data = ffi.string(enc, outLen[0])
  l.novel_image_free_mem(enc)
  return writeBytes(path, data)
end

-- saveJPG encodes the image as JPEG (quality 1..100, default 90) and writes it
-- to path. Returns (true, nil) or (nil, err).
function Image:saveJPG(path, quality)
  local l, lerr = loadLib()
  if l == nil then return nil, lerr end
  local outLen = ffi.new("int[1]")
  local enc = l.novel_image_encode_jpg(self._buf, self.width, self.height,
                                       quality or 90, outLen)
  if enc == nil then
    return nil, novel.error("image: JPEG encode failed")
  end
  local data = ffi.string(enc, outLen[0])
  l.novel_image_free_mem(enc)
  return writeBytes(path, data)
end

-- new returns a blank w-by-h image, fully transparent (all zero). Returns
-- (img, nil) or (nil, err) if the library is unavailable (cdata alloc needs
-- nothing from the lib, but we keep the signature uniform).
function image.new(w, h)
  if w <= 0 or h <= 0 then
    return nil, novel.error("image.new: width and height must be positive")
  end
  return newImage(w, h), nil
end

-- load decodes an image file to an RGBA image. Returns (img, nil) or (nil, err).
function image.load(path)
  local l, lerr = loadLib()
  if l == nil then return nil, lerr end
  local w = ffi.new("int[1]")
  local h = ffi.new("int[1]")
  local ch = ffi.new("int[1]")
  local pixels = l.novel_image_load(path, w, h, ch)
  if pixels == nil then
    return nil, novel.error("image.load: " .. ffi.string(l.novel_image_failure()))
  end
  -- Copy stb's buffer into our own GC-managed cdata, then free stb's, so the
  -- image owns a buffer with normal Lua lifetime.
  local n = w[0] * h[0] * 4
  local buf = ffi.new("uint8_t[?]", n)
  ffi.copy(buf, pixels, n)
  l.novel_image_free(pixels)
  return newImage(w[0], h[0], buf), nil
end

return image
