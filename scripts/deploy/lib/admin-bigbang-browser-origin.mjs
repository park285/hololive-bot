import fs from "node:fs/promises";
import http from "node:http";
import https from "node:https";
import path from "node:path";
import { listen, run } from "./admin-bigbang-fixture.mjs";

/** BrowserOrigin은 실제 BFF의 HTTP/WS와 production assets를 같은 격리 HTTPS origin으로 전달합니다. */
export class BrowserOrigin {
  constructor(fixture) { this.fixture = fixture; this.agent = new http.Agent({ keepAlive: true }); this.sockets = new Set(); this.scriptTransfers = []; this.errors = []; }
  async start() {
    const f = this.fixture; await f.prepare();
    const key = path.join(f.directory, "fixture-tls.key"), cert = path.join(f.directory, "fixture-tls.crt");
    await run("openssl", ["req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes", "-days", "1", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1", "-keyout", key, "-out", cert]);
    await fs.chmod(key, 0o600);
    this.server = https.createServer({ key: await fs.readFile(key), cert: await fs.readFile(cert) }, (req, res) => {
      const target = new URL(f.bffURL);
      const pathname = new URL(req.url, "https://fixture.invalid").pathname;
      const file = pathname === "/" || pathname === "/login" || pathname.startsWith("/dashboard") ? "index.html" : pathname.slice(1);
      const asset = req.method === "GET" ? this.assets?.get(file) : undefined;
      const upstream = http.request({ agent: this.agent, hostname: target.hostname, port: target.port, method: req.method, path: req.url, headers: { ...req.headers, "x-forwarded-proto": "https" }, timeout: 15000 }, response => {
        if (asset) {
          const headers = { ...response.headers, "content-type": asset.type, "content-length": asset.body.length, "cache-control": "no-store" };
          for (const name of ["content-encoding", "transfer-encoding", "etag"]) delete headers[name];
          response.resume(); res.writeHead(200, headers); res.end(asset.body); return;
        }
        res.writeHead(response.statusCode, response.headers);
        let bytes = 0;
        response.on("data", data => { bytes += data.length; });
        res.on("finish", () => { if (req.url.endsWith(".js")) this.scriptTransfers.push({ path: req.url, status: response.statusCode, bytes, encoding: response.headers["content-encoding"] ?? "identity" }); });
        res.on("close", () => { if (req.url.endsWith(".js") && !res.writableFinished) this.errors.push("incomplete_script_transfer"); });
        response.pipe(res);
      });
      upstream.on("timeout", () => upstream.destroy(new Error("fixture upstream timeout")));
      upstream.on("error", () => { this.errors.push("upstream_transport"); if (!res.headersSent) res.writeHead(502); res.end(); });
      req.on("aborted", () => upstream.destroy()); req.pipe(upstream);
    });
    this.server.on("connection", socket => { this.sockets.add(socket); socket.on("error", () => {}); socket.on("close", () => this.sockets.delete(socket)); });
    this.server.on("upgrade", (req, socket, head) => {
      const target = new URL(f.bffURL);
      const upstream = http.request({ agent: this.agent, hostname: target.hostname, port: target.port, method: "GET", path: req.url, headers: { ...req.headers, "x-forwarded-proto": "https" } });
      upstream.on("upgrade", (response, peer, upstreamHead) => {
        this.sockets.add(peer); peer.on("error", () => {}); peer.on("close", () => this.sockets.delete(peer));
        socket.write(`HTTP/1.1 101 Switching Protocols\r\n${response.rawHeaders.reduce((text, value, index, values) => index % 2 ? text : text + value + ': ' + values[index + 1] + '\r\n', '')}\r\n`);
        if (upstreamHead.length) socket.write(upstreamHead); if (head.length) peer.write(head);
        socket.pipe(peer).pipe(socket); socket.on("close", () => peer.destroy());
      });
      upstream.on("response", response => { this.errors.push(`ws_rejected_${response.statusCode}`); socket.end(`HTTP/1.1 ${response.statusCode} Denied\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`); response.resume(); });
      upstream.on("error", () => { this.errors.push("ws_upstream_transport"); socket.destroy(); }); upstream.end();
    });
    await listen(this.server, { host: "127.0.0.1", port: 0 });
    this.url = `https://127.0.0.1:${this.server.address().port}`; f.origin = this.url; return this;
  }
  async close() {
    // 다음 fixture가 같은 bridge IP를 받아도 이전 BFF의 keep-alive 연결을 재사용하지 않습니다.
    this.agent.destroy();
    for (const socket of this.sockets) socket.destroy();
    if (this.server) await new Promise(resolve => this.server.close(resolve));
  }
}
