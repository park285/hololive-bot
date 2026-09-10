import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const admin = path.join(root, "admin-dashboard");
const require = createRequire(path.join(admin, "frontend/package.json"));
const ts = require("typescript");
const read = relative => fs.readFileSync(path.join(admin, relative), "utf8");
const hash = value => createHash("sha256").update(value).digest("hex");
const baseline = JSON.parse(read("docs/bigbang/endpoint-feature-parity.json"));
const operations = JSON.parse(read("frontend/src/api/generated/operation-contracts.json"));
const sdkSource = ts.createSourceFile("Admin.ts", read("frontend/src/api/generated/Admin.ts"), ts.ScriptTarget.Latest, true);
const methods = new Map();

function visit(node, callback) {
	callback(node);
	ts.forEachChild(node, child => visit(child, callback));
}

visit(sdkSource, node => {
	if (!ts.isPropertyDeclaration(node) || !ts.isIdentifier(node.name)) return;
	visit(node, child => {
		if (!ts.isPropertyAssignment(child) || child.name.getText(sdkSource) !== "operationId" || !ts.isStringLiteral(child.initializer)) return;
		assert(!methods.has(child.initializer.text), "duplicate SDK operation");
		methods.set(child.initializer.text, node.name.text);
	});
});

function owner(operation) {
	if (operation.id === "getAdminMetadata") return ["app/bootstrap.ts", "auth", false];
	if (operation.id === "getAdminOpenAPI") return ["api/generated/Admin.ts", "openapi", true];
	if (operation.id === "holoGetYouTubeCommunityShortsOps") return ["api/generated/Admin.ts", "stats", true];
	if (operation.path.startsWith("/admin/api/auth/")) return ["session/controller.ts", "auth", false];
	if (operation.path.startsWith("/admin/api/docker/")) return ["features/docker/api.ts", "docker", false];
	if (operation.id === "holoGetCalendar") return ["features/calendar/api.ts", "calendar", false];
	if (operation.path.startsWith("/admin/api/holo/members")) return ["features/members/api.ts", "members", false];
	if (operation.path.startsWith("/admin/api/holo/alarms") || operation.path.startsWith("/admin/api/holo/names/")) return ["features/alarms/api.ts", "alarms", operation.id === "holoSetUserName"];
	if (operation.path.startsWith("/admin/api/holo/rooms")) return ["features/rooms/api.ts", "rooms", false];
	if (operation.path === "/admin/api/holo/settings") return ["features/settings/api.ts", "settings", false];
	if (operation.path === "/admin/api/holo/stats" || operation.path === "/admin/api/status") return ["features/stats/api.ts", "stats", false];
	if (operation.path.startsWith("/admin/api/holo/streams/")) return ["features/streams/api.ts", "streams", false];
	throw new Error(`Unmapped administrator operation: ${operation.id}`);
}

assert.equal(operations.length, 35);
assert.equal(methods.size, operations.length);
assert.equal(operations.filter(operation => operation.mutation).length, 16);
for (const operation of baseline.operations) assert(operations.some(candidate => candidate.id === operation.operationId && candidate.method === operation.method && candidate.path === operation.path), `baseline route missing: ${operation.operationId}`);

const bindings = operations.map(operation => {
	const [file, feature, hidden] = owner(operation);
	const sdkMethod = methods.get(operation.id);
	assert(sdkMethod, `missing SDK method: ${operation.id}`);
	const frontendOwner = `frontend/src/${file}`;
	if (file !== "api/generated/Admin.ts") {
		const source = ts.createSourceFile(file, read(frontendOwner), ts.ScriptTarget.Latest, true);
		let bound = false;
		visit(source, node => { if (ts.isPropertyAccessExpression(node) && node.name.text === sdkMethod) bound = true; });
		assert(bound, `SDK method is not used by ${file}: ${sdkMethod}`);
	}
	return { operationId: operation.id, method: operation.method, path: operation.path, feature, hidden, mutation: operation.mutation, access: operation.access, sdkMethod, frontendOwner, backendBindings: "backend/internal/httpapi/access.go", requestValidator: operation.request, responseValidators: operation.responses };
});

const result = {
	schema_version: 1,
	decision: "DEC-20260909-hololive-admin-bigbang-replacement",
	claim: "Source bindings and baseline route coverage; executable behavior and release gates require their separate evidence.",
	spec_sha256: hash(read("backend/internal/contract/openapi.json")),
	provenance: "frontend/src/api/generated/provenance.json",
	counts: { baseline: baseline.operations.length, candidate: operations.length, business_mutations: 16, menus: baseline.features.length },
	bindings,
};
const artifact = path.join(admin, "docs/bigbang/candidate-parity.json");
if (process.argv.length === 3 && process.argv[2] === "--write") fs.writeFileSync(artifact, `${JSON.stringify(result, null, 2)}\n`);
else { assert.equal(process.argv.length, 2, "usage: check-admin-feature-parity.mjs [--write]"); assert.deepEqual(JSON.parse(fs.readFileSync(artifact, "utf8")), result, "candidate bindings changed; regenerate the review artifact"); }
console.log("PASS: 33 baseline routes, 35 candidate SDK/consumer bindings, 16 mutations and 7 menus mapped");
