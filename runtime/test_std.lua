-- Test suite for the Novel standard library Lua modules.
-- Run with: luajit runtime/test_std.lua
-- Exits non-zero on failure so `make test-std` can gate CI.

package.path = (arg[0]:match("(.*/)") or "./") .. "?.lua;" .. package.path

local math_m = require("std/math")
local str = require("std/str")
local rand = require("std/rand")
local crypto = require("std/crypto")
local os_m = require("std/os")
local ffi_m = require("std/ffi")
local json = require("std/json")
local encoding = require("std/encoding")

local failures = 0
local function check(name, cond)
  if cond then
    print("ok   - " .. name)
  else
    print("FAIL - " .. name)
    failures = failures + 1
  end
end

-- std/math
check("math.sqrt", math_m.sqrt(16.0) == 4.0)
check("math.pow", math_m.pow(2.0, 10.0) == 1024.0)
check("math.floor", math_m.floor(3.7) == 3)
check("math.ceil", math_m.ceil(3.2) == 4)
check("math.round half-up", math_m.round(2.5) == 3)
check("math.abs", math_m.abs(-5.0) == 5.0)
check("math.min/max", math_m.min(3, 7) == 3 and math_m.max(3, 7) == 7)
check("math.clamp", math_m.clamp(12, 0, 10) == 10 and math_m.clamp(-1, 0, 10) == 0)

-- std/str
check("str.upper/lower", str.upper("aB") == "AB" and str.lower("aB") == "ab")
check("str.trim", str.trim("  hi  ") == "hi")
check("str.contains literal", str.contains("a.b.c", ".") and not str.contains("abc", "."))
check("str.indexOf", str.indexOf("hello", "l") == 3 and str.indexOf("hello", "z") == 0)
check("str.startsWith/endsWith", str.startsWith("novel", "nov") and str.endsWith("novel", "vel"))

