import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  EncodedArrayBudget,
  paginate,
  paginationEnvelopeReserve,
  paginationResult,
} from "./pagination.mjs";
import { runWithRequestContext } from "./request-context.mjs";

const tupleFixture = JSON.parse(
  readFileSync(new URL("../testdata/pagination-tuples.json", import.meta.url), "utf8"),
);

function page(id, continuation) {
  return { id, continuation, items: [{ id }] };
}

function mapped(current) {
  return { recognized_shape: true, items: current.items };
}

function resultBuilder(items, pagination) {
  return { items, ...pagination };
}

function options(overrides = {}) {
  return {
    firstPage: page("a"),
    getContinuation: async () => {
      throw new Error("should not continue");
    },
    mapPage: mapped,
    maxPages: 3,
    maxResults: 10,
    maxSuccessResponseBytes: 1 << 20,
    reservedEnvelopeBytes: paginationEnvelopeReserve({ protocol_version: 1, items: [] }),
    buildResult: resultBuilder,
    ...overrides,
  };
}

test("PAG-009 recognized empty is complete", async () => {
  const result = await paginate({
    ...options(),
    firstPage: { items: [] },
  });
  assert.equal(result.page_count, 1);
  assert.equal(result.exhausted, true);
  assert.equal(result.continuity, "CONTIGUOUS");
  assert.equal(result.termination_reason, "exhausted");
  assert.deepEqual(result.items, []);
});

test("PAG-001 first page mapper failure is fatal", async () => {
  await assert.rejects(
    () => paginate(options({
      mapPage: () => {
        const error = new Error("timed out");
        error.code = "collection_timeout";
        throw error;
      },
    })),
    (error) => error.code === "collection_timeout",
  );
});

test("PAG-002 first page parser drift is fatal", async () => {
  await assert.rejects(
    () => paginate(options({
      mapPage: () => {
        const error = new Error("schema drifted");
        error.code = "parser_drift";
        throw error;
      },
    })),
    (error) => error.code === "parser_drift",
  );
});

test("PAG-003 later recognized transient preserves prefix", async () => {
  const error = new Error("connection reset");
  error.code = "ECONNRESET";
  const result = await paginate(options({
    firstPage: page("a", "cursor-b"),
    getContinuation: async () => {
      throw error;
    },
  }));
  assert.equal(result.termination_reason, "continuation_transient");
  assert.equal(result.exhausted, false);
  assert.equal(result.continuity, "GAP_UNRESOLVED");
  assert.deepEqual(result.items, [{ id: "a" }]);

  const timeout = new Error("attempt timed out");
  timeout.code = "collection_timeout";
  const timedOut = await paginate(options({
    firstPage: page("a", "cursor-b"),
    getContinuation: async () => {
      throw timeout;
    },
  }));
  assert.equal(timedOut.termination_reason, "continuation_transient");
});

test("PAG-004 later protocol/internal/parser failures are fatal", async () => {
  for (const code of ["collection_canceled", "helper_protocol_mismatch", "helper_internal_invariant", "parser_drift", "cooldown", "configuration_error"]) {
    await assert.rejects(
      () => paginate(options({
        firstPage: page("a", "cursor-b"),
        getContinuation: async () => {
          const error = new Error(code);
          error.code = code;
          throw error;
        },
      })),
      (error) => error.code === code,
    );
  }

  const controller = new AbortController();
  await assert.rejects(
    () => runWithRequestContext(
      { requestId: "PAG-004", signal: controller.signal },
      () => paginate(options({
        firstPage: page("a", "cursor-b"),
        getContinuation: async () => {
          controller.abort();
          const error = new Error("attempt timed out");
          error.code = "collection_timeout";
          throw error;
        },
      })),
    ),
    (error) => error.code === "collection_timeout",
  );
});

test("PAG-005 missing continuation is parser drift", async () => {
  await assert.rejects(
    () => paginate(options({
      firstPage: page("a", "cursor-b"),
      getContinuation: undefined,
    })),
    (error) => error.code === "parser_drift",
  );
});

test("PAG-006 repeated non-empty cursor terminates with cursor_loop", async () => {
  const result = await paginate(options({
    firstPage: page("a", "loop"),
    getContinuation: async () => page("b", "loop"),
  }));
  assert.equal(result.page_count, 2);
  assert.equal(result.termination_reason, "cursor_loop");
  assert.equal(result.continuity, "GAP_UNRESOLVED");
});

test("PAG-007 page and result caps retain exact reason", async () => {
  const pageCapped = await paginate(options({
    firstPage: page("a", "b"),
    maxPages: 1,
  }));
  assert.equal(pageCapped.termination_reason, "max_pages");

  const resultCapped = await paginate(options({
    firstPage: { items: [{ id: "a" }, { id: "b" }] },
    maxResults: 1,
  }));
  assert.equal(resultCapped.termination_reason, "max_results");
  assert.deepEqual(resultCapped.items, [{ id: "a" }]);

  let continued = 0;
  const exactFill = await paginate(options({
    firstPage: { items: [{ id: "a" }], continuation: "next" },
    maxResults: 1,
    getContinuation: async () => {
      continued += 1;
      const error = new Error("reset");
      error.code = "ECONNRESET";
      throw error;
    },
  }));
  assert.equal(continued, 0);
  assert.equal(exactFill.termination_reason, "max_results");
  assert.deepEqual(exactFill.items, [{ id: "a" }]);
});

