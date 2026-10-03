#!/usr/bin/env python3
"""Read project sources; reproduce layout constraints only in a fresh /tmp copy."""
from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(sys.argv[1]).resolve()
GO = Path(sys.argv[2]) if len(sys.argv) > 2 else Path("/home/kapu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.linux-amd64/bin/go")
WORK = Path(tempfile.mkdtemp(prefix="hololive-layout-reproduce-", dir="/tmp"))
ENV = dict(os.environ, GOWORK="off", GOPROXY="off", GOTOOLCHAIN="local",
           GOSUMDB="off", GOFLAGS="-mod=readonly", NPM_CONFIG_OFFLINE="true",
           NPM_CONFIG_AUDIT="false", NPM_CONFIG_FUND="false")
HELPER = ROOT / "hololive/hololive-youtube-collector/youtubejs"
print("temporary_workspace", WORK, flush=True)

def run(label, argv, cwd, expected=0, as_json=False):
    print("COMMAND", label, " ".join(str(x) for x in argv), flush=True)
    result = subprocess.run([str(x) for x in argv], cwd=cwd, env=ENV,
                            capture_output=True, text=True, timeout=90)
    print("EXIT", result.returncode, "EXPECTED", expected, flush=True)
    if as_json and result.returncode == 0:
        data = json.loads(result.stdout)
        print("DIRECT_IMPORTS", [x for x in data["Imports"] if x.startswith("github.com/kapu/")], flush=True)
        print("EMBED_FILES", data.get("EmbedFiles", []), flush=True)
    else:
        print(result.stdout + result.stderr, flush=True)
    if result.returncode != expected:
        raise RuntimeError(f"unexpected exit for {label}")
    return result

# The actual package.json test command, with a nested failing sentinel.
case = WORK / "node-discovery"
(case / "src/rpc").mkdir(parents=True)
test_script = json.loads((HELPER / "package.json").read_text())["scripts"]["test"]
(case / "package.json").write_text(json.dumps({
    "name": "layout-discovery-probe", "private": True, "type": "module",
    "scripts": {"test": test_script}
}))
(case / "src/top.test.mjs").write_text(
    "import test from 'node:test';\ntest('top-level selected', () => {});\n")
(case / "src/rpc/nested.test.mjs").write_text(
    "import test from 'node:test';\nimport assert from 'node:assert/strict';\n"
    "test('nested failure must be discovered', () => assert.fail('nested sentinel'));\n")
run("existing npm test silently skips nested failure", ["npm", "test"], case)
run("recursive test selection discovers nested failure",
    ["node", "--test", "--test-concurrency=1", "src/**/*.test.mjs"], case, expected=1)
run("flat artifact test pruning", ["sh", "-c", "rm -f ./src/*.test.mjs"], case)
print("REMAINING_TESTS", [str(p.relative_to(case)) for p in case.rglob("*.test.mjs")], flush=True)

# Exact project configs; a moved source escapes strict checking.
case = WORK / "type-discovery"
(case / "src/runtime").mkdir(parents=True)
for name in ("tsconfig.json", "jsconfig.json"):
    shutil.copy2(HELPER / name, case / name)
(case / "node_modules").symlink_to(HELPER / "node_modules", target_is_directory=True)
(case / "src/contracts.d.ts").write_text("export {};\n")
(case / "src/server.mjs").write_text(
    "// @ts-check\nexport function double(value) { return value * 2; }\n")
tsc = HELPER / "node_modules/.bin/tsc"
run("before move strict implicit-any failure",
    [tsc, "--noEmit", "--pretty", "false", "-p", "tsconfig.json"], case, expected=1)
run("before move relaxed excludes strict source",
    [tsc, "--noEmit", "--pretty", "false", "-p", "jsconfig.json"], case)
(case / "src/server.mjs").rename(case / "src/runtime/server.mjs")
run("after nested move strict no longer includes source",
    [tsc, "--noEmit", "--pretty", "false", "-p", "tsconfig.json"], case)
run("after nested move relaxed allows implicit any",
    [tsc, "--noEmit", "--pretty", "false", "-p", "jsconfig.json"], case)

BASE = WORK / "go/hololive"
for module in ("hololive-alarm-worker", "hololive-youtube-collector", "hololive-shared", "hololive-dbtest"):
    shutil.copytree(ROOT / "hololive" / module, BASE / module,
                    ignore=shutil.ignore_patterns("node_modules", ".git", ".env*", "*.env",
                                                 "bin", "bin-prod", "docs", "logs", "coverage"))
worker = BASE / "hololive-alarm-worker"
collector = BASE / "hololive-youtube-collector"

# The original actual interface is sealed by an unexported method.
probe = worker / "internal/egress/splitprobe"
probe.mkdir(parents=True)
(probe / "probe.go").write_text('''package splitprobe
import (
 "context"
 yt "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
 "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
 "github.com/kapu/hololive-shared/pkg/domain"
)
// SendEngine은 패키지 경계에서 비공개 계약을 구현할 수 없는 상황을 재현합니다.
type SendEngine struct{}
func (*SendEngine) dispatchDeliveryRows(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) dispatchstate.DispatchResult {
 return dispatchstate.DispatchResult{}
}
var _ yt.DeliveryExecutor = (*SendEngine)(nil)
''')
result = run("original DeliveryExecutor cannot cross package",
             [GO, "build", "./internal/egress/splitprobe"], worker, expected=1)
