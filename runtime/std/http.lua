-- Copyright (c) 2026. Asrian Putra. All rights reserved.
-- Use of this source code is governed by an MIT license
-- that can be found in the LICENSE file.

-- Module: std/http (a minimal HTTP/1.1 server and client for Novel)
--
-- Purpose:
--   Plain HTTP over std/net. The server spawns a green thread per connection
--   (real concurrency on the async scheduler), parses the request line and
--   headers, and calls a Novel handler `fn(req)` that returns a response. The
--   client does a one-shot GET. Imported with `import std.http`; calls lower to
--   dot-calls (http.serve(...)), and req/resp are plain tables accessed with
--   field syntax (req.method, req.path, resp.status).
--
-- Key Components:
--   - serve(host, port, handler): accept loop; handler(req) -> response table
--   - response(status, body, headers?): build a response table
--   - get(url): one-shot GET, returns (response, err)
--   - request shape: { method, path, version, headers = {lower=val}, body }
--   - response shape: { status, body, headers = {name=val} }
--
-- Security:
--   No TLS, no auth, no request-size limits beyond a header cap. Do not expose
--   this directly to untrusted networks for anything sensitive; bind to
--   127.0.0.1 unless off-host access is intended, and validate handler input.

local novel = require("novel")
local net = require("std/net")
local fs = require("std/fs")
local io_mod = require("std/io")

local http = {}

-- Standard reason phrases for the status codes a handler is likely to use.
local REASON = {
  [200] = "OK", [201] = "Created", [204] = "No Content",
  [301] = "Moved Permanently", [302] = "Found", [304] = "Not Modified",
  [400] = "Bad Request", [401] = "Unauthorized", [403] = "Forbidden",
  [404] = "Not Found", [405] = "Method Not Allowed",
  [500] = "Internal Server Error", [503] = "Service Unavailable",
}

-- response builds a response table. body defaults to "", status to 200, and
-- headers to an empty table the server fills in (Content-Length, etc.).
function http.response(status, body, headers)
  return {
    status = status or 200,
    body = body or "",
    headers = headers or {},
  }
end

-- MIME types table mapping file extension (in lowercase) to Content-Type.
local MIME_TYPES = {
  html = "text/html; charset=utf-8",
  htm  = "text/html; charset=utf-8",
  css  = "text/css; charset=utf-8",
  js   = "application/javascript; charset=utf-8",
  mjs  = "application/javascript; charset=utf-8",
  json = "application/json; charset=utf-8",
  txt  = "text/plain; charset=utf-8",
  md   = "text/markdown; charset=utf-8",
  nv   = "text/plain; charset=utf-8",
  png  = "image/png",
  jpg  = "image/jpeg",
  jpeg = "image/jpeg",
  gif  = "image/gif",
  svg  = "image/svg+xml",
  ico  = "image/x-icon",
  wasm = "application/wasm",
  pdf  = "application/pdf",
  xml  = "application/xml; charset=utf-8",
}

local function guessMime(path)
  local ext = path:match("%.([^%.]+)$")
  if ext then
    return MIME_TYPES[ext:lower()] or "application/octet-stream"
  end
  return "application/octet-stream"
end

-- serveFile reads a file and returns an HTTP response table.
-- If the file does not exist or is a directory, it returns a 404 response.
--
-- params:
--   path: path to the file to serve
-- returns:
--   response: table representing the HTTP response
function http.serveFile(path)
  local info, err = fs.stat(path)
  if info == nil or info.is_dir then
    return http.response(404, "Not Found\n")
  end
  local data, rerr = io_mod.readFile(path)
  if data == nil then
    return http.response(404, "Not Found\n")
  end
  local ct = guessMime(path)
  return http.response(200, data, { ["Content-Type"] = ct })
end

-- Cap on the size of the request head (request line + headers) we will buffer,
-- so a misbehaving client cannot make the server allocate without bound.
local MAX_HEAD = 64 * 1024

