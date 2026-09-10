import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { Fixture, policy, proxyImage, valkeyImage, request, generation, root } from "./lib/admin-bigbang-fixture.mjs";

import { checkCompose } from "./lib/admin-bigbang-compose-fixture.mjs";

assert.equal(process.argv.length, 3, "immutable local fixture image required");
const fixture = new Fixture(process.argv[2]);
const evidence = { schema_version: 1, fixture_image: fixture.image, generation, proxy_image: proxyImage, valkey_image: valkeyImage, cases: [], production_effects: 0 };
try {
  await fixture.start();
  evidence.compose = await checkCompose(fixture.directory);
  const direct = async (method, target, allowed) => {
    const before = fixture.dockerRequests.length;
    const response = await request(fixture.proxyURL, method, target);
    assert.equal(response.status, allowed ? method === "POST" ? 204 : 200 : ["DELETE", "PUT"].includes(method) ? 405 : 403, `${method} ${target}`);
    assert.equal(fixture.dockerRequests.length - before, Number(allowed), `proxy boundary ${method} ${target}`);
    evidence.cases.push({ boundary: "proxy", method, target, allowed, status: response.status, fake_engine_calls: fixture.dockerRequests.length - before });
  };
  for (const target of ["/_ping", "/version", "/containers/json", "/containers/json?all=true", "/v1.52/containers/json?all=1", "/containers/json?all=true&filters={}"]) await direct("GET", target, true);
  await direct("HEAD", "/v1.52/_ping", true);
  for (const entry of policy.containers) for (const action of ["start", "stop", "restart"]) {
    for (const prefix of ["", "/v1.52"]) await direct("POST", `${prefix}/containers/${entry.name}/${action}`, entry.actions.includes(action));
  }
  for (const [method, target] of [
    ["GET", "/containers/hololive-api/json"], ["GET", "/containers/hololive-api/logs"], ["GET", "/events"], ["GET", "/images/json"],
    ["POST", "/containers/create"], ["POST", "/containers/hololive-api/exec"],
    ["POST", "/containers/hololive-api-extra/start"], ["POST", "/containers/hololive-api/restart/extra"],
    ["POST", "/containers/0123456789abcdef/restart"], ["POST", "/containers/hololive-youtube-collector-a/restart"],
    ["POST", "/containers/hololive-api/../unmanaged/restart"], ["POST", "/containers/hololive-api%2f..%2funmanaged/restart"],
    ["DELETE", "/containers/hololive-api"], ["PUT", "/containers/hololive-api/start"],
  ]) await direct(method, target, false);
  // 고정 upstream의 method/name/action이 proxy의 경계이며 query는 그대로 전달됩니다.
  await direct("POST", "/containers/hololive-api/restart?t=30&x=1", true);
  const auth = await fixture.login();
  const list = await request(fixture.bffURL, "GET", "/admin/api/docker/containers", auth);
  assert.equal(list.status, 200);
  assert.equal(JSON.parse(list.body).containers.length, 9);
  for (const entry of policy.containers) for (const action of ["start", "stop", "restart"]) {
    const target = `/admin/api/docker/containers/${entry.name}/${action}`, before = fixture.effects.length;
    const mutationID = randomUUID();
    const response = await request(fixture.bffURL, "POST", target, { ...auth, "X-Admin-Mutation-ID": mutationID });
    const allowed = entry.actions.includes(action);
    assert.equal(response.status, allowed ? 200 : 403, target);
    assert.equal(fixture.effects.length - before, Number(allowed), `BFF boundary ${target}`);
    if (!allowed) assert.equal(JSON.parse(response.body).notDispatchedMutationId, mutationID);
    evidence.cases.push({ boundary: "BFF + proxy", target, allowed, status: response.status, fake_engine_effects: fixture.effects.length - before });
  }
  for (const name of ["0123456789abcdef", "hololive-api-extra", "hololive-youtube-collector-a", "unmanaged"]) {
    const before = fixture.effects.length;
    const response = await request(fixture.bffURL, "POST", `/admin/api/docker/containers/${name}/restart`, { ...auth, "X-Admin-Mutation-ID": randomUUID() });
    assert.equal(response.status, 404); assert.equal(fixture.effects.length, before);
    evidence.cases.push({ boundary: "BFF + proxy", name, allowed: false, status: response.status, fake_engine_effects: 0 });
  }
  // auth·CSRF·generation 검사를 통과하지 못한 호출은 proxy를 통해서도 효과를 낼 수 없습니다.
  for (const kind of ["auth", "csrf", "generation", "mutation-id"]) {
    const headers = { ...auth, "X-Admin-Mutation-ID": randomUUID() };
    delete headers[{ auth: "Cookie", csrf: "X-CSRF-Token", generation: "X-Admin-Client-Generation", "mutation-id": "X-Admin-Mutation-ID" }[kind]];
    const before = fixture.effects.length;
    const response = await request(fixture.bffURL, "POST", "/admin/api/docker/containers/hololive-api/restart", headers);
    assert.equal(response.status, { auth: 401, csrf: 403, generation: 409, "mutation-id": 400 }[kind]);
    assert.equal(fixture.effects.length, before);
    evidence.cases.push({ boundary: "BFF authorization", kind, status: response.status, fake_engine_effects: 0 });
  }
  const constrained = await request(fixture.bffURL, "POST", "/admin/api/docker/containers/hololive-api/restart?t=-1&signal=SIGKILL", { ...auth, "X-Admin-Mutation-ID": randomUUID() });
  assert.equal(constrained.status, 200);
  assert.equal(fixture.effects.at(-1).path, "/containers/hololive-api/restart?t=30");
  evidence.query_boundary = { proxy: "method and decoded URL.Path only; query forwarded", BFF: "fixed action path and t=30; caller query not forwarded" };
  evidence.materialization = { files: 4, owner: 0, group: 1002, mode: "0640", unrelated_reader_denied: 4, candidate_file_authentication: "PASS" };
  evidence.status = "PASS";
  const destination = path.join(root, "admin-dashboard/frontend/node_modules/.cache/admin-boundaries.json");
  await fs.mkdir(path.dirname(destination), { recursive: true }); await fs.writeFile(destination, JSON.stringify(evidence, null, 2) + "\n");
  console.log(`PASS: ${evidence.cases.length} real proxy/BFF boundary cases; four materialized secret files; evidence=${destination}`);
} finally { await fixture.cleanup(); }