if "unexported method dispatchDeliveryRows" not in result.stderr:
    raise RuntimeError("expected sealed-method diagnostic missing")

# Move real alarm dispatch code, same-package tests, and all embedded queries.
old = worker / "internal/service/dispatchrun"
new = worker / "internal/egress/alarmdispatch"
old.rename(new)
for path in worker.rglob("*.go"):
    source = path.read_text()
    updated = source.replace(
        "github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun",
        "github.com/kapu/hololive-alarm-worker/internal/egress/alarmdispatch")
    if path.is_relative_to(new):
        updated = updated.replace("package dispatchrun", "package alarmdispatch")
    else:
        updated = updated.replace("dispatchrun.", "alarmdispatch.")
    if updated != source:
        path.write_text(updated)
path = new / "alarm_dispatch_render.go"
source = path.read_text().replace(
    '"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"',
    '"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/format"'
).replace("youtubedispatch.FormatYouTubeOutboxPayload", "format.FormatYouTubeOutboxPayload")
# Preserve the error-context layer previously added by the removed wrapper.
source = source.replace(
    'return out, fmt.Errorf("format youtube outbox payload: %w", err)',
    'return out, fmt.Errorf("format youtube outbox payload: %w", fmt.Errorf("format youtube outbox payload: %w", err))')
path.write_text(source)
run("moved alarmdispatch and workerapp production compile",
    [GO, "build", "./internal/egress/alarmdispatch", "./internal/app/workerapp"], worker)
run("moved alarmdispatch tests compile without running",
    [GO, "test", "-c", "-o", WORK / "alarmdispatch.test", "./internal/egress/alarmdispatch"], worker)
run("alarmdispatch direct imports and embedded queries",
    [GO, "list", "-json", "./internal/egress/alarmdispatch"], worker, as_json=True)

# Move only RPC pagination interpretation and its existing regression to the provider.
util = collector / "internal/runtime/collectutil"
provider = collector / "internal/runtime/youtubejscollector"
path = util / "runner.go"
source = path.read_text()
start = source.index("func PaginationOf(")
end = source.index("\nfunc DefaultMaxResults", start)
function = source[start:end].replace("func PaginationOf", "func paginationOf")
source = (source[:start] + source[end:]).replace(
    '\n\t"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"', "")
path.write_text(source)
(provider / "pagination.go").write_text('''package youtubejscollector
import (
 contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
 "github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
 "github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)
''' + function + "\n")
path = util / "runner_test.go"
source = path.read_text()
start = source.index("func TestPaginationOfRejectsImpossibleTupleAsProtocolFault")
end = source.index("\nfunc ", start + 5)
test = source[start:end].replace("PaginationOf(", "paginationOf(")
source = (source[:start] + source[end:]).replace(
    '\n\t"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"', "")
path.write_text(source)
(provider / "pagination_test.go").write_text('''package youtubejscollector
import (
 "testing"
 "github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
 "github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)
''' + test + "\n")
for name in ("channel.go", "content.go", "community.go"):
    path = provider / name
    path.write_text(path.read_text().replace("collectutil.PaginationOf(", "paginationOf("))
run("pagination moved production compile",
    [GO, "build", "./internal/runtime/collectutil", "./internal/runtime/youtubejscollector"], collector)
run("collectutil test compilation",
    [GO, "test", "-c", "-o", WORK / "collectutil.test", "./internal/runtime/collectutil"], collector)
run("provider test compilation",
    [GO, "test", "-c", "-o", WORK / "youtubejscollector.test", "./internal/runtime/youtubejscollector"], collector)
run("moved existing impossible tuple regression",
    [WORK / "youtubejscollector.test", "-test.run", "^TestPaginationOfRejectsImpossibleTupleAsProtocolFault$", "-test.v"], provider)
run("collectutil imports after move",
    [GO, "list", "-json", "./internal/runtime/collectutil"], collector, as_json=True)

# Moving repository helper source preserves relative ESM imports but breaks old Go fixture paths.
source = collector / "youtubejs"
destination = collector / "helpers/youtubejs"
destination.parent.mkdir()
source.rename(destination)
print("OLD_GO_HELPER_TEST_SOURCE_EXISTS",
      (collector / "internal/runtime/youtubejs/../../../youtubejs/src/server.mjs").resolve().is_file(), flush=True)
print("NEW_HELPER_SOURCE_EXISTS", (destination / "src/server.mjs").is_file(), flush=True)
print("OLD_PROTOCOL_FIXTURE_EXISTS",
      (collector / "internal/runtime/youtubejs/../../../youtubejs/testdata/pagination-tuples.json").resolve().is_file(), flush=True)
print("NEW_PROTOCOL_FIXTURE_EXISTS", (destination / "testdata/pagination-tuples.json").is_file(), flush=True)
run("moved actual helper entrypoint syntax only", ["node", "--check", destination / "src/server.mjs"], collector)
print("ALL_PROBES_COMPLETED; no production source or runtime changed", flush=True)