-- readHead reads from conn until the end-of-headers marker (CRLF CRLF) or the
-- size cap. Returns (head, leftover) where leftover is any body bytes already
-- read, or (nil, nil, err) on failure.
local function readHead(conn)
  local buf = ""
  while true do
    local sep = buf:find("\r\n\r\n", 1, true)
    if sep then
      return buf:sub(1, sep - 1), buf:sub(sep + 4), nil
    end
    if #buf > MAX_HEAD then
      return nil, nil, novel.error("request head too large")
    end
    local data, err = conn:read(4096)
    if err ~= nil then return nil, nil, err end
    if data == "" then
      -- Connection closed before headers completed.
      if #buf == 0 then return nil, nil, nil end
      return nil, nil, novel.error("incomplete request head")
    end
    buf = buf .. data
  end
end

-- parseRequest turns a raw head into a request table. Returns (req, nil) or
-- (nil, err) on a malformed request line.
local function parseRequest(head, body)
  local lines = {}
  for line in (head .. "\r\n"):gmatch("(.-)\r\n") do
    lines[#lines + 1] = line
  end
  local reqline = lines[1] or ""
  local method, path, version = reqline:match("^(%S+)%s+(%S+)%s+(%S+)$")
  if method == nil then
    return nil, novel.error("malformed request line: " .. reqline)
  end
  local headers = {}
  for i = 2, #lines do
    local k, v = lines[i]:match("^([^:]+):%s*(.*)$")
    if k then
      headers[k:lower()] = v
    end
  end
  return {
    method = method,
    path = path,
    version = version,
    headers = headers,
    body = body or "",
  }, nil
end

-- writeResponse serializes resp and sends it on conn. It always sets
-- Content-Length and a default Content-Type, and closes with Connection: close
-- (one request per connection keeps the server simple and predictable).
local function writeResponse(conn, resp)
  local status = resp.status or 200
  local body = resp.body or ""
  local reason = REASON[status] or "Status"
  local lines = { string.format("HTTP/1.1 %d %s", status, reason) }
  local headers = resp.headers or {}
  local hasType = false
  for k, v in pairs(headers) do
    if k:lower() == "content-type" then hasType = true end
    lines[#lines + 1] = k .. ": " .. tostring(v)
  end
  if not hasType then
    lines[#lines + 1] = "Content-Type: text/plain; charset=utf-8"
  end
  lines[#lines + 1] = "Content-Length: " .. #body
  lines[#lines + 1] = "Connection: close"
  lines[#lines + 1] = ""
  lines[#lines + 1] = body
  return conn:write(table.concat(lines, "\r\n"))
end
http.writeResponse = writeResponse
http.parseRequest = parseRequest
http._writeResponse = writeResponse
http._parseRequest = parseRequest

-- readBody ensures the request body is fully read when the client declared a
-- Content-Length. `have` is whatever body bytes arrived with the head.
local function readBody(conn, headers, have)
  local len = tonumber(headers["content-length"] or "0") or 0
  local body = have or ""
  while #body < len do
    local data, err = conn:read(len - #body)
    if err ~= nil then return body, err end
    if data == "" then break end
    body = body .. data
  end
  return body, nil
end

-- handleConn serves a single connection: parse the request, run the handler,
-- write the response, close. Runs as its own green thread so a slow handler or
-- client never blocks the accept loop or other connections.
local function handleConn(conn, handler)
  local req
  local ok, errOrResp = pcall(function()
    local head, leftover, herr = readHead(conn)
    if head == nil then
      if herr ~= nil then
        writeResponse(conn, http.response(400, "Bad Request\n"))
      end
      return nil
    end
    local perr
    req, perr = parseRequest(head, leftover)
    if req == nil then
      writeResponse(conn, http.response(400, "Bad Request\n"))
      return nil
    end
    req.conn = conn
    req.hijacked = false
    req.body = readBody(conn, req.headers, req.body)
    return handler(req)
  end)

  if req and req.hijacked then
    return
  end

  if not ok then
    -- A handler error becomes a 500 rather than crashing the green thread.
    writeResponse(conn, http.response(500, "Internal Server Error\n"))
  elseif errOrResp ~= nil then
    local resp = errOrResp
    if type(resp) ~= "table" or resp.status == nil then
      -- Handler returned something that isn't a response table; coerce to 200
      -- with the value stringified, so simple handlers can `return "hi"`.
      resp = http.response(200, tostring(resp))
    end
    writeResponse(conn, resp)
  end
  conn:close()
end

-- serve listens on host:port and serves requests until the listener is closed,
-- spawning a green thread per connection. handler(req) returns a response table
-- (from http.response) or a string (sent as a 200 text body). Returns
-- (listener, nil) so the caller can later listener:close(); or (nil, err) if
-- the bind fails. serve itself does not block: it spawns the accept loop.
function http.serve(host, port, handler)
  local ln, err = net.listenTCP(host, port)
  if ln == nil then return nil, err end
  novel.spawn(function()
    while not ln.closed do
      local conn, aerr = ln:accept()
      if conn == nil then
        if ln.closed then break end
        -- transient accept error: yield and keep serving
        novel.sleep(0)
      else
        novel.spawn(function() handleConn(conn, handler) end)
      end
    end
  end)
  return ln, nil
end

-- parseURL splits "http://host[:port]/path" into (host, port, path). Only plain
-- http is supported (no TLS). Returns (host, port, path) or (nil, err).
local function parseURL(url)
  local rest = url:match("^http://(.+)$")
  if rest == nil then
    return nil, nil, nil, novel.error("only http:// URLs are supported: " .. tostring(url))
  end
  local hostport, path = rest:match("^([^/]+)(/.*)$")
  if hostport == nil then
    hostport = rest
    path = "/"
  end
  local host, port = hostport:match("^([^:]+):(%d+)$")
  if host == nil then
    host = hostport
    port = 80
  else
    port = tonumber(port)
  end
  return host, port, path, nil
end

-- get performs a one-shot HTTP GET and returns (response, nil) or (nil, err).
-- The response table is { status, headers = {lower=val}, body }. Parks the
-- calling green thread on the network I/O like any other net operation.
function http.get(url)
  local host, port, path, uerr = parseURL(url)
  if host == nil then return nil, uerr end

  local conn, derr = net.dialTCP(host, port)
  if conn == nil then return nil, derr end

  local reqText = table.concat({
    "GET " .. path .. " HTTP/1.1",
    "Host: " .. host,
    "User-Agent: novel-http/0.1",
    "Accept: */*",
    "Connection: close",
    "", "",
  }, "\r\n")
  local ok, werr = conn:write(reqText)
  if not ok then conn:close(); return nil, werr end

  -- Read the whole response (Connection: close => read to EOF).
  local raw = ""
  while true do
    local data, rerr = conn:read(4096)
    if rerr ~= nil then conn:close(); return nil, rerr end
    if data == "" then break end
    raw = raw .. data
  end
  conn:close()

  local sep = raw:find("\r\n\r\n", 1, true)
  if sep == nil then
    return nil, novel.error("malformed HTTP response")
  end
  local head = raw:sub(1, sep - 1)
  local body = raw:sub(sep + 4)
  local lines = {}
  for line in (head .. "\r\n"):gmatch("(.-)\r\n") do
    lines[#lines + 1] = line
  end
  local status = tonumber((lines[1] or ""):match("^HTTP/%d%.%d%s+(%d+)")) or 0
  local headers = {}
  for i = 2, #lines do
    local k, v = lines[i]:match("^([^:]+):%s*(.*)$")
    if k then headers[k:lower()] = v end
  end
  return { status = status, headers = headers, body = body }, nil
end

return http
