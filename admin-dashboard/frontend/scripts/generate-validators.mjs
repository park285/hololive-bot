import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import standaloneCode from "ajv/dist/standalone/index.js";
import { build } from "vite";
import ts from "typescript";
import { createHash } from "node:crypto";

function groupFor(operation) {
  const route = operation.path;
  if (operation.access === "public_metadata" || route.includes("/auth/")) return `auth-${operation.id}`;
  if (route.includes("/docker/")) return "docker";
  if (route.endsWith("/openapi.json") || route.includes("/youtube/")) return "diagnostics";
  if (route.endsWith("/calendar")) return "calendar";
  if (route.includes("/members")) return "members";
  if (route.includes("/alarms") || route.includes("/names/")) return "alarms";
  if (route.includes("/rooms")) return "rooms";
  if (route.endsWith("/settings")) return "settings";
  if (route.includes("/streams/")) return "streams";
  if (route.endsWith("/status") || route.endsWith("/stats")) return "stats";
  throw new Error(`Unclassified validator group: ${operation.id}`);
}

function expandReferences(value, definitions, seen = new Set()) {
  if (Array.isArray(value)) return value.map(item => expandReferences(item, definitions, seen));
  if (value === null || typeof value !== "object") return value;
  if (Object.hasOwn(value, "$ref")) {
    assert.equal(Object.keys(value).length, 1, "reference siblings need an explicit assertion contract");
    assert.match(value.$ref, /^#\/\$defs\/[A-Za-z0-9_]+$/);
    const name = value.$ref.slice("#/$defs/".length);
    assert(Object.hasOwn(definitions, name) && !seen.has(name), `unresolved or cyclic assertion: ${name}`);
    return expandReferences(definitions[name], definitions, new Set([...seen, name]));
  }
  const expanded = Object.fromEntries(Object.entries(value).map(([key, item]) => [key, expandReferences(item, definitions, seen)]));
  if (expanded.format === "int32" || expanded.format === "int64") {
    const types = Array.isArray(expanded.type) ? expanded.type : [expanded.type];
    assert(types.includes("integer") && types.every(type => type === "integer" || type === "null"));
    // ajv-formats 3.0.1의 정수 판정과 int32 범위를 표준 integer/minimum/maximum으로 표현합니다.
    if (expanded.format === "int32") {
      expanded.minimum = Math.max(expanded.minimum ?? -2147483648, -2147483648);
      expanded.maximum = Math.min(expanded.maximum ?? 2147483647, 2147483647);
    }
    delete expanded.format;
  }
  return expanded;
}
function assertRuntime(code) {
  const source = ts.createSourceFile("standalone.mjs", code, ts.ScriptTarget.Latest, true, ts.ScriptKind.JS);
  const visit = node => {
    if ((ts.isCallExpression(node) || ts.isNewExpression(node)) && ts.isIdentifier(node.expression)) assert(!["require", "eval", "Function"].includes(node.expression.text), `forbidden runtime call: ${node.expression.text}`);
    ts.forEachChild(node, visit);
  };
  visit(source);
}

/** generateValidators는 operation별 모든 응답을 같은 기능 module에 묶어 전송 전에 검증 코드를 준비합니다. */
export async function generateValidators({ frontend, generated, schema, operations, typeNames }) {
  // UI는 오류 원문 대신 소유한 계약 오류를 표시하므로 사용하지 않는 영어 message 문자열을 출력하지 않습니다.
  const ajv = new Ajv2020({ strict: true, allErrors: false, messages: false, coerceTypes: false, useDefaults: false, removeAdditional: false, code: { source: true, esm: true, optimize: 2 } });
  addFormats(ajv);
  const groups = new Map(), operationGroups = {};
  for (const operation of operations) {
    const group = groupFor(operation); operationGroups[operation.id] = group;
    if (!groups.has(group)) groups.set(group, new Set());
    const names = groups.get(group); names.add(operation.request);
    for (const response of Object.values(operation.responses)) names.add(response.validator);
  }
  groups.set("system-stats", new Set(["SystemStats"]));
  // 모델의 직접 검증은 DTO/계약 시험용이며 production operation registry에서는 참조하지 않습니다.
  groups.set("models", new Set(typeNames));
  const output = path.join(generated, "validators"), source = await fs.mkdtemp(path.join(generated, ".validator-source-"));
  const modules = new Set();
  try {
    await fs.mkdir(path.join(source, "assertions"));
    const assertions = new Map(), definitionFiles = new Map();
    const responseNames = new Set(operations.flatMap(operation => Object.values(operation.responses).map(response => response.validator)));
    async function emitAssertion(definition) {
      const expanded = expandReferences(definition, schema.$defs);
      const identity = createHash("sha256").update(JSON.stringify(expanded)).digest("hex");
      const file = `assertions/${identity}.mjs`;
      if (assertions.has(identity)) return file;
      // 이중 not은 같은 boolean 판정을 유지하며 사용하지 않는 내부 오류 상세를 생성하지 않습니다.
      const assertion = { $id: `urn:hololive:admin:assertion:${identity}`, not: { not: expanded } };
      ajv.addSchema(assertion);
      await fs.writeFile(path.join(source, file), standaloneCode(ajv, { check: assertion.$id }));
      assertions.set(identity, file);
      return file;
    }
    for (const [name, definition] of Object.entries(schema.$defs)) {
      if (!responseNames.has(name)) { definitionFiles.set(name, await emitAssertion(definition)); continue; }
      // 소유한 response envelope의 검증과 body 검증을 모두 수행하되 같은 header 코드를 공유합니다.
      assert.deepEqual(Object.keys(definition.properties), ["headers", "body"]);
      const envelope = await emitAssertion({ ...definition, properties: { ...definition.properties, body: true } });
      const body = await emitAssertion(definition.properties.body);
      const file = `assertions/${name}.mjs`;
      await fs.writeFile(path.join(source, file), `import { check as envelope } from "./${path.basename(envelope)}";\nimport { check as body } from "./${path.basename(body)}";\nexport function check(value) { return envelope(value) && body(value.body); }\n`);
      definitionFiles.set(name, file);
    }
    const entries = {};
    for (const [group, names] of groups) {
      const file = path.join(source, `${group}.mjs`);
      await fs.writeFile(file, [...names].map(name => `export { check as ${name} } from "./${definitionFiles.get(name)}";`).join("\n"));
      entries[group] = file;
    }
    const result = await build({
      configFile: false, root: frontend, logLevel: "silent",
      plugins: [{ name: "validator-module-evidence", generateBundle() { for (const id of this.getModuleIds()) modules.add(id); } }],
      build: { lib: { entry: entries, formats: ["es"] }, target: "es2024", minify: true, write: false,
        rolldownOptions: { experimental: { attachDebugInfo: "none" }, output: { entryFileNames: "[name].mjs", chunkFileNames: "shared-[hash].mjs" } } },
    });
    const files = (Array.isArray(result) ? result : [result]).flatMap(bundle => bundle.output);
    assert(![...modules].some(file => /\/ajv\/dist\/(compile|core|2020|ajv)(\/|\.)/.test(file)), "Ajv compiler entered runtime");
    for (const file of files) { assert.equal(file.type, "chunk"); assertRuntime(file.code); }
    await fs.rm(output, { recursive: true, force: true }); await fs.mkdir(output, { recursive: true });
    for (const file of files) await fs.writeFile(path.join(output, file.fileName), file.code);
    for (const [group, names] of groups) {
      const models = [...names].filter(name => typeNames.includes(name));
      const declarations = ["// Generated by scripts/generate-validators.mjs. Do not edit.",
        ...(models.length ? [`import type { ${models.join(", ")} } from "../data-contracts";`] : []),
        "type Validator = ((value: unknown) => boolean) & { errors?: unknown };",
        ...[...names].map(name => models.includes(name) ? `export const ${name}: ((value: unknown) => value is ${name}) & { errors?: unknown };` : `export const ${name}: Validator;`),
      ];
      await fs.writeFile(path.join(output, `${group}.d.mts`), declarations.join("\n") + "\n");
    }
    const runtimeGroups = [...new Set(Object.values(operationGroups))];
    await fs.writeFile(path.join(generated, "validation.ts"), [
      "// Generated by scripts/generate-validators.mjs. Do not edit.",
      "export type Validator = ((value: unknown) => boolean) & { errors?: unknown };",
      "export type ValidationModule = Record<string, Validator>;",
      `const groups: Record<string, string> = ${JSON.stringify(operationGroups, null, 2)};`,
      "const loaders: Record<string, () => Promise<ValidationModule>> = {",
      ...runtimeGroups.map(group => `  ${JSON.stringify(group)}: () => import(${JSON.stringify(`./validators/${group}.mjs`)}),`), "};",
      "/** loadOperationValidators는 해당 operation의 입력·모든 응답 validator를 함께 불러오며 재시도하지 않습니다. */",
      "export function loadOperationValidators(operationId: string): Promise<ValidationModule> {",
      "  const group = groups[operationId]; const load = group === undefined ? undefined : loaders[group];",
      '  if (!load) throw new Error("Unclassified operation validation module");', "  return load();", "}", "",
    ].join("\n"));
    await fs.rm(path.join(generated, "validators.mjs"), { force: true });
    await fs.rm(path.join(generated, "validators.d.mts"), { force: true });
    return { runtimeHelpers: [...modules].filter(file => /\/node_modules\/ajv(?:-formats)?\//.test(file)).map(file => file.slice(file.indexOf("node_modules/"))).sort(), validationMode: "boolean assertion; local acyclic refs expanded; not(not(schema)); no detailed error payload", assertionCount: assertions.size, validationGroups: runtimeGroups, validationFiles: files.map(file => `validators/${file.fileName}`).sort() };
  } finally { await fs.rm(source, { recursive: true, force: true }); }
}
