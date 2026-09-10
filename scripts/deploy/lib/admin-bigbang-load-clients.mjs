import assert from "node:assert/strict";
import { Worker } from "node:worker_threads";

/** createLoadClients는 8개 client의 HTTP·검증을 분리하고 준비 실패 시 생성한 worker를 회수합니다. */
export async function createLoadClients({ base, routes, headers, expected, timeoutMilliseconds = 10000 }) {
  assert(routes.length > 0 && routes.length === expected.length);
  assert(timeoutMilliseconds > 0 && timeoutMilliseconds <= 10000);
  const clients = new LoadClients(routes), readiness = [];
  const timer = setTimeout(() => clients.fail(new Error("load clients did not become ready")), 10000);
  try {
    for (let index = 0; index < 8; index++) {
      const ready = Promise.withResolvers();
      const worker = new Worker(new URL("./admin-bigbang-load-worker.mjs", import.meta.url), { workerData: { base, routes, headers, expected, timeoutMilliseconds } });
      readiness.push(ready.promise);
      const slot = { worker, ready, pending: null }; clients.slots.push(slot);
      worker.on("message", message => {
        if (message.ready) { ready.resolve(); return; }
        if (clients.closed || clients.failure) return;
        if (!slot.pending || message.phase !== slot.pending.phase) { clients.fail(new Error("load worker returned an unexpected phase")); return; }
        const pending = slot.pending; slot.pending = null; pending.resolve(message);
      });
      worker.on("error", () => clients.fail(new Error("load worker failed")));
      worker.on("exit", code => { if (!clients.closed) clients.fail(new Error(`load worker exited: ${code}`)); });
    }
    await Promise.all(readiness);
    if (clients.failure) throw clients.failure;
    return clients;
  } catch (error) {
    clients.fail(error);
    await Promise.allSettled([...readiness, clients.close()]);
    throw error;
  } finally { clearTimeout(timer); }
}

class LoadClients {
  slots = [];
  sequence = 0;
  current;
  failure;
  closed = false;

  constructor(routes) { this.routes = routes; }

  /** phase는 전역 round-robin과 최소 표본 수를 유지하며 모든 client의 완료·오류를 합칩니다. */
  async phase(milliseconds, minimumSamples) {
    assert(Number.isInteger(milliseconds) && milliseconds > 0 && milliseconds <= 300000);
    assert(Number.isInteger(minimumSamples) && minimumSamples >= 0);
    if (this.closed || this.failure) throw this.failure ?? new Error("load clients closed");
    if (this.current) throw new Error("load phases must not overlap");
    const phase = ++this.sequence, shared = new SharedArrayBuffer(Int32Array.BYTES_PER_ELEMENT * (this.routes.length + 3));
    this.current = new Int32Array(shared);
    const started = performance.now();
    const promises = this.slots.map(slot => {
      const pending = Promise.withResolvers(); slot.pending = { ...pending, phase }; return pending.promise;
    });
    const complete = Promise.all(promises);
    const timer = setTimeout(() => this.fail(new Error("load phase exceeded its completion bound")), 310000);
    try {
      for (const { worker } of this.slots) worker.postMessage({ phase, milliseconds, minimumSamples, shared });
      const results = await complete, counts = this.routes.map(() => 0), latencies = this.routes.map(() => []), errors = [];
      for (const result of results) {
        errors.push(...result.errors);
        if (result.fatal) errors.push({ reason: result.fatal });
        for (let index = 0; index < this.routes.length; index++) {
          counts[index] += result.counts[index];
          for (const value of result.latencies[index]) latencies[index].push(value);
        }
      }
      return { elapsed_seconds: (performance.now() - started) / 1000, counts, latencies, errors, client_seconds: results.map(result => result.elapsed_seconds) };
    } catch (error) {
      error.completedCounts = this.routes.map((_, index) => Atomics.load(this.current, index + 1));
      this.fail(error);
      await complete.catch(() => {});
      throw error;
    } finally {
      clearTimeout(timer); this.current = undefined;
      for (const slot of this.slots) slot.pending = null;
    }
  }

  fail(error) {
    this.failure ??= error;
    if (this.current) Atomics.store(this.current, this.routes.length + 1, 1);
    for (const slot of this.slots) { slot.ready.reject(this.failure); slot.pending?.reject(this.failure); }
  }

  /** close는 진행 중인 구간을 거부하고 이 인스턴스가 만든 worker와 연결을 모두 회수합니다. */
  async close() {
    this.closed = true;
    this.fail(new Error("load clients closed"));
    await Promise.allSettled(this.slots.map(({ worker }) => worker.terminate()));
  }
}