local parts = str.split("a,b,c", ",")
check("str.split count", #parts == 3 and parts[1] == "a" and parts[3] == "c")
check("str.join", str.join(parts, "-") == "a-b-c")
check("str.replace all", str.replace("a.b.c", ".", "/") == "a/b/c")
check("str.substring 1-based", str.substring("novel", 2, 4) == "ove")

-- std/rand
rand.seed(42)
local n = rand.between(1, 6)
check("rand.between in range", n >= 1 and n <= 6)
check("rand.intn in range", (function()
  for _ = 1, 100 do
    local v = rand.intn(10)
    if v < 0 or v > 9 then return false end
  end
  return true
end)())
local f = rand.float()
check("rand.float in [0,1)", f >= 0.0 and f < 1.0)
local b = rand.bytes(16)
check("rand.bytes length", b ~= nil and #b == 16)
-- deterministic seed -> reproducible sequence
rand.seed(7)
local a1 = rand.intn(1000000)
rand.seed(7)
local a2 = rand.intn(1000000)
check("rand.seed reproducible", a1 == a2)

-- std/crypto: SHA-256 against the standard NIST test vectors.
check("sha256 empty",
  crypto.sha256("") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
check("sha256 abc",
  crypto.sha256("abc") == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
check("sha256 fox",
  crypto.sha256("The quick brown fox jumps over the lazy dog")
    == "d7a8fbb307d7809469ca9abcb0082e4f8d5651e46d3cdb762d02d0bf37c9e592")
check("sha256 multi-block (million a)",
  crypto.sha256(string.rep("a", 1000000))
    == "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0")

-- std/crypto: FNV-1a against independently computed values.
check("fnv1a empty", crypto.fnv1a("") == 2166136261)
check("fnv1a abc", crypto.fnv1a("abc") == 440920331)
check("fnv1a hello", crypto.fnv1a("hello") == 1335831723)

-- std/os
check("os.platform is a known name", (function()
  local p = os_m.platform()
  return p == "linux" or p == "macos" or p == "windows" or p == "bsd" or p == "other"
end)())
check("os.isUnix or isWindows", os_m.isUnix() or os_m.isWindows())
check("os.arch nonempty", type(os_m.arch()) == "string" and #os_m.arch() > 0)
check("os.args is a table", type(os_m.args()) == "table")

-- std/ffi: call a libc function through the flat fn() helper.
ffi_m.cdef("size_t strlen(const char *s); double pow(double, double);")
local strlen = ffi_m.fn(ffi_m.C, "strlen")
check("ffi.fn calls strlen", tonumber(strlen("novel")) == 5)
check("ffi.os nonempty", type(ffi_m.os()) == "string" and #ffi_m.os() > 0)

-- std/json: encode/decode roundtrip and edge cases.
check("json encode array", json.encode({1, 2, 3}) == "[1,2,3]")
check("json encode null", json.encode(json.null) == "null")
check("json encode escapes", json.encode("a\"b") == '"a\\"b"')
do
  local v = json.decode('{"k": [true, null, 2.5], "s": "hi"}')
  check("json decode object", v ~= nil and v.s == "hi")
  check("json decode array elem", v ~= nil and v.k[1] == true)
  check("json decode null sentinel", v ~= nil and v.k[2] == json.null)
  check("json decode number", v ~= nil and v.k[3] == 2.5)
end
do
  local roundtrip = json.decode(json.encode({ x = 42, y = "two" }))
  check("json roundtrip", roundtrip.x == 42 and roundtrip.y == "two")
  local bad = json.decode("{nope}")
  check("json decode rejects bad input", bad == nil)
end

-- std/encoding: base64 (RFC 4648 vectors) and hex.
check("base64 foobar", encoding.base64Encode("foobar") == "Zm9vYmFy")
check("base64 padding", encoding.base64Encode("fo") == "Zm8=")
check("base64 decode", encoding.base64Decode("Zm9vYmFy") == "foobar")
do
  local bin = string.char(0, 255, 16, 128, 1)
  check("base64 binary roundtrip", encoding.base64Decode(encoding.base64Encode(bin)) == bin)
end
check("hex encode", encoding.hexEncode("ABC") == "414243")
check("hex decode", encoding.hexDecode("414243") == "ABC")
check("hex rejects odd length", (select(1, encoding.hexDecode("abc"))) == nil)
check("hex rejects non-hex", (select(1, encoding.hexDecode("zz"))) == nil)

-- std/fs
local fs = require("std/fs")
local io_mod = require("std/io")
local test_dir = "test_fs_dir"
local test_file = test_dir .. "/test.txt"

-- clean up any leftover from failed tests
fs.removeAll(test_dir)

check("fs.exists not present", not fs.exists(test_dir))

-- mkdir
local ok, err = fs.mkdir(test_dir)
check("fs.mkdir success", ok and fs.exists(test_dir))

-- stat on directory
local info, serr = fs.stat(test_dir)
check("fs.stat directory", info ~= nil and info.is_dir == true and info.name == "test_fs_dir")

-- write a file
local fok, ferr = io_mod.writeFile(test_file, "hello filesystem")
check("writeFile success", fok)

check("fs.exists file", fs.exists(test_file))

-- stat on file
local file_info, f_serr = fs.stat(test_file)
check("fs.stat file", file_info ~= nil and file_info.is_dir == false and file_info.size == 16 and file_info.name == "test.txt" and type(file_info.mod_time) == "number")

-- readDir
local files, rderr = fs.readDir(test_dir)
check("fs.readDir contents", files ~= nil and #files == 1 and files[1] == "test.txt")

-- rename
local renamed_file = test_dir .. "/renamed.txt"
local rnok, rnerr = fs.rename(test_file, renamed_file)
check("fs.rename success", rnok and fs.exists(renamed_file) and not fs.exists(test_file))

-- remove file
local rmok, rmerr = fs.remove(renamed_file)
check("fs.remove file", rmok and not fs.exists(renamed_file))

-- removeAll directory
local rmall_ok, rmall_err = fs.removeAll(test_dir)
check("fs.removeAll success", rmall_ok and not fs.exists(test_dir))

-- std/exec
local exec = require("std/exec")
local novel = require("novel")

novel.spawn(function()
  -- Test output & run (cross-platform using sh/cmd)
  local echoPath, echoArgs
  if os_m.isWindows() then
    echoPath = "cmd"
    echoArgs = { "/c", "echo hello" }
  else
    echoPath = "echo"
    echoArgs = { "hello" }
  end

  local echoCmd = exec.command(echoPath, echoArgs)
  local out, err = echoCmd:output()
  check("exec.command echo output", out ~= nil and err == nil and str.trim(out) == "hello")

  local code, rerr = echoCmd:run()
  check("exec.command echo run code", code == 0 and rerr == nil)

  -- Test bad binary name
  local badCmd = exec.command("nonexistent_binary_name_xyz")
  local bout, berr = badCmd:output()
  check("exec nonexistent command returns error", bout == nil and berr ~= nil)

  -- Test combinedOutput
  local shPath, shArgs
  if os_m.isWindows() then
    shPath = "cmd"
    shArgs = { "/c", "echo stdout && echo stderr 1>&2" }
  else
    shPath = "sh"
    shArgs = { "-c", "echo stdout; echo stderr >&2" }
  end

  local combCmd = exec.command(shPath, shArgs)
  local cout, cerr = combCmd:combinedOutput()
  check("exec.command combinedOutput captures both", cout ~= nil and cerr == nil and str.contains(cout, "stdout") and str.contains(cout, "stderr"))
end)
novel.run()

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all stdlib tests passed")
