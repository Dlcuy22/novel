-- Integration test for std/net and std/http over the async scheduler.
-- Run with: luajit runtime/test_net.lua  (or `make test-net`).
-- Exits non-zero on failure.
--
-- Each case spins up a server and a client as green threads in one novel.run(),
-- exercising the poll(2) reactor end to end: non-blocking accept/connect, reads
-- and writes that park on fd readiness, and per-connection concurrency.

package.path = (arg[0]:match("(.*/)") or "./") .. "?.lua;" .. package.path

local novel = require("novel")
local net = require("std/net")
local http = require("std/http")

local failures = 0
local function check(name, cond)
  if cond then
    print("ok   - " .. name)
  else
    print("FAIL - " .. name)
    failures = failures + 1
  end
end

-- Pick high, unlikely-to-clash loopback ports for the test cases.
local TCP_PORT  = 47931
local UDP_PORT  = 47932
local CONC_PORT = 47933
local HTTP_PORT = 47934

-- 1) TCP echo: client sends a line, server upper-cases it back.
print("Starting test 1 (TCP echo)"); io.flush()
do
  local serverGot, clientGot
  novel.spawn(function()
    local ln = assert(net.listenTCP("127.0.0.1", TCP_PORT))
    local conn = ln:accept()
    serverGot = conn:readLine()
    conn:write(string.upper(serverGot) .. "\n")
    conn:close()
    ln:close()
  end)
  novel.spawn(function()
    novel.sleep(0.01)
    local conn = assert(net.dialTCP("127.0.0.1", TCP_PORT))
    conn:write("hello async\n")
    clientGot = conn:readLine()
    conn:close()
  end)
  novel.run()
  check("tcp echo: server received the line", serverGot == "hello async")
  check("tcp echo: client got the uppercased reply", clientGot == "HELLO ASYNC")
end

-- 2) UDP datagram echo.
print("Starting test 2 (UDP echo)"); io.flush()
do
  local udpGot, reply
  novel.spawn(function()
    local u = assert(net.bindUDP("127.0.0.1", UDP_PORT))
    local data, ip, port = u:recvfrom()
    udpGot = data
    u:sendto("pong:" .. data, ip, port)
    u:close()
  end)
  novel.spawn(function()
    novel.sleep(0.01)
    local c = assert(net.bindUDP("127.0.0.1", 0))
    c:sendto("ping", "127.0.0.1", UDP_PORT)
    reply = c:recvfrom()
    c:close()
  end)
  novel.run()
  check("udp: server received the datagram", udpGot == "ping")
  check("udp: client got the reply", reply == "pong:ping")
end

-- 3) Concurrency: three simultaneous connections, each handled in its own
--    spawned green thread, all complete correctly.
print("Starting test 3 (Concurrency)"); io.flush()
do
  local results = {}
  novel.spawn(function()
    local ln = assert(net.listenTCP("127.0.0.1", CONC_PORT))
    local handled = 0
    while handled < 3 do
      local conn = ln:accept()
      handled = handled + 1
      novel.spawn(function()
        local line = conn:readLine()
        conn:write("ack " .. line .. "\n")
        conn:close()
      end)
    end
    ln:close()
  end)
  for i = 1, 3 do
    novel.spawn(function()
      novel.sleep(0.01)
      local c = assert(net.dialTCP("127.0.0.1", CONC_PORT))
      c:write("client" .. i .. "\n")
      results[i] = c:readLine()
      c:close()
    end)
  end
  novel.run()
  local ok = true
  for i = 1, 3 do
    if results[i] ~= "ack client" .. i then ok = false end
  end
  check("concurrent: all three connections acked", ok)
end

-- 4) HTTP server + client GET, with status and body.
print("Starting test 4 (HTTP GET)"); io.flush()
do
  local status, body, notFound
  local ln = assert(http.serve("127.0.0.1", HTTP_PORT, function(req)
    if req.path == "/hello" then
      return http.response(200, "hi " .. (req.headers["x-name"] or "anon"))
    end
    return http.response(404, "nope\n")
  end))
  novel.spawn(function()
    novel.sleep(0.02)
    local resp = assert(http.get("http://127.0.0.1:" .. HTTP_PORT .. "/hello"))
    status, body = resp.status, resp.body
    local r2 = assert(http.get("http://127.0.0.1:" .. HTTP_PORT .. "/missing"))
    notFound = r2.status
    ln:close()
  end)
  novel.run()
  check("http: 200 status", status == 200)
  check("http: response body", body == "hi anon")
  check("http: 404 for unknown path", notFound == 404)
end

-- 5) HTTP hijacking, serveFile, and public parsers.
print("Starting test 5 (HTTP hijacking/serveFile)"); io.flush()
do
  local hijackedResult, fileResponse, parserOk
  local hijackedPort = HTTP_PORT + 1
  local fs = require("std/fs")
  local io_mod = require("std/io")

  -- Create a temporary test file.
  local tempFile = "temp_test_serve_file.txt"
  fs.remove(tempFile)
  io_mod.writeFile(tempFile, "static content test")

  local ln = assert(http.serve("127.0.0.1", hijackedPort, function(req)
    if req.path == "/hijack" then
      req.hijacked = true
      req.conn:write("HTTP/1.1 200 OK\r\nConnection: close\r\n\r\nhijacked response")
      req.conn:close()
      return nil
    elseif req.path == "/file" then
      return http.serveFile(tempFile)
    end
    return http.response(404, "nope\n")
  end))

  novel.spawn(function()
    novel.sleep(0.02)
    -- Test hijack
    local resp1 = assert(http.get("http://127.0.0.1:" .. hijackedPort .. "/hijack"))
    hijackedResult = resp1.body

    -- Test serveFile
    local resp2 = assert(http.get("http://127.0.0.1:" .. hijackedPort .. "/file"))
    fileResponse = resp2.body

    -- Test public parsers
    local rawReq = "GET /index HTTP/1.1\r\nHost: localhost\r\nX-Test: true\r\n\r\n"
    local parsed, perr = http.parseRequest(rawReq, "")
    if parsed and parsed.method == "GET" and parsed.headers["x-test"] == "true" then
      parserOk = true
    end

    fs.remove(tempFile)
    ln:close()
  end)

  novel.run()
  check("http hijack works", hijackedResult == "hijacked response")
  check("http serveFile works", fileResponse == "static content test")
  check("http public parseRequest works", parserOk == true)
end

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all net/http tests passed")
