-- Test suite for std/image. Run with: luajit runtime/test_image.lua
-- (or `make test-image`, which builds the native library first).
-- Exits non-zero on failure. Requires libnovel_image (built by `make image-lib`).

package.path = (arg[0]:match("(.*/)") or "./") .. "?.lua;" .. package.path

local image = require("std/image")

local failures = 0
local function check(name, cond)
  if cond then
    print("ok   - " .. name)
  else
    print("FAIL - " .. name)
    failures = failures + 1
  end
end

-- new: a blank image is the requested size and fully transparent.
local img, err = image.new(4, 3)
if img == nil then
  -- The native library isn't built; skip gracefully so the suite can be run
  -- without the C toolchain. test-image (make) always builds it first.
  print("SKIP - std/image: " .. (err and err.msg or "library unavailable"))
  print("all image tests skipped (library not built)")
  os.exit(0)
end

check("new sets dimensions", img.width == 4 and img.height == 3)
do
  local r, g, b, a = img:get(0, 0)
  check("new image is transparent", r == 0 and g == 0 and b == 0 and a == 0)
end

-- set/get roundtrip, including default alpha.
img:set(0, 0, 255, 0, 0)        -- red, alpha defaults to 255
img:set(3, 2, 10, 20, 30, 40)
do
  local r, g, b, a = img:get(0, 0)
  check("set/get red with default alpha", r == 255 and g == 0 and b == 0 and a == 255)
  local r2, g2, b2, a2 = img:get(3, 2)
  check("set/get with explicit alpha", r2 == 10 and g2 == 20 and b2 == 30 and a2 == 40)
end

-- out-of-bounds access is safe.
check("out-of-bounds get returns zeros", (select(1, img:get(99, 99))) == 0)

-- PNG save -> load roundtrip preserves dimensions and pixels (PNG is lossless).
local tmpPng = os.tmpname() .. ".png"
local okPng, perr = img:savePNG(tmpPng)
check("savePNG succeeds", okPng == true and perr == nil)

local loaded, lerr = image.load(tmpPng)
check("load succeeds", loaded ~= nil and lerr == nil)
if loaded then
  check("PNG roundtrip preserves dimensions", loaded.width == 4 and loaded.height == 3)
  local r, g, b = loaded:get(0, 0)
  check("PNG roundtrip preserves red pixel", r == 255 and g == 0 and b == 0)
  local r2, g2, b2, a2 = loaded:get(3, 2)
  check("PNG roundtrip preserves RGBA pixel", r2 == 10 and g2 == 20 and b2 == 30 and a2 == 40)
end
os.remove(tmpPng)

-- JPEG save succeeds and produces a loadable file (lossy: don't assert pixels).
local tmpJpg = os.tmpname() .. ".jpg"
local okJpg = img:saveJPG(tmpJpg, 85)
check("saveJPG succeeds", okJpg == true)
local jloaded = image.load(tmpJpg)
check("saved JPEG reloads with same dimensions",
  jloaded ~= nil and jloaded.width == 4 and jloaded.height == 3)
os.remove(tmpJpg)

-- load of a non-image returns an error rather than crashing.
do
  local bad, berr = image.load("/nonexistent/path/to/nothing.png")
  check("load of missing file returns an error", bad == nil and berr ~= nil)
end

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all image tests passed")
