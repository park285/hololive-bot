import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import * as models from "../src/api/generated/validators/models.mjs";
import { loadOperationValidators } from "../src/api/generated/validation.ts";
import runtimeOperations from "../src/api/generated/operation-contracts.ts";

const frontend = fileURLToPath(new URL("../", import.meta.url));
const operations = JSON.parse(await fs.readFile(new URL("../src/api/generated/operation-contracts.json", import.meta.url)));
const validators = Object.assign({}, models, ...await Promise.all(operations.map(operation => loadOperationValidators(operation.id))));
const provenance = JSON.parse(await fs.readFile(new URL("../src/api/generated/provenance.json", import.meta.url)));
const headers = { "x-admin-client-generation": provenance.generation };
const responseHeaders = { "x-admin-server-generation": provenance.generation };
const member = { id: "9007199254740993", channelId: "UC-test", name: "한글😀", aliases: { ko: [], ja: [] }, isGraduated: false };

test("standalone validators preserve large IDs, null, required fields and closed objects without mutation", () => {
	for (const [data, valid] of [
		[member, true], [{ ...member, nameKo: null }, true],
		[{ ...member, id: 9007199254740993 }, false], [{ ...member, id: "01" }, false],
		[{ ...member, aliases: undefined }, false], [{ ...member, unowned: true }, false],
	]) {
		const before = structuredClone(data);
		assert.equal(validators.Member(data), valid);
		assert.deepEqual(data, before);
	}
});

test("heartbeat oneOf distinguishes refreshed, rotated and idle outcomes", () => {
	for (const [body, valid] of [
		[{ status: "ok", absolute_expires_at: 1 }, true],
		[{ status: "ok", absolute_expires_at: 1, rotated: true, csrf_token: "fixture-token" }, true],
		[{ status: "idle", idle_rejected: true }, true],
		[{ status: "ok" }, false], [{ status: "idle" }, false],
		[{ status: "ok", absolute_expires_at: "1" }, false],
		[{ status: "ok", absolute_expires_at: 1, rotated: true }, false],
	]) assert.equal(validators.HeartbeatResponse(body), valid, JSON.stringify(body));
});

test("request validators cover query, path, header and body at operation boundaries", () => {
	const calendar = { path: {}, query: { year: 2026, month: 9 }, headers };
	assert(validators.request_holoGetCalendar(calendar));
	assert(!validators.request_holoGetCalendar({ ...calendar, headers: {} }));
	assert(!validators.request_holoGetCalendar({ ...calendar, query: { year: 2026, month: "9" } }));
	assert(!validators.request_holoGetCalendar({ ...calendar, query: { year: 2026, month: 9, token: "unowned" } }));
	const rename = { path: { id: member.id }, query: {}, headers: { ...headers, "x-csrf-token": "fixture-token", "x-admin-mutation-id": "5ae58f70-51c4-4df2-bf70-683773e40638" }, body: { name: "한글" } };
	assert(validators.request_holoUpdateMemberName(rename));
	assert(!validators.request_holoUpdateMemberName({ ...rename, headers: { ...headers, "x-csrf-token": "fixture-token" } }));
	assert(!validators.request_holoUpdateMemberName({ ...rename, headers: { ...rename.headers, "x-admin-mutation-id": "not-a-uuid" } }));
	assert(!validators.request_holoUpdateMemberName({ ...rename, path: { id: 12 } }));
	assert(!validators.request_holoUpdateMemberName({ ...rename, headers }));
	assert(!validators.request_holoUpdateMemberName({ ...rename, body: { name: "한글", extra: 1 } }));
	assert(validators.request_getAdminMetadata({ path: {}, query: {}, headers: {} }));
});

test("all operation responses have declared status/content-type and a generated validator", () => {
	assert.equal(operations.length, 35);
	assert.deepEqual(runtimeOperations, operations);
	const names = new Set();
	for (const operation of operations) {
		assert(!names.has(operation.id)); names.add(operation.id);
		assert.equal(typeof validators[operation.request], "function");
		for (const [status, response] of Object.entries(operation.responses)) {
			assert.match(status, /^[1-5][0-9]{2}$/);
			assert.equal(response.contentType, "application/json");
			assert.equal(typeof validators[response.validator], "function");
		}
	}
	const login = operations.find(operation => operation.id === "handle_login");
	const validateError = validators[login.responses["401"].validator];
	assert(validateError({ headers: responseHeaders, body: { code: "UNAUTHORIZED", message: "인증이 필요합니다.", requestId: "fixture-request" } }));
	assert(!validateError({ headers: {}, body: { code: "UNAUTHORIZED", message: "인증이 필요합니다.", requestId: "fixture-request" } }));
	assert(!validateError({ headers: responseHeaders, body: { error: "old shape" } }));
});

