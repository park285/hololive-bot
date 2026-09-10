import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { CLIENT_GENERATION } from "../../../admin-dashboard/frontend/src/api/generated/generation.ts";
import fs from "node:fs/promises";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

export const root = fileURLToPath(new URL("../../../", import.meta.url));
// 격리 시험 중 다른 세션이 Docker context를 바꿔도 원격 daemon을 대상으로 삼지 않습니다.
process.env.DOCKER_HOST = "unix:///var/run/docker.sock";
delete process.env.DOCKER_CONTEXT;
export const run = promisify(execFile);
export const proxyImage = "wollomatic/socket-proxy:1.12.3@sha256:74e770f5ed3cfc9ecb6350e177d2aa55873568c85bc953079834e68607dbf71b";
export const valkeyImage = "valkey/valkey:9.1.2-alpine3.24@sha256:ccfa19b0d743e48927e1c8c14e39e0acb97b5cea347fef0bfe340247fea920cd";
export const policy = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/backend/internal/contract/docker-policy.json")));
export const generation = CLIENT_GENERATION;
export const origin = "https://admin.fixture.invalid";
export const syntheticPassword = "synthetic-fixture-password";
const syntheticValkeyPassword = "synthetic-fixture-valkey-password";
export const docker = async args => (await run("docker", args, { timeout: 45000, maxBuffer: 2 ** 20 })).stdout.trim();
export const listen = (server, address) => new Promise((resolve, reject) => { server.once("error", reject); server.listen(address, resolve); });
export const close = server => new Promise((resolve, reject) => { server.closeAllConnections(); server.close(error => error ? reject(error) : resolve()); });
export async function poll(check, label, milliseconds = 15000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) { if (await check()) return; await new Promise(resolve => setTimeout(resolve, 100)); }
  throw new Error(`fixture timeout: ${label}`);
}

/** request는 raw path를 보존하며 redirect/retry를 수행하지 않습니다. */
export function request(base, method, target, headers = {}, body, timeoutMs = 12000) {
  const address = new URL(base);
  return new Promise((resolve, reject) => {
    const req = http.request({ hostname: address.hostname, port: address.port, method, path: target, headers, timeout: timeoutMs }, res => {
      const chunks = []; let size = 0;
      res.on("data", data => { size += data.length; if (size > 8 * 1024 * 1024) res.destroy(new Error("fixture response too large")); else chunks.push(data); });
      res.on("error", reject);
      res.on("end", () => { const bytes = Buffer.concat(chunks); resolve({ status: res.statusCode, headers: res.headers, body: bytes.toString(), bytes }); });
    });
    req.on("error", reject); req.on("timeout", () => req.destroy(new Error(`fixture HTTP timeout: ${method} ${target}`)));
    req.end(body === undefined ? undefined : JSON.stringify(body));
  });
}

