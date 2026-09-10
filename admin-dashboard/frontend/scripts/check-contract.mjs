import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";

const spec = JSON.parse(await fs.readFile(new URL("../../backend/internal/contract/openapi.json", import.meta.url), "utf8"));
const provenance = JSON.parse(await fs.readFile(new URL("../src/api/generated/provenance.json", import.meta.url), "utf8"));
const generation = createHash("sha256").update(JSON.stringify(spec)).digest("hex");
assert.equal(provenance.generation, generation, "contract changed: run npm run generate:api");
const generationSource = await fs.readFile(new URL("../src/api/generated/generation.ts", import.meta.url), "utf8");
assert(generationSource.includes(`CLIENT_GENERATION = "${generation}"`), "SDK generation is stale");
const goSource = await fs.readFile(new URL("../../backend/internal/contract/operations_generated.go", import.meta.url), "utf8");
assert(goSource.includes(`Generation = "${generation}"`), "BFF generation is stale");
console.log(`Contract generation verified: ${generation}`);