test("manual Go DTOs satisfy the same generated validators without rounding an upstream integer ID", () => {
	const output = execFileSync("go", ["run", "./cmd/contract-fixtures"], { cwd: path.resolve(frontend, "../backend"), encoding: "utf8" });
	const fixtures = JSON.parse(output);
	for (const [name, value] of Object.entries(fixtures)) assert(validators[name](value), name);
	assert.equal(fixtures.Member.id, "9007199254740993");
	const appOutput = execFileSync("go", ["test", "-json", "-count=1", "./internal/httpapi", "./internal/adapters/holo", "-run", "^TestContractResponseFixtures$"], { cwd: path.resolve(frontend, "../backend"), encoding: "utf8" });
	const lines = appOutput.trim().split("\n").map(line => JSON.parse(line));
	const marker = "CONTRACT_FIXTURES ";
	// go test -json은 긴 로그 한 줄도 여러 Output 사건으로 나눕니다.
	const packages = new Map();
	for (const line of lines) if (line.Test === "TestContractResponseFixtures" && typeof line.Output === "string") packages.set(line.Package, (packages.get(line.Package) ?? "") + line.Output);
	assert.equal(packages.size, 2);
	for (const outputText of packages.values()) {
		const payload = outputText.split("\n").find(line => line.includes(marker));
		assert(payload, "Go HTTP DTO fixtures were not emitted");
		const appFixtures = JSON.parse(payload.slice(payload.indexOf(marker) + marker.length));
		for (const [name, values] of Object.entries(appFixtures)) for (const value of values) assert(validators[name](value), `${name}: ${JSON.stringify(validators[name].errors)}`);
	}
});

test("generation is deterministic in a clean fixture and runtime excludes the schema compiler", async () => {
	async function hashes(root) {
		const directory = path.join(root, "src/api/generated");
		const files = (await fs.readdir(directory, { recursive: true, withFileTypes: true })).filter(file => file.isFile() && !file.name.startsWith(".")).map(file => path.relative(directory, path.join(file.parentPath, file.name))).sort();
		const entries = await Promise.all(files.map(async file => [file, createHash("sha256").update(await fs.readFile(path.join(directory, file))).digest("hex")]));
		entries.push(["operations_generated.go", createHash("sha256").update(await fs.readFile(path.resolve(root, "../backend/internal/contract/operations_generated.go"))).digest("hex")]);
		return Object.fromEntries(entries);
	}
	const before = await hashes(frontend);
	const temporary = await fs.mkdtemp(path.join(tmpdir(), "admin-contract-repro-"));
	try {
		const root = path.join(temporary, "frontend");
		const contract = path.join(temporary, "backend/internal/contract");
		await fs.mkdir(contract, { recursive: true });
		await fs.cp(path.join(frontend, "scripts"), path.join(root, "scripts"), { recursive: true });
		await fs.cp(path.join(frontend, "src/api/generated"), path.join(root, "src/api/generated"), { recursive: true });
		for (const name of ["package.json", "package-lock.json"]) await fs.copyFile(path.join(frontend, name), path.join(root, name));
		for (const name of ["openapi.json", "operations_generated.go"]) await fs.copyFile(path.resolve(frontend, "../backend/internal/contract", name), path.join(contract, name));
		// 설치된 lock 의존성만 공유하고 소스·생성 출력은 별도 빈 임시 트리에 둡니다.
		await fs.symlink(path.join(frontend, "node_modules"), path.join(root, "node_modules"), "dir");
		for (let run = 0; run < 2; run++) {
			execFileSync(process.execPath, ["scripts/generate-api.mjs"], { cwd: root, stdio: "pipe" });
			assert.deepEqual(await hashes(root), before);
		}
	} finally {
		await fs.rm(temporary, { recursive: true, force: true });
	}
	assert.deepEqual(await hashes(frontend), before);
	assert.equal(provenance.compilerInRuntime, false);
	assert.deepEqual(provenance.runtimeHelpers, ["node_modules/ajv/dist/runtime/ucs2length.js"]);
});

test("boolean assertions match unchanged schemas on Go fixtures and malformed boundary values", () => {
	// compiler는 비교용 자식 프로세스에서만 실행하고 이 프로세스와 production은 동적 코드를 금지합니다.
	const result = execFileSync(process.execPath, ["scripts/assertion-equivalence.mjs"], { cwd: frontend, encoding: "utf8", timeout: 30000 });
	assert.match(result, /110 schemas, .* no mismatch or input mutation/);
	process.stdout.write(result);
});
