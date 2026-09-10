import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { generateValidators } from "./generate-validators.mjs";
import { generateApi } from "swagger-typescript-api";

const frontend = fileURLToPath(new URL("../", import.meta.url));
const contract = path.resolve(frontend, "../backend/internal/contract");
const generated = path.join(frontend, "src/api/generated");
const spec = JSON.parse(await fs.readFile(path.join(contract, "openapi.json"), "utf8"));
const generation = createHash("sha256").update(JSON.stringify(spec)).digest("hex");
const methods = new Set(["get", "post", "put", "patch", "delete", "head", "options"]);
const accesses = new Set(["public_metadata", "public_login", "session", "session_csrf", "session_csrf_audit"]);

// ASVS 2.2.1: 외부 참조·알 수 없는 형식은 생성 실패이며 검증 없는 경로를 만들지 않는다.
function schemaRefs(value) {
	if (Array.isArray(value)) return value.map(schemaRefs);
	if (typeof value !== "object" || value === null) return value;
	return Object.fromEntries(Object.entries(value).map(([key, item]) => {
		if (key === "example") return ["examples", [item]];
		if (key === "examples") return [key, item];
		if (key !== "$ref") return [key, schemaRefs(item)];
		assert.match(item, /^#\/components\/schemas\/[A-Za-z0-9_]+$/);
		const name = item.slice("#/components/schemas/".length);
		assert(Object.hasOwn(spec.components.schemas, name), `unresolved schema: ${item}`);
		return [key, `#/$defs/${name}`];
	}));
}

function parametersSchema(parameters, location) {
	const selected = parameters.filter(parameter => parameter.in === location);
	const name = parameter => location === "header" ? parameter.name.toLowerCase() : parameter.name;
	return {
		type: "object",
		properties: Object.fromEntries(selected.map(parameter => [name(parameter), schemaRefs(parameter.schema)])),
		required: selected.filter(parameter => parameter.required).map(name),
		additionalProperties: false,
	};
}

const definitions = Object.fromEntries(Object.entries(spec.components.schemas).map(([name, schema]) => [name, schemaRefs(schema)]));
const operations = [];
const outputs = new Map();
const names = new Set();

for (const [route, pathItem] of Object.entries(spec.paths)) {
	for (const [method, operation] of Object.entries(pathItem)) {
		if (!methods.has(method)) continue;
		const id = operation.operationId;
		assert.match(id, /^[A-Za-z][A-Za-z0-9_]*$/);
		assert(!names.has(id), `duplicate operation: ${id}`);
		names.add(id);
		assert(accesses.has(operation["x-admin-access"]), `unclassified route: ${method} ${route}`);
		const parameters = [...(pathItem.parameters ?? []), ...(operation.parameters ?? [])];
		assert(parameters.every(parameter => ["path", "query", "header"].includes(parameter.in)));
		const body = operation.requestBody;
		if (body) assert.deepEqual(Object.keys(body.content), ["application/json"]);
		const requestName = `request_${id}`;
		definitions[requestName] = {
			type: "object", additionalProperties: false,
			required: ["path", "query", "headers", ...(body?.required ? ["body"] : [])],
			properties: {
				path: parametersSchema(parameters, "path"), query: parametersSchema(parameters, "query"),
				headers: parametersSchema(parameters, "header"), body: body ? schemaRefs(body.content["application/json"].schema) : false,
			},
		};
		const responses = {};
		for (const [status, response] of Object.entries(operation.responses)) {
			assert.match(status, /^[1-5][0-9]{2}$/);
			assert.deepEqual(Object.keys(response.content), ["application/json"]);
			const shape = {
				type: "object", additionalProperties: false, required: ["headers", "body"],
				properties: {
					headers: {
						type: "object", additionalProperties: false,
						required: Object.keys(response.headers ?? {}).map(name => name.toLowerCase()),
						properties: Object.fromEntries(Object.entries(response.headers ?? {}).map(([name, header]) => [name.toLowerCase(), schemaRefs(header.schema)])),
					},
					body: schemaRefs(response.content["application/json"].schema),
				},
			};
			const hash = createHash("sha256").update(JSON.stringify(shape)).digest("hex").slice(0, 16);
			const responseName = `response_${hash}`;
			outputs.set(responseName, shape);
			responses[status] = { validator: responseName, contentType: "application/json" };
		}
		operations.push({ id, method: method.toUpperCase(), path: route, access: operation["x-admin-access"], mutation: operation["x-admin-mutation"], request: requestName, responses });
	}
}

for (const [name, shape] of outputs) definitions[name] = shape;
const schema = { $id: "urn:hololive:admin:contract", $schema: "https://json-schema.org/draft/2020-12/schema", $defs: definitions };
await fs.mkdir(generated, { recursive: true });
const typeNames = Object.keys(spec.components.schemas);
const validatorBuild = await generateValidators({ frontend, generated, schema, operations, typeNames });

const sdkSpec = structuredClone(spec);
// 인증·세대 header는 transport만 소유하며 호출자가 값이나 요청 경로를 덮어쓸 수 없다.
for (const item of Object.values(sdkSpec.paths)) for (const operation of Object.values(item)) {
	if (operation.parameters) operation.parameters = operation.parameters.filter(parameter => parameter.in !== "header");
}
await generateApi({ spec: sdkSpec, output: generated, templates: path.join(frontend, "scripts/templates"), modular: true, httpClientType: "axios", singleHttpClient: true, enumStyle: "const", sortTypes: true, sortRoutes: true, silent: true });

await fs.writeFile(path.join(generated, "operation-contracts.json"), `${JSON.stringify(operations, null, 2)}\n`);
// JSON은 검사 inventory이며 runtime은 동일 response 선언을 공유하여 중복 문자열 전송을 줄입니다.
const responseDeclarations = new Map();
const commonResponses = Object.fromEntries(Object.entries(operations[0].responses).filter(([status, response]) => operations.every(operation => JSON.stringify(operation.responses[status]) === JSON.stringify(response))));
const responseReference = response => {
	const key = JSON.stringify(response);
	if (!responseDeclarations.has(key)) responseDeclarations.set(key, `response${responseDeclarations.size}`);
	return responseDeclarations.get(key);
};
const commonResponseSource = Object.entries(commonResponses).map(([status, response]) => `${JSON.stringify(status)}: ${responseReference(response)}`).join(", ");
const runtimeOperations = operations.map(operation => {
	const responses = Object.entries(operation.responses).filter(([status]) => !Object.hasOwn(commonResponses, status)).map(([status, response]) => `${JSON.stringify(status)}: ${responseReference(response)}`);
	assert.equal(operation.request, `request_${operation.id}`);
	const metadata = [operation.id, operation.method, operation.path, operation.access, operation.mutation];
	return `[${metadata.map(value => JSON.stringify(value)).join(", ")}, { ...commonResponses, ${responses.join(", ")} }]`;
});
await fs.writeFile(path.join(generated, "operation-contracts.ts"), [
	"// Generated by scripts/generate-api.mjs. Do not edit.",
	"type ResponseContract = { validator: string; contentType: string };",
	"type OperationRow = [id: string, method: string, path: string, access: string, mutation: boolean, responses: Record<string, ResponseContract>];",
	...[...responseDeclarations].map(([declaration, name]) => `const ${name} = ${declaration};`),
	`const commonResponses = { ${commonResponseSource} };`,
	`const rows: OperationRow[] = [\n${runtimeOperations.join(",\n")}\n];`,
	"export default rows.map(([id, method, path, access, mutation, responses]) => ({ id, method, path, access, mutation, request: `request_${id}`, responses }));", "",
].join("\n"));
await fs.writeFile(path.join(generated, "schema-bundle.json"), `${JSON.stringify(schema, null, 2)}\n`);
await fs.writeFile(path.join(generated, "generation.ts"), `// Generated by scripts/generate-api.mjs. Do not edit.\nexport const CLIENT_GENERATION = "${generation}";\n`);
const goRoutes = operations.map(operation => `\t{Method: ${JSON.stringify(operation.method)}, Path: ${JSON.stringify(operation.path)}, ID: ${JSON.stringify(operation.id)}, Access: ${JSON.stringify(operation.access)}, Mutation: ${operation.mutation}},`).join("\n");
await fs.writeFile(path.join(contract, "operations_generated.go"), `// Code generated by frontend/scripts/generate-api.mjs; DO NOT EDIT.\npackage contract\n\n// Generation은 BFF와 브라우저가 공유하는 계약 내용의 SHA-256입니다.\nconst Generation = "${generation}"\n\nvar operations = []Operation{\n${goRoutes}\n}\n`);
execFileSync("gofmt", ["-w", path.join(contract, "operations_generated.go")]);
await fs.writeFile(path.join(generated, "provenance.json"), `${JSON.stringify({ generation, operationCount: operations.length, validatorCount: Object.keys(definitions).length, ...validatorBuild, compilerInRuntime: false }, null, 2)}\n`);
console.log(`Generated ${operations.length} operations, ${Object.keys(definitions).length} validators; generation ${generation}`);
