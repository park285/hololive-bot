import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const admin = path.join(root, "admin-dashboard"), frontend = path.join(admin, "frontend");
const require = createRequire(path.join(frontend, "package.json")), ts = require("typescript");
const hash = value => createHash("sha256").update(value).digest("hex");
const read = relative => fs.readFileSync(path.join(admin, relative), "utf8");
const manifest = JSON.parse(read("docs/bigbang/retirement.json"));
const args = process.argv.slice(2);
assert(args.length === 1 && args[0] === "--source-bundle" || args.length === 2 && args[0] === "--image" && /^sha256:[a-f0-9]{64}$/.test(args[1]), "usage: check-admin-retirement.sh --source-bundle | --image sha256:<64 hex>");
const walk = directory => fs.existsSync(directory) ? fs.readdirSync(directory, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? walk(path.join(directory, entry.name)) : [path.join(directory, entry.name)]) : [];
const replacements = new Set(["backend/internal/config/config.go", "frontend/src/api/client.ts", "frontend/src/api/generated/Admin.ts", "frontend/src/api/generated/data-contracts.ts", "frontend/src/api/generated/http-client.ts", "frontend/src/features/stats/lib/systemStats.ts", "frontend/src/features/streams/api.ts"]);
const retired = manifest.entries.flatMap(entry => entry.old_owners).filter(owner => !replacements.has(owner) && !owner.includes("#"));
for (const owner of retired) {
  const location = path.join(admin, owner);
  assert(owner.endsWith("/") ? walk(location).length === 0 : !fs.existsSync(location), `retired owner remains: ${owner}`);
}
const config = ts.readConfigFile(path.join(frontend, "tsconfig.app.json"), ts.sys.readFile);
assert(!config.error, "tsconfig read failed");
const parsed = ts.parseJsonConfigFileContent(config.config, ts.sys, frontend);
assert.equal(parsed.errors.length, 0);
const sourceFiles = walk(path.join(frontend, "src")).filter(file => /\.(?:tsx?|mjs)$/.test(file) && !/\.test\./.test(file));
const imports = [];
let axiosOwners = 0;
for (const file of sourceFiles) {
  const source = fs.readFileSync(file, "utf8"), relative = path.relative(frontend, file);
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true);
  const visit = node => {
    if (ts.isCallExpression(node) && node.expression.getText(ast) === "axios.create") { assert.equal(relative, "src/api/client.ts"); axiosOwners++; }
    if (ts.isPropertyAccessExpression(node)) assert.notEqual(node.name.text, "interceptors", `obsolete interceptor path: ${relative}`);
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
      const specifier = node.moduleSpecifier;
      if (specifier && ts.isStringLiteral(specifier)) {
        const name = specifier.text;
        if (name.startsWith("@/") || name.startsWith(".")) {
          if (!/\.(?:css|svg|png|jpg)(?:\?|$)/.test(name)) {
            const resolved = ts.resolveModuleName(name, file, parsed.options, ts.sys).resolvedModule;
            assert(resolved, `unresolved import ${relative} → ${name}`);
            const target = path.relative(admin, resolved.resolvedFileName);
            assert(!retired.some(old => target === old || old.endsWith("/") && target.startsWith(old)), `retired import: ${target}`);
            if (/^src\/api\/(?:client|transport|errors)\.ts$/.test(relative)) assert(!/^frontend\/src\/(?:app|session|stores|hooks|pages|layouts)\//.test(target), `transport depends on UI state: ${target}`);
            imports.push({ from: relative, to: target });
          }
        }
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(ast);
}
assert.equal(axiosOwners, 1, "exactly one injected Axios constructor required");
const goPackages = execFileSync("go", ["list", "-deps", "./cmd/admin-dashboard"], { cwd: path.join(admin, "backend"), encoding: "utf8" });
assert(!/github\.com\/kapu\/admin-dashboard\/internal\/(?:app|holo|docker|status|openapi)(?:\n|$)/m.test(goPackages), "old Go package still linked");
const backendSource = walk(path.join(admin, "backend/internal")).filter(file => file.endsWith(".go") && !file.endsWith("_test.go")).map(file => fs.readFileSync(file, "utf8")).join("\n");
assert(!/normalizeLegacySession|sessionStreamFamilyID|os\.(?:Setenv|Unsetenv)|ProxyGet|ProxyMutation/.test(backendSource), "retired backend behavior remains");
assert(!read("backend/internal/httpapi/streams.go").includes("return sess.ID"), "WS family fallback remains");
assert(!/axios\.create|\.instance\s*=/.test(read("frontend/src/api/generated/http-client.ts")), "generated client owns a second transport");
assert(!/Array\.isArray/.test(read("frontend/src/features/streams/api.ts")), "malformed streams must not become empty success");
assert(read("frontend/src/features/stats/lib/systemStats.ts").includes("validateSystemStats(value)"));
assert(!/total_goroutines|normalizeLegacy/.test(read("frontend/src/features/stats/lib/systemStats.ts")), "legacy stats coercion remains");
const build = JSON.parse(read("frontend/node_modules/.cache/admin-build-inventory.json"));
assert.equal(build.schema_version, 1);
for (const [file, digest] of Object.entries(build.inputs)) assert.equal(hash(fs.readFileSync(path.resolve(frontend, file))), digest, `stale build input: ${file}`);
const modules = [...new Set(Object.values(build.files).flatMap(file => file.modules))];
assert(modules.includes("src/app/bootstrap.ts") && modules.includes("src/session/state.ts"), "current owners missing from production bundle");
for (const module of modules) {
  assert(!retired.some(old => `frontend/${module}` === old || old.endsWith("/") && `frontend/${module}`.startsWith(old)), `old bundle module: ${module}`);
  assert(!/node_modules\/ajv\/dist\/(?:compile|core|ajv)|node_modules\/ajv-formats|\.test\./.test(module), `build-only module bundled: ${module}`);
}
assert(!fs.existsSync(path.join(frontend, "dist/mockServiceWorker.js")), "development service worker shipped");
for (const [file, info] of Object.entries(build.files)) assert.equal(hash(fs.readFileSync(path.join(frontend, "dist", file))), info.sha256, `bundle hash mismatch: ${file}`);
const report = {
  schema_version: 1, decision: manifest.decision,
  generation: JSON.parse(read("frontend/src/api/generated/provenance.json")).generation,
  spec_sha256: hash(read("backend/internal/contract/openapi.json")),
  policy_sha256: hash(JSON.stringify(JSON.parse(read("backend/internal/contract/docker-policy.json")))),
  entries: manifest.entries.map(entry => entry.id),
  source: { status: "PASS", files: sourceFiles.length, imports: imports.length, graph_sha256: hash(JSON.stringify(imports)) },
  bundle: { status: "PASS", files: Object.keys(build.files).length, modules: modules.length, inventory_sha256: hash(read("frontend/node_modules/.cache/admin-build-inventory.json")) },
  image: { status: "NOT RUN" },
};
if (args[0] === "--image") {
  const imageID = args[1], directory = fs.mkdtempSync(path.join(os.tmpdir(), "admin-retirement-"));
  let container;
  try {
    const metadata = JSON.parse(execFileSync("docker", ["image", "inspect", imageID, "--format", '{"id":{{json .Id}},"architecture":{{json .Architecture}},"revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}},"fixture":{{json (index .Config.Labels "dev.iris.local-fixture")}}}'], { encoding: "utf8" }));
    assert.equal(metadata.id, imageID);
    container = execFileSync("docker", ["create", "--platform", `linux/${metadata.architecture}`, "--network", "none", "--label", "iris.task=admin-retirement", imageID], { encoding: "utf8" }).trim();
    assert(/^[a-f0-9]{64}$/.test(container));
    const archive = path.join(directory, "rootfs.tar"), fd = fs.openSync(archive, "wx", 0o600);
    try { execFileSync("docker", ["export", container], { stdio: ["ignore", fd, "pipe"] }); } finally { fs.closeSync(fd); }
    const files = execFileSync("tar", ["-tf", archive], { encoding: "utf8" }).trim().split("\n").sort();
    assert.deepEqual(files.filter(file => file.startsWith("app/") && !file.endsWith("/")), ["app/bin/admin-dashboard", "app/bin/healthcheck", "app/licenses/ajv.txt"]);
    const license = execFileSync("tar", ["-xOf", archive, "app/licenses/ajv.txt"], { maxBuffer: 64 * 1024 });
    assert.deepEqual(license, fs.readFileSync(path.join(frontend, "node_modules/ajv/LICENSE")), "pinned runtime helper notice must accompany the image");
    assert(!files.some(file => !file.endsWith("/") && /(?:^|\/)(?:\.git|node_modules|openapi|swagger\.json|src|backend|frontend)(?:\/|$)/.test(file)), "source or old runtime files in image");
    const binary = execFileSync("tar", ["-xOf", archive, "app/bin/admin-dashboard"], { maxBuffer: 128 * 1024 * 1024 });
    const symbols = binary.toString("latin1");
    assert(!/github\.com\/kapu\/admin-dashboard\/internal\/(?:app|holo|docker|status|openapi)[./]|normalizeLegacySession|sessionStreamFamilyID/.test(symbols), "old Go execution path in image binary");
    for (const [file, info] of Object.entries(build.files)) {
      const content = fs.readFileSync(path.join(frontend, "dist", file));
      assert(binary.includes(content), `image does not contain verified asset bytes: ${file} (${info.sha256})`);
    }
    report.image = { status: "PASS", ...metadata, binary_sha256: hash(binary), files, filesystem_inventory_sha256: hash(files.join("\n")), matched_assets: Object.keys(build.files).length, runtime_helper_license_sha256: hash(license) };
  } finally {
    if (container) execFileSync("docker", ["rm", container], { stdio: "ignore" });
    fs.rmSync(directory, { recursive: true, force: true });
  }
}
const destination = path.join(frontend, "node_modules/.cache/admin-retirement.json");
fs.writeFileSync(destination, `${JSON.stringify(report, null, 2)}\n`);
console.log(`PASS: ${manifest.entries.length} retirement entries, ${sourceFiles.length} source files, ${imports.length} imports, ${modules.length} bundle modules; image=${report.image.status}`);
console.log(`Evidence: ${destination}`);
