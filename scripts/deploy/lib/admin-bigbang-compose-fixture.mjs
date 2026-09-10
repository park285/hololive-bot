import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import { run, root } from "./admin-bigbang-fixture.mjs";

/** checkCompose는 service env 파일을 읽지 않고 canonical overlay를 synthetic 입력으로 렌더합니다. */
export async function checkCompose(directory) {
  const envFile = path.join(directory, "compose-fixture.env");
  await fs.writeFile(envFile, `DB_PASSWORD=synthetic-fixture-only\nCACHE_PASSWORD=synthetic-fixture-only\nHOLO_API_VERSION=fixture\nHOLO_ALARM_WORKER_VERSION=fixture\nLIVE_LOGS_PATH=${directory}/logs\nHOLOLIVE_RUNTIME_GID=1002\nDOCKER_SOCKET_GID=107\n`, { mode: 0o600 });
  const files = ["docker-compose.prod.yml", "docker-compose.admin-security.yml", "docker-compose.live-compat.yml"];
  const { stdout } = await run("docker", ["compose", "--env-file", envFile, ...files.flatMap(file => ["-f", path.join(root, "deploy/compose", file)]), "config", "--no-env-resolution", "--format", "json"], {
    cwd: root, env: { PATH: process.env.PATH, HOME: process.env.HOME }, maxBuffer: 4 * 1024 * 1024,
  });
  const config = JSON.parse(stdout), admin = config.services["admin-dashboard"], proxy = config.services["admin-docker-proxy"];
  assert.equal((admin.env_file ?? []).length, 0, "security overlay must remove direct secret env files");
  for (const key of ["ADMIN_PASS_HASH", "SESSION_SECRET", "VALKEY_URL", "HOLO_BOT_API_KEY"]) {
    assert.equal(admin.environment[key], undefined, `${key} must not enter container Config.Env`);
    assert.equal(typeof admin.environment[`${key}_FILE`], "string");
  }
  assert.equal(admin.environment.DOCKER_HOST, "tcp://admin-docker-proxy:2375");
  assert.deepEqual(Object.keys(admin.networks).sort(), ["admin-docker-proxy-net", "hololive-net"]);
  assert.deepEqual(Object.keys(proxy.networks), ["admin-docker-proxy-net"]);
  assert.equal(config.networks["admin-docker-proxy-net"].internal, true);
  assert.equal(proxy.read_only, true);
  assert(proxy.cap_drop.includes("ALL") && proxy.security_opt.includes("no-new-privileges:true"));
  assert(admin.group_add.map(String).includes("1002"));
  assert(admin.volumes.some(volume => volume.target === "/run/hololive-bot/admin-secrets" && volume.read_only));
  assert.equal(admin.volumes.some(volume => volume.target.endsWith("docker.sock")), false);
  assert(proxy.volumes.some(volume => volume.target === "/var/run/docker.sock" && volume.read_only));
  assert(admin.ports.every(port => port.host_ip === "127.0.0.1" && Number(port.target) === 30190));
  assert.equal(config.services["admin-dashboard-ingress"].network_mode, "host");
  assert.equal(admin.healthcheck.test[0], "CMD");
  assert.equal(admin.healthcheck.test[1], "./bin/healthcheck");
  const actualPolicy = JSON.parse(await fs.readFile(path.join(root, "deploy/compose/admin-docker-policy.generated.json")));
  assert.deepEqual(proxy.command, actualPolicy.services["admin-docker-proxy"].command);
  return { status: "PASS", overlays: files, env_files_read: 0, secret_values_in_candidate_environment: 0, admin_port: "127.0.0.1:30190", dedicated_proxy_network: true, healthcheck: admin.healthcheck.test, unrelated_services_activated: 0 };
}
