import assert from "node:assert/strict";
import os from "node:os";
import { performance } from "node:perf_hooks";

import { EncodedArrayBudget, paginationEnvelopeReserve, paginationResult } from "./pagination.mjs";

const warmupCount = 5;
const sampleCount = 11;
const wallBatchItems = 32_000;
const sizes = [1000, 2000, 4000];
const reserve = paginationEnvelopeReserve({ protocol_version: 1, items: [] });
const fixtures = new Map(sizes.map((size) => [
  size,
  Array.from({ length: size }, (_, index) => ({
    id: `item-${index}`,
    payload: "x".repeat(512),
  })),
]));

for (let index = 0; index < warmupCount; index += 1) {
  aggregate(fixtures.get(4000));
}

const measurements = sizes.map((size) => measure(size, fixtures.get(size)));
process.stdout.write(`${JSON.stringify({
  test: "PAG-014",
  node_version: process.version,
  cpu_model: os.cpus()[0]?.model ?? "unknown",
  warmup_count: warmupCount,
  sample_count: sampleCount,
  wall_batch_items: wallBatchItems,
  measurements,
})}\n`);
for (let index = 1; index < measurements.length; index += 1) {
  const previous = measurements[index - 1];
  const current = measurements[index];
  assert.ok(
    current.median_wall_ms / previous.median_wall_ms <= 2.5,
    `PAG-014 median wall ratio exceeded 2.5 at ${current.items} items`,
  );
  assert.ok(
    current.heap_allocation_bytes / previous.heap_allocation_bytes <= 2.5,
    `PAG-014 allocation ratio exceeded 2.5 at ${current.items} items`,
  );
}

function measure(size, fixture) {
  const wallBatchCount = Math.max(1, Math.trunc(wallBatchItems / size));
  const walls = [];
  const allocations = [];
  let finalEncodedBytes = 0;
  for (let index = 0; index < sampleCount; index += 1) {
    globalThis.gc();
    const heapBefore = process.memoryUsage().heapUsed;
    const budget = aggregate(fixture);
    const heapAfter = process.memoryUsage().heapUsed;
    allocations.push(Math.max(1, heapAfter - heapBefore));
    finalEncodedBytes = finalBytes(budget);
    globalThis.gc();
    const started = performance.now();
    for (let batch = 0; batch < wallBatchCount; batch += 1) {
      aggregate(fixture);
    }
    walls.push((performance.now() - started) / wallBatchCount);
  }
  walls.sort((left, right) => left - right);
  allocations.sort((left, right) => left - right);
  return {
    items: size,
    wall_batch_count: wallBatchCount,
    median_wall_ms: walls[Math.floor(walls.length / 2)],
    p95_wall_ms: walls[Math.ceil(walls.length * 0.95) - 1],
    heap_allocation_bytes: allocations[Math.floor(allocations.length / 2)],
    final_encoded_bytes: finalEncodedBytes,
  };
}

function aggregate(fixture) {
  const budget = new EncodedArrayBudget(Number.MAX_SAFE_INTEGER, reserve);
  for (const item of fixture) {
    assert.equal(budget.tryAppend(item), "APPENDED");
  }
  return budget;
}

function finalBytes(budget) {
  const result = {
    protocol_version: 1,
    items: budget.values(),
    ...paginationResult({
      pageCount: 1,
      reason: "exhausted",
      continuity: "CONTIGUOUS",
    }),
  };
  return Buffer.byteLength(JSON.stringify(result));
}
