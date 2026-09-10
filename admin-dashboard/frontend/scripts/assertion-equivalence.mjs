import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs/promises";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import * as models from "../src/api/generated/validators/models.mjs";
import { loadOperationValidators } from "../src/api/generated/validation.ts";

const schema = JSON.parse(await fs.readFile(new URL("../src/api/generated/schema-bundle.json", import.meta.url)));
const operations = JSON.parse(await fs.readFile(new URL("../src/api/generated/operation-contracts.json", import.meta.url)));
const generation = JSON.parse(await fs.readFile(new URL("../src/api/generated/provenance.json", import.meta.url))).generation;
const generated = Object.assign({}, models, ...await Promise.all(operations.map(operation => loadOperationValidators(operation.id))));
const ajv = new Ajv2020({ strict: true, allErrors: false, coerceTypes: false, useDefaults: false, removeAdditional: false });
addFormats(ajv); ajv.addSchema(schema);
const fixtures = JSON.parse(execFileSync("go", ["run", "./cmd/contract-fixtures"], { cwd: new URL("../../backend/", import.meta.url), encoding: "utf8" }));
const values = [null, false, true, 0, -1, 1, -2147483649, -2147483648, 2147483647, 2147483648, 1e20, 1.5, NaN, Infinity, "", "한글😀", "9007199254740993", [], {}, ...Object.values(fixtures),
  { status: "ok" }, { status: "ok", absolute_expires_at: 1 }, { status: "idle", idle_rejected: true },
  { status: "ok", absolute_expires_at: 1, rotated: true, csrf_token: "fixture-csrf" },
  { code: "FORBIDDEN", message: "fixture", requestId: "fixture-request" },
];
const headers = { "x-admin-client-generation": generation, "x-csrf-token": "fixture-csrf", "x-admin-mutation-id": "5ae58f70-51c4-4df2-bf70-683773e40638" };
for (const body of values.slice()) {
  values.push({ headers: { "x-admin-server-generation": generation }, body });
  for (const path of [{}, { id: "9007199254740993" }, { roomId: "9007199254740993", channelId: "UC-test" }, { name: "hololive-api" }]) {
    values.push({ path, query: {}, headers, body });
  }
}
for (const object of values.slice()) if (object && typeof object === "object" && !Array.isArray(object)) {
  values.push({ ...object, unowned: true });
  for (const key of Object.keys(object)) {
    const missing = { ...object }; delete missing[key]; values.push(missing);
    for (const replacement of [null, false, "", [], {}, -1, 9007199254740992, -2147483649, -2147483648, 2147483647, 2147483648, 1e20, 1.5]) values.push({ ...object, [key]: replacement });
  }
}
let comparisons = 0, positive = 0;
for (const name of Object.keys(schema.$defs)) {
  const original = ajv.getSchema(`${schema.$id}#/$defs/${name}`);
  for (const value of values) {
    const before = structuredClone(value), expected = original(value), actual = generated[name](value);
    assert.equal(actual, expected, `${name}: boolean assertion differs from the unchanged schema`);
    assert.deepEqual(value, before, `${name}: validator mutated input`);
    comparisons++; if (expected) positive++;
  }
}
assert(positive > 0);
console.log(`Assertion equivalence: ${Object.keys(schema.$defs).length} schemas, ${comparisons} comparisons, ${positive} valid fixtures; no mismatch or input mutation`);
