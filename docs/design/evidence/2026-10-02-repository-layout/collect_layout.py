#!/usr/bin/env python3
"""One-off package graph evidence; not a repository gate or test."""
import argparse
from collections import deque
import csv
import json
import os
from pathlib import Path
import subprocess
import time


def json_stream(raw):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(raw):
        while position < len(raw) and raw[position].isspace():
            position += 1
        if position == len(raw):
            return
        item, position = decoder.raw_decode(raw, position)
        yield item


def normalized_import(path):
    return path.split(" [", 1)[0]


def write_tsv(path, columns, rows):
    with path.open("w", newline="") as stream:
        writer = csv.writer(stream, delimiter="\t")
        writer.writerow(columns)
        writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--go", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root, out = args.root.resolve(), args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    modules = ["hololive-api", "hololive-alarm-worker", "hololive-youtube-collector", "hololive-shared", "hololive-dbtest"]
    fields = "ImportPath,Dir,Module,GoFiles,CgoFiles,IgnoredGoFiles,TestGoFiles,XTestGoFiles,Imports,TestImports,XTestImports,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Error,DepsErrors"
    env = dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off", GOSUMDB="off", GOFLAGS="-mod=readonly")
    profiles = [("workspace-default-tests", {}, ["-test"], root, [f"./hololive/{m}/..." for m in modules]),
                ("workspace-integration-tests", {}, ["-test", "-tags=integration"], root, [f"./hololive/{m}/..." for m in modules]),
                ("workspace-arm64-production", {"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "arm64"}, [], root, [f"./hololive/{m}/..." for m in modules[:3]])]
    entries = {"hololive-api": ["./cmd/hololive-api", "./cmd/db-migrate", "./cmd/source-observation-replay-epoch", "./cmd/healthcheck"],
               "hololive-alarm-worker": ["./cmd/alarm-worker", "./cmd/healthcheck"],
               "hololive-youtube-collector": ["./cmd/runtime/youtube-collector", "./cmd/runtime/healthcheck", "./cmd/po-broker"]}
    for module, targets in entries.items():
        profiles.append((f"canonical-{module}", {"GOWORK": "off", "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "arm64"}, [], root / "hololive" / module, targets))
    summaries, graphs = [], {}
    for name, extraenv, options, cwd, patterns in profiles:
        start = time.monotonic()
        command = [args.go, "list", "-deps", "-e", "-json=" + fields, *options, *patterns]
        result = subprocess.run(command, cwd=cwd, env={**env, **extraenv}, capture_output=True, text=True, timeout=120)
        packages = list(json_stream(result.stdout))
        errors = [{"package": p["ImportPath"], "error": p.get("Error"), "dependency_errors": p.get("DepsErrors", [])} for p in packages if p.get("Error") or p.get("DepsErrors")]
        own = [p for p in packages if p.get("Dir", "").startswith(str(root) + "/")]
        summary = {"name": name, "command": command, "cwd": str(cwd.relative_to(root)) or ".", "environment": {k: ({**env, **extraenv})[k] for k in ["GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "GOFLAGS", *extraenv]}, "exit_code": result.returncode, "elapsed_seconds": round(time.monotonic() - start, 3), "loaded_packages": len(packages), "repository_packages_including_test_variants": len(own), "packages_with_errors": len(errors), "errors": errors, "stderr": result.stderr}
        summaries.append(summary)
        graphs[name] = {p["ImportPath"]: p for p in own if " [" not in p["ImportPath"] and not p["ImportPath"].endswith(".test")}
        print(name, "packages", len(packages), "errors", len(errors), flush=True)
    base = graphs["workspace-default-tests"]
    rows = []
    for imp, p in sorted(base.items()):
        directory = Path(p["Dir"]).relative_to(root)
        rows.append([imp, directory, p.get("Module", {}).get("Path", ""), len(p.get("GoFiles", []) + p.get("CgoFiles", [])), len(p.get("TestGoFiles", [])), len(p.get("XTestGoFiles", [])), len(p.get("EmbedFiles", [])), len(p.get("IgnoredGoFiles", []))])
    write_tsv(out / "packages.tsv", ["import_path", "directory", "module", "selected_non_test_go", "same_package_test_go", "external_test_go", "embedded_files", "ignored_go"], rows)
    rows = []
    for imp, p in sorted(base.items()):
        for key, label in [("Imports", "production"), ("TestImports", "same_package_test"), ("XTestImports", "external_test")]:
            for target in sorted(set(p.get(key, []))):
                target = normalized_import(target)
                if target in base:
                    rows.append([imp, target, label])
    write_tsv(out / "package_edges.tsv", ["importer", "imported", "kind"], rows)
    shared = {k for k in base if k.startswith("github.com/kapu/hololive-shared/")}
    graph = {k: {normalized_import(v) for v in p.get("Imports", []) if normalized_import(v) in base} for k, p in base.items()}
    ownership = {k: {"direct": [], "transitive": []} for k in shared}
    for module in modules[:3]:
        seeds = {k for k in base if k.startswith("github.com/kapu/" + module + "/") and base[k].get("GoFiles")}
        direct = set().union(*(graph[k] for k in seeds)) & shared
        seen, pending = set(seeds), list(seeds)
        while pending:
            for target in graph.get(pending.pop(), set()):
                if target not in seen:
                    seen.add(target)
                    pending.append(target)
        for k in shared:
            if k in direct:
                ownership[k]["direct"].append(module)
            if k in seen:
                ownership[k]["transitive"].append(module)
    write_tsv(out / "shared_consumers.tsv", ["package", "direct_runtime_consumers", "transitive_runtime_consumers"], [[k, ",".join(v["direct"]), ",".join(v["transitive"])] for k, v in sorted(ownership.items())])
    rows = []
    for imp, p in sorted(base.items()):
        directory = Path(p["Dir"]).relative_to(root)
        for key in ["EmbedFiles", "TestEmbedFiles", "XTestEmbedFiles"]:
            for item in p.get(key, []):
                rows.append([imp, str(directory / item), key])
    write_tsv(out / "embedded_assets.tsv", ["package", "asset", "kind"], rows)
    write_tsv(out / "profile_file_deltas.tsv", ["profile", "package", "file_kind", "added_files", "removed_files"], [[profile, imp, key, ",".join(sorted(set(p.get(key, [])) - set(base.get(imp, {}).get(key, [])))), ",".join(sorted(set(base.get(imp, {}).get(key, [])) - set(p.get(key, []))))] for profile, graphdata in graphs.items() if profile in {"workspace-integration-tests", "workspace-arm64-production"} for imp, p in sorted(graphdata.items()) for key in ["GoFiles", "TestGoFiles", "XTestGoFiles"] if set(p.get(key, [])) != set(base.get(imp, {}).get(key, []))])

    def reachable(start, removed_edges=()):
        seen, pending = {start}, [start]
        while pending:
            current = pending.pop()
            for target in graph.get(current, set()):
                if (current, target) not in removed_edges and target not in seen:
                    seen.add(target)
                    pending.append(target)
        return seen

    entrypoints = ["github.com/kapu/hololive-api/cmd/hololive-api", "github.com/kapu/hololive-api/cmd/source-observation-replay-epoch", "github.com/kapu/hololive-alarm-worker/cmd/alarm-worker", "github.com/kapu/hololive-youtube-collector/cmd/runtime/youtube-collector", "github.com/kapu/hololive-youtube-collector/cmd/po-broker"]
    write_tsv(out / "entrypoint_reachability.tsv", ["entrypoint", "reachable_repository_packages", "reachable_shared_packages", "shared_paths"], [[entry, len(reachable(entry)), len(reachable(entry) & shared), ",".join(sorted(reachable(entry) & shared))] for entry in entrypoints])
    collector = entrypoints[3]
    provider = "github.com/kapu/hololive-shared/pkg/providers"
    cuts = {(source, provider) for source in graph if source.startswith("github.com/kapu/hololive-youtube-collector/") and provider in graph[source]}
    before, after = reachable(collector), reachable(collector, cuts)
    target = "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
    queue, visited, path = deque([(collector, [collector])]), {collector}, None
    while queue:
        current, trail = queue.popleft()
        if current == target:
            path = trail
            break
        for dependency in sorted(graph.get(current, set())):
            if (current, dependency) not in cuts and dependency not in visited:
                visited.add(dependency)
                queue.append((dependency, trail + [dependency]))
    (out / "collector_provider_cut.json").write_text(json.dumps({"kind": "graph-only counterfactual, not measured compiled output", "removed_edges": sorted(cuts), "before_shared_packages": len(before & shared), "after_shared_packages": len(after & shared), "removed_shared_packages": sorted((before - after) & shared), "remaining_path_to_holodex": path}, ensure_ascii=False, indent=2) + "\n")
    metadata = {"head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(), "toolchain": subprocess.check_output([args.go, "version"], env=env, text=True).strip(), "profiles": summaries, "note": "Package loading only, not compilation or test execution. Working tree includes uncommitted changes; no dependencies were downloaded."}
    (out / "graph_results.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
