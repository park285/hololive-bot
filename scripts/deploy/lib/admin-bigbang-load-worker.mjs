import { parentPort, workerData } from "node:worker_threads";
import http from "node:http";
import { isDeepStrictEqual } from "node:util";

const { base, routes, headers, expected, timeoutMilliseconds } = workerData;
const agent = new http.Agent({ keepAlive: true, maxSockets: 1, maxFreeSockets: 1 });
const utf8 = new TextDecoder("utf-8", { fatal: true });
let running = false;

function get(route) {
  return new Promise((resolve, reject) => {
    const call = http.get(new URL(route, base), { headers, agent, timeout: timeoutMilliseconds }, response => {
      const chunks = []; let size = 0;
      response.on("data", chunk => {
        size += chunk.length;
        if (size > 8 * 1024 ** 2) response.destroy(new Error("body_limit"));
        else chunks.push(chunk);
      });
      response.on("error", reject);
      response.on("end", () => {
        const receivedAt = performance.now();
        resolve({ status: response.statusCode, receivedAt, bytes: Buffer.concat(chunks) });
      });
    });
    call.on("error", reject);
    call.on("timeout", () => call.destroy(new Error("request_timeout")));
  });
}

parentPort.on("message", async ({ phase, milliseconds, minimumSamples, shared }) => {
  if (running) throw new Error("load phases must not overlap");
  running = true;
  const counters = new Int32Array(shared), stop = routes.length + 1, errorCount = routes.length + 2;
  const started = performance.now(), counts = routes.map(() => 0), latencies = routes.map(() => []), errors = [];
  let fatal;
  try {
    while (Atomics.load(counters, stop) === 0 && (performance.now() - started < milliseconds || routes.some((_, index) => Atomics.load(counters, index + 1) < minimumSamples))) {
      if (performance.now() - started > 300000) throw new Error("sample collection exceeded 300s bound");
      const index = Atomics.add(counters, 0, 1) % routes.length, route = routes[index], sent = performance.now();
      let received = false, error;
      try {
        const response = await get(route);
        received = true; counts[index]++; Atomics.add(counters, index + 1, 1); latencies[index].push(response.receivedAt - sent);
        let matches = false;
        try { matches = isDeepStrictEqual(JSON.parse(utf8.decode(response.bytes)), expected[index]); } catch { /* 잘못된 본문도 같은 요청의 실패로 남깁니다. */ }
        if (response.status !== 200 || !matches) error = { route, status: response.status, reason: "response_mismatch" };
      } catch (cause) {
        if (!received) { counts[index]++; Atomics.add(counters, index + 1, 1); latencies[index].push(performance.now() - sent); }
        error = { route, reason: cause.code ?? cause.message };
      }
      if (error) {
        errors.push(error);
        if (Atomics.add(counters, errorCount, 1) + 1 > 100) throw new Error("more than 100 request errors; samples preserved");
      }
    }
  } catch (error) { fatal = error.message; Atomics.store(counters, stop, 1); }
  finally { running = false; }
  parentPort.postMessage({ phase, counts, latencies, errors, fatal, elapsed_seconds: (performance.now() - started) / 1000 });
});

// 각 client는 수신 시각을 먼저 기록하고 검증을 끝낸 뒤에만 다음 요청을 보냅니다.
// 같은 agent를 다음 측정 구간에도 유지하여 워밍업한 연결을 재사용합니다.
parentPort.postMessage({ ready: true });
parentPort.on("close", () => agent.destroy());