test("PAG-008 budget measures the full success envelope", async () => {
  const reserve = paginationEnvelopeReserve({ protocol_version: 1, items: [] });
  const first = { id: "a" };
  const second = { id: "b" };
  const firstBytes = Buffer.byteLength(JSON.stringify(first));
  const secondBytes = Buffer.byteLength(JSON.stringify(second));
  const oneItemLimit = reserve + firstBytes;
  const twoItemLimit = reserve + firstBytes + 1 + secondBytes;

  await assert.rejects(
    () => paginate(options({
      firstPage: { items: [first] },
      maxSuccessResponseBytes: oneItemLimit - 1,
      reservedEnvelopeBytes: reserve,
    })),
    (error) => error.code === "response_too_large",
  );

  const atLimit = await paginate(options({
    firstPage: { items: [first] },
    maxSuccessResponseBytes: oneItemLimit,
    reservedEnvelopeBytes: reserve,
  }));
  assert.equal(atLimit.termination_reason, "exhausted");
  assert.equal(atLimit.items.length, 1);
  assert.ok(Buffer.byteLength(JSON.stringify({ protocol_version: 1, ...atLimit })) <= oneItemLimit);

  const plusOne = await paginate(options({
    firstPage: { items: [first] },
    maxSuccessResponseBytes: oneItemLimit + 1,
    reservedEnvelopeBytes: reserve,
  }));
  assert.equal(plusOne.termination_reason, "exhausted");
  assert.equal(plusOne.items.length, 1);

  const twoItemPartial = await paginate(options({
    firstPage: { items: [first, second] },
    maxSuccessResponseBytes: twoItemLimit - 1,
    reservedEnvelopeBytes: reserve,
  }));
  assert.equal(twoItemPartial.termination_reason, "max_success_response_bytes");
  assert.equal(twoItemPartial.items.length, 1);

  const twoItemExact = await paginate(options({
    firstPage: { items: [first, second] },
    maxSuccessResponseBytes: twoItemLimit,
    reservedEnvelopeBytes: reserve,
  }));
  assert.equal(twoItemExact.termination_reason, "exhausted");
  assert.equal(twoItemExact.items.length, 2);
  assert.ok(Buffer.byteLength(JSON.stringify({ protocol_version: 1, ...twoItemExact })) <= twoItemLimit);

  const cursor = "x".repeat(8190);
  const maximumMetadata = {
    protocol_version: 1,
    items: [],
    page_count: 100,
    cursor_start: cursor,
    cursor_end: cursor,
    exhausted: false,
    continuity: "GAP_UNRESOLVED",
    termination_reason: "max_success_response_bytes",
  };
  assert.ok(reserve >= Buffer.byteLength(JSON.stringify(maximumMetadata)));
});

test("PAG-010 unknown shape and plain array are distinct fatal errors", async () => {
  await assert.rejects(
    () => paginate(options({
      mapPage: () => {
        const error = new Error("unknown shape");
        error.code = "parser_drift";
        throw error;
      },
    })),
    (error) => error.code === "parser_drift",
  );
  await assert.rejects(
    () => paginate(options({ mapPage: (current) => current.items })),
    (error) => error.code === "helper_internal_invariant",
  );
});

test("PAG-012 cursor JSON bytes accept 8192 and reject 8193", async () => {
  const accepted = "x".repeat(8190);
  const result = await paginate(options({
    firstPage: page("a", accepted),
    maxPages: 1,
  }));
  assert.equal(result.cursor_start, accepted);

  await assert.rejects(
    () => paginate(options({
      firstPage: page("a", "x".repeat(8191)),
      maxPages: 1,
    })),
    (error) => error.code === "helper_protocol_mismatch",
  );
});

test("PAG-013 paginationResult rejects impossible tuples", () => {
  for (const item of tupleFixture.valid) {
    const result = paginationResult({
      pageCount: 1,
      reason: item.reason,
      continuity: item.continuity,
    });
    assert.equal(result.exhausted, item.exhausted);
    assert.equal(result.continuity, item.continuity);
    assert.equal(result.termination_reason, item.reason);
  }
  for (const item of tupleFixture.invalid) {
    try {
      const result = paginationResult({
        pageCount: 1,
        reason: item.reason,
        continuity: item.continuity,
      });
      assert.notEqual(result.exhausted, item.exhausted);
    } catch (error) {
      assert.equal(error.code, "helper_protocol_mismatch");
    }
  }
});

test("EncodedArrayBudget stringifies each item once", () => {
  let calls = 0;
  const items = Array.from({ length: 8 }, (_, index) => ({
    toJSON() {
      calls += 1;
      return { id: index };
    },
  }));
  const budget = new EncodedArrayBudget(1024, 100);
  for (const item of items) {
    assert.equal(budget.tryAppend(item), "APPENDED");
  }
  assert.equal(calls, items.length);
  assert.equal(budget.count(), items.length);
  assert.deepEqual(budget.values(), items);
});