/** Fixture는 자기 label의 격리 network·컨테이너·가짜 upstream만 만들고 정리합니다. */
export class Fixture {
  constructor(image) {
    assert(/^sha256:[a-f0-9]{64}$/.test(image), "immutable local fixture image required");
    this.image = image; this.id = process.env.ADMIN_FIXTURE_RUN_ID;
    assert(/^admin-lab-[a-z0-9-]+$/.test(this.id), "bounded runner must supply the fixture owner");
    this.directory = path.join("/tmp", this.id); this.containers = []; this.servers = []; this.effects = []; this.dockerRequests = [];
  }
  async prepare() { await fs.mkdir(this.directory, { mode: 0o700 }); this.prepared = true; }
  async start() {
    if (!this.prepared) await this.prepare();
    const label = await docker(["image", "inspect", this.image, "--format", '{{index .Config.Labels "dev.iris.local-fixture"}}']);
    assert.equal(label, "true", "runtime/release images cannot be substituted for the local test candidate");
    this.architecture = await docker(["image", "inspect", this.image, "--format", '{{.Architecture}}']);
    assert(["amd64", "arm64"].includes(this.architecture));
    this.network = await docker(["network", "create", "--internal", "--label", `io.hololive.admin-fixture=${this.id}`, this.id]);
    const gateway = await docker(["network", "inspect", this.network, "--format", '{{(index .IPAM.Config 0).Gateway}}']);
    assert(/^172\.(?:1[6-9]|2[0-9]|3[01])\.\d+\.1$/.test(gateway), "fixture requires a private Docker bridge gateway");
    const upstream = http.createServer((req, res) => {
      res.setHeader("Content-Type", "application/json");
      if (this.holoHandler) { this.holoHandler(req, res); return; }
      res.end(JSON.stringify({ status: "ok", services: [] }));
    });
    this.servers.push(upstream); await listen(upstream, { host: gateway, port: 0 }); this.holoURL = `http://${gateway}:${upstream.address().port}`;
    const engine = http.createServer(async (req, res) => {
      if (this.dockerDelay) await new Promise(resolve => setTimeout(resolve, this.dockerDelay));
      this.dockerRequests.push({ method: req.method, path: req.url });
      const target = req.url.replace(/^\/v1\.\d+/, "");
      if (req.method === "POST") {
        this.effects.push({ method: req.method, path: req.url });
        if (this.holdDocker) { this.heldDockerResponse = res; return; }
        res.writeHead(this.dockerStatus ?? 204); res.end(); return;
      }
      if (target === "/_ping") { res.end("OK"); return; }
      res.setHeader("Content-Type", "application/json");
      if (target === "/version") { res.end(JSON.stringify({ Version: "fixture", ApiVersion: "1.52", MinAPIVersion: "1.44" })); return; }
      if (target.startsWith("/containers/json")) { res.end(JSON.stringify(policy.containers.map((entry, index) => ({ Id: `${index}`.padStart(64, "0"), Names: [`/${entry.name}`], Image: "synthetic-image", Status: "Up (healthy)", State: "running", Created: 1, Ports: [] })))); return; }
      res.writeHead(404); res.end("{}");
    });
    this.servers.push(engine); this.socket = path.join(this.directory, "fake-engine.sock"); await listen(engine, this.socket); await fs.chmod(this.socket, 0o666);
    const generated = JSON.parse(await fs.readFile(path.join(root, "deploy/compose/admin-docker-policy.generated.json")));
    await this.applyProxy(generated.services["admin-docker-proxy"].command);
    const cacheEnv = path.join(this.directory, "cache.env");
    await fs.writeFile(cacheEnv, `CACHE_PASSWORD=${syntheticValkeyPassword}\n`, { mode: 0o600 });
    this.valkey = await this.container("valkey", ["--network-alias", "valkey", "--read-only", "--tmpfs", "/data:rw,size=64m", "--env-file", cacheEnv, valkeyImage, "sh", "-c", 'exec valkey-server --save "" --appendonly no --requirepass "$CACHE_PASSWORD"']);
    await poll(async () => (await this.cache(["ping"]).catch(() => "")) === "PONG", "isolated Valkey");
    await this.materialize();
    await this.startBFF();
    return this;
  }
  async container(name, args) {
    const id = await docker(["run", "-d", "--network", this.network, "--name", `${this.id}-${name}`, "--label", `io.hololive.admin-fixture=${this.id}`, ...args]);
    assert(/^[a-f0-9]{64}$/.test(id)); this.containers.push(id); return id;
  }
  async applyProxy(command) {
    if (this.proxy) {
      assert.equal(await docker(["inspect", this.proxy, "--format", '{{index .Config.Labels "io.hololive.admin-fixture"}}']), this.id);
      await docker(["stop", "--time", "10", this.proxy]); await docker(["rm", this.proxy]);
      this.containers = this.containers.filter(id => id !== this.proxy);
    }
    this.proxy = await this.container("proxy", ["--network-alias", "proxy", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--mount", `type=bind,src=${this.socket},dst=/var/run/docker.sock,readonly`, proxyImage, ...command]);
    assert.deepEqual(JSON.parse(await docker(["inspect", this.proxy, "--format", '{{json .Config.Cmd}}'])), command);
    this.proxyURL = await this.url(this.proxy, "2375/tcp");
    await poll(async () => (await request(this.proxyURL, "GET", "/_ping").catch(() => ({ status: 0 }))).status === 200, "real socket-proxy");
  }
  async url(container, port) {
    const network = JSON.parse(await docker(["inspect", container, "--format", '{{json .NetworkSettings.Networks}}']));
    const address = network[this.id].IPAddress;
    if (!address) {
      // 격리 fixture의 synthetic 설정만 사용하므로 시작 실패 로그에 운영 값은 없습니다.
      const status = await docker(["inspect", container, "--format", '{{.State.Status}} exit={{.State.ExitCode}}']);
      const logs = await run("docker", ["logs", "--tail", "12", container]);
      throw new Error(`fixture container has no address: ${status}\n${logs.stdout}${logs.stderr}`);
    }
    assert(/^172\.(?:1[6-9]|2[0-9]|3[01])\.\d+\.\d+$/.test(address));
    return `http://${address}:${Number.parseInt(port, 10)}`;
  }
  async materialize() {
    const program = path.join(this.directory, "fixture-hash.go");
    await fs.writeFile(program, 'package main\nimport("fmt";"os";"golang.org/x/crypto/bcrypt")\nfunc main(){b,e:=bcrypt.GenerateFromPassword([]byte("synthetic-fixture-password"),10);if e!=nil{os.Exit(1)};fmt.Print(string(b))}\n', { mode: 0o600 });
    const passwordHash = (await run("go", ["run", program], { cwd: path.join(root, "admin-dashboard/backend"), timeout: 60000 })).stdout;
    this.secretGeneration = (this.secretGeneration ?? 0) + 1;
    this.secret = `synthetic-fixture-signing-key-${this.id}-${this.secretGeneration}`;
    this.secretSource = path.join(this.directory, "admin.env");
    // 재료 파일도 세대별로 새로 만들어 이전 서명 key를 복구에 재사용하지 않습니다.
    this.secretSource = path.join(this.directory, `admin-${this.secretGeneration}.env`);
    await fs.writeFile(this.secretSource, `ADMIN_PASS_HASH=${passwordHash}\nSESSION_SECRET=${this.secret}\nVALKEY_URL=:${syntheticValkeyPassword}@valkey:6379\nHOLO_BOT_API_KEY=synthetic-fixture-holo-key\n`, { mode: 0o600 });
    await run("sudo", ["-n", "chown", "0:0", this.secretSource]);
    this.secretDirectory = path.join(this.directory, "host/admin-secrets");
    await run("sudo", ["-n", "env", `ADMIN_DASHBOARD_ENV_FILE=${this.secretSource}`, `COMPOSE_ENV_FILE=${path.join(this.directory, "unused.env")}`, `ADMIN_DASHBOARD_SECRET_DIR=${this.secretDirectory}`, "HOLOLIVE_RUNTIME_GID=1002", "bash", path.join(root, "scripts/deploy/materialize-admin-dashboard-secrets.sh")]);
    for (const name of ["admin-pass-hash", "session-secret", "valkey-url", "holo-bot-api-key"]) {
      const file = path.join(this.secretDirectory, name);
      assert.equal((await run("sudo", ["-n", "stat", "-c", "%u:%g:%a", file])).stdout.trim(), "0:1002:640");
      const readable = await run("sudo", ["-n", "setpriv", "--reuid", "65534", "--regid", "65534", "--clear-groups", "test", "-r", file]).then(() => true, error => { assert.equal(error.code, 1); return false; });
      assert.equal(readable, false, "unrelated identity read a fixture secret");
    }
  }
  async startBFF() {
    const env = { PORT: "30190", ENV: "production", ADMIN_USER: "admin", FORCE_HTTPS: "true", CSRF_MODE: "enforce", WS_ORIGIN_MODE: "enforce", ALLOWED_ORIGINS: this.origin ?? origin, ALLOW_LOCALHOST_IN_PROD: this.origin?.startsWith("https://127.0.0.1:") ? "true" : "false", TRUST_FORWARDED_HEADERS: "true", TRUSTED_PROXY_CIDRS: "127.0.0.1/32", DOCKER_HOST: "tcp://proxy:2375", HOLO_ADMIN_API_URL: this.holoURL, LOG_DIR: "/tmp/logs", GOMAXPROCS: "2", GOGC: "100", GOMEMLIMIT: "96MiB", ADMIN_PASS_HASH_FILE: "/fixture/admin-pass-hash", SESSION_SECRET_FILE: "/fixture/session-secret", VALKEY_URL_FILE: "/fixture/valkey-url", HOLO_BOT_API_KEY_FILE: "/fixture/holo-bot-api-key" };
    const envFile = path.join(this.directory, "bff.env"); await fs.writeFile(envFile, Object.entries(env).map(([key, value]) => `${key}=${value}`).join("\n"), { mode: 0o600 });
    const emulator = this.architecture === "arm64" ? await this.emulator() : [];
    this.bff = await this.container("bff", ["--read-only", "--tmpfs", "/tmp:rw,size=16m", "--cpus", "2", "--memory", "128m", "--pids-limit", "128", "--group-add", "1002", "--env-file", envFile, "--mount", `type=bind,src=${this.secretDirectory},dst=/fixture,readonly`, ...emulator, this.image, ...(emulator.length ? ["/app/bin/admin-dashboard"] : [])]);
    this.bffURL = await this.url(this.bff, "30190/tcp");
    await poll(async () => (await request(this.bffURL, "GET", "/health").catch(() => ({ status: 0 }))).status === 200, "candidate BFF");
  }
  async login(base = this.bffURL) {
    const login = await request(base, "POST", "/admin/api/auth/login", { "Content-Type": "application/json", "X-Admin-Client-Generation": generation, Origin: this.origin ?? origin }, { username: "admin", password: syntheticPassword });
    const errorCode = login.status !== 200 && login.headers["content-type"]?.includes("application/json") ? JSON.parse(login.body).code : "non-JSON";
    assert.equal(login.status, 200, `synthetic login failed: type=${login.headers["content-type"]} code=${errorCode}`);
    const cookie = login.headers["set-cookie"].map(value => value.split(";")[0]).join("; ");
    const token = JSON.parse(login.body).csrf_token;
    return { Cookie: cookie, "X-CSRF-Token": token, "X-Admin-Client-Generation": generation, Origin: this.origin ?? origin, "Content-Type": "application/json" };
  }
  /** cache는 이 fixture container의 기존 설정을 내부 인증에만 사용합니다. */
  async cache(args) {
    return docker(["exec", this.valkey, "sh", "-c", 'export REDISCLI_AUTH="$CACHE_PASSWORD"; exec valkey-cli --raw "$@"', "fixture-cache", ...args]);
  }
  /** startOldBFF는 보존한 실제 arm64 artifact를 private QEMU로만 실행하며 host binfmt를 바꾸지 않습니다. */
  async startOldBFF() {
    const artifact = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/docs/bigbang/old-artifact.json")));
    this.old = await this.container("old", ["--read-only", "--tmpfs", "/tmp:rw,size=16m", "--cpus", "2", "--memory", "128m", "--pids-limit", "128", "--group-add", "1002", "--env-file", path.join(this.directory, "bff.env"), "--mount", `type=bind,src=${this.secretDirectory},dst=/fixture,readonly`, ...await this.emulator(), artifact.image, "/app/bin/admin-dashboard"]);
    this.oldURL = await this.url(this.old, "30190/tcp");
    await poll(async () => (await request(this.oldURL, "GET", "/health").catch(() => ({ status: 0 }))).status === 200, "actual old arm64 BFF", 30000);
    return artifact;
  }
  async emulator() {
    const artifact = JSON.parse(await fs.readFile(path.join(root, "admin-dashboard/docs/bigbang/old-artifact.json")));
    const { createHash } = await import("node:crypto");
    assert.equal(createHash("sha256").update(await fs.readFile(artifact.qemu.path)).digest("hex"), artifact.qemu.binary_sha256);
    return ["--platform", "linux/arm64", "--mount", `type=bind,src=${artifact.qemu.path},dst=/qemu-aarch64,readonly`, "--entrypoint", "/qemu-aarch64"];
  }
  async cleanup() {
    for (const id of this.containers.toReversed()) {
      assert.equal(await docker(["inspect", id, "--format", '{{index .Config.Labels "io.hololive.admin-fixture"}}']), this.id);
      await docker(["stop", "--time", "25", id]);
      await docker(["rm", id]);
    }
    for (const server of this.servers.toReversed()) await close(server);
    if (this.network) await docker(["network", "rm", this.network]);
    if (this.prepared) {
      const directory = await fs.lstat(this.directory);
      assert(directory.isDirectory() && !directory.isSymbolicLink() && directory.uid === process.getuid(), "fixture cleanup requires its original owned directory");
      await run("sudo", ["-n", "chown", "-R", `${process.getuid()}:${process.getgid()}`, this.directory]);
      await fs.rm(this.directory, { recursive: true });
    }
  }
}
