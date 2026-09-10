import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { docker, listen, request, root, run } from "./admin-bigbang-fixture.mjs";

/** IngressFixture는 정본 도구를 복사하고 테스트 주소만 바꿔 실제 Nginx reload를 검증합니다. */
export class IngressFixture {
  constructor(fixture) { this.fixture = fixture; this.directory = path.join(fixture.directory, "deploy-tree"); this.shortCalls = 0; }
  async start(backendURL) {
    const f = this.fixture;
    const gateway = new URL(f.holoURL).hostname; this.address = gateway.replace(/\.1$/, ".10");
    const short = http.createServer((_req, res) => { this.shortCalls++; res.writeHead(302, { Location: "https://short.fixture.invalid/confirmed" }); res.end(); });
    f.servers.push(short); await listen(short, { host: gateway, port: 0 }); this.shortURL = `http://${gateway}:${short.address().port}`;
    for (const file of ["scripts/deploy/admin-dashboard-maintenance.sh", "scripts/deploy/lib/public-bind-mounts.sh", "scripts/deploy/purge-admin-dashboard-sessions.sh"]) {
      await fs.mkdir(path.dirname(path.join(this.directory, file)), { recursive: true }); await fs.copyFile(path.join(root, file), path.join(this.directory, file));
    }
    this.config = path.join(f.directory, "admin-dashboard-ingress.conf");
    await this.target(backendURL); await this.render();
    const source = await fs.readFile(path.join(root, "deploy/compose/docker-compose.live-compat.yml"), "utf8");
    const image = source.match(/nginx:[^\s}]+@sha256:[a-f0-9]{64}/)?.[0]; assert(image, "pinned Nginx image required");
    this.container = await f.container("ingress", ["--ip", this.address, "--read-only", "--user", "101:101", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--tmpfs", "/tmp:size=16m,uid=101,gid=101", "--tmpfs", "/var/run:size=1m,uid=101,gid=101", "--tmpfs", "/var/cache/nginx:size=16m,uid=101,gid=101", "--mount", `type=bind,src=${this.config},dst=/etc/nginx/admin-dashboard-ingress.conf,readonly`, image, "nginx", "-c", "/etc/nginx/admin-dashboard-ingress.conf", "-g", "daemon off;"]);
    this.url = `http://${this.address}:30191`; this.shortOrigin = `http://${this.address}:30192`;
    return this;
  }
  async target(backendURL) {
    let template = await fs.readFile(path.join(root, "deploy/nginx/admin-dashboard-ingress.conf.template"), "utf8");
    template = template.replaceAll("http://127.0.0.1:30190", backendURL).replaceAll("http://127.0.0.1:30101", this.shortURL).replaceAll("100.100.1.5", new URL(this.fixture.holoURL).hostname);
    const file = path.join(this.directory, "deploy/nginx/admin-dashboard-ingress.conf.template"); await fs.mkdir(path.dirname(file), { recursive: true }); await fs.writeFile(file, template);
  }
  async render() {
    await run("bash", ["-c", 'source "$1"; prepare_admin_dashboard_ingress_bind_mount "$2"', "fixture-render", path.join(this.directory, "scripts/deploy/lib/public-bind-mounts.sh"), this.directory], { env: { ...process.env, HOLOLIVE_BOT_PORT_BIND_IP: this.address, HOLOLIVE_INGRESS_CONF: this.config }, timeout: 5000 });
  }
  async maintenance(action) {
    const result = await run("bash", [path.join(this.directory, "scripts/deploy/admin-dashboard-maintenance.sh"), action, this.container, this.config], { timeout: 45000 });
    assert(result.stdout.includes(action === "fence" ? "administrator_ingress=503" : "administrator_ingress=200"));
    return result.stdout.trim();
  }
  async shortlink() {
    const before = this.shortCalls, response = await request(this.shortOrigin, "GET", "/l/fixture");
    assert.equal(response.status, 302); assert.equal(this.shortCalls, before + 1); return response.status;
  }
  async purge(stoppedBFF) {
    const result = await run("bash", [path.join(this.directory, "scripts/deploy/purge-admin-dashboard-sessions.sh"), this.fixture.valkey, stoppedBFF, this.container], { timeout: 40000 });
    assert(result.stdout.startsWith("admin_session_purge=completed prefix=session:admin:")); return result.stdout.trim();
  }
  async remove(container) {
    assert.equal(await docker(["inspect", container, "--format", '{{index .Config.Labels "io.hololive.admin-fixture"}}']), this.fixture.id);
    assert.equal(await docker(["inspect", container, "--format", '{{.State.Status}}']), "exited");
    await docker(["rm", container]); this.fixture.containers = this.fixture.containers.filter(id => id !== container);
  }
}
