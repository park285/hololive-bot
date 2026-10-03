#!/usr/bin/env python3
"""Read-only lexical path impact inventory; not a CI gate or reachability proof."""
from __future__ import annotations

import argparse
import csv
import hashlib
import json
from pathlib import Path
import re
import subprocess
from collections import Counter

OUTER = {
    "hololive/hololive-api": "apps/api",
    "hololive/hololive-alarm-worker": "apps/alarm-worker",
    "hololive/hololive-youtube-collector": "apps/youtube-collector",
    "hololive/hololive-shared": "libs/hololive",
    "hololive/hololive-dbtest": "tests/dbtest",
}
INNER = {
    "hololive/hololive-api/internal/planes/bot/internal/app/runtime": "hololive/hololive-api/internal/planes/bot/runtime",
    "hololive/hololive-api/internal/planes/bot/internal/app/bootstrap": "hololive/hololive-api/internal/planes/bot/internal/bootstrap",
    "hololive/hololive-api/internal/planes/admin/app/http": "hololive/hololive-api/internal/planes/admin/internal/httpapi",
    "hololive/hololive-api/internal/planes/admin/app": "hololive/hololive-api/internal/planes/admin/runtime",
    "hololive/hololive-alarm-worker/internal/app/workerapp": "hololive/hololive-alarm-worker/internal/app",
    "hololive/hololive-alarm-worker/internal/service/workerruntime": "hololive/hololive-alarm-worker/internal/runtime",
    "hololive/hololive-youtube-collector/cmd/runtime/youtube-collector": "hololive/hololive-youtube-collector/cmd/youtube-collector",
    "hololive/hololive-youtube-collector/cmd/runtime/healthcheck": "hololive/hololive-youtube-collector/cmd/healthcheck",
}
SEMANTIC_SPLITS = {
    "hololive/hololive-shared/pkg/service/youtube/sourceobservation": "SEARCH-ONLY semantic split: retain shared public contracts; review collector Publish and API Consume per symbol; no whole-directory move",
}
MODULES = {p: "github.com/kapu/" + p.split("/")[-1] for p in OUTER}


def package_name(disk: str) -> str:
    for prefix, module in MODULES.items():
        if disk == prefix or disk.startswith(prefix + "/"):
            return module + disk[len(prefix):]
    raise ValueError(disk)


def classify(path: str, ignored: bool) -> str:
    if ignored:
        return "ignored-local"
    if path.startswith("docs/current/") or path in {"README.md", "AGENTS.md", "scripts/README.md", "admin-dashboard/README.md", "admin-dashboard/AGENTS.md", "runtime-config/README.md", "deploy/compose/README.md", "docs/README.md", "docs/PROJECT_MAP.md"}:
        return "current-doc"
    if path.startswith(("docs/history/", "docs/architecture/", "docs/handoff/", "docs/review/", "docs/runbook_execution/", "docs/decisions/", "admin-dashboard/docs/")) or path == "CHANGELOG.md":
        return "historical"
    if path.startswith("docs/design/"):
        return "proposal-doc"
    if path.endswith(".md"):
        return "current-doc"
    if path.endswith("_test.go") or "test" in Path(path).name or "/testdata/" in path:
        return "test-build"
    if path.startswith((".github/", "scripts/ci/", "scripts/build/", "scripts/architecture/", "scripts/perf/", "internal/workspace/", "../tools/lint/")) or "Dockerfile" in Path(path).name or Path(path).name == "Makefile" or path in {".golangci.yml", ".dockerignore", ".gitignore", "go.work", "go.mod", "build-all.sh"} or path.endswith("/go.mod"):
        return "test-build"
    return "runtime-active"


def safe_candidate(path: str) -> bool:
    p = Path(path)
    if any(part in {".git", "node_modules", "target", "artifacts", "evidence", "security-findings", "backups", "data", "logs"} for part in p.parts):
        return False
    if p.name.startswith(".env") or p.suffix in {".key", ".pem", ".crt", ".p12", ".pfx", ".sum", ".csv", ".png", ".ttf", ".zip", ".gz"}:
        return False
    if "package-lock" in p.name or "credentials" in p.name or "service-account" in p.name:
        return False
    return p.suffix in {".go", ".sh", ".py", ".mjs", ".js", ".ts", ".tsx", ".md", ".yml", ".yaml", ".txt", ".tsv", ".json", ".toml", ".conf", ".nft", ".service", ".timer", ".socket", ".dockerignore"} or p.name in {"go.work", "go.mod", "Makefile", "Dockerfile", "Dockerfile.po-sandbox", ".dockerignore", ".gitignore"}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root = args.root.resolve()
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    tracked_raw = subprocess.check_output(["git", "ls-files", "-z"], cwd=root)
    visible_raw = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root)
    tracked = {v.decode() for v in tracked_raw.split(b"\0") if v}
    visible = {v.decode() for v in visible_raw.split(b"\0") if v}
    # Explicitly named project-doc area omitted by default rg/Git ignore rules.
    ignored_docs = {str(p.relative_to(root)) for p in (root / "docs/agent-workflows").rglob("*.md") if p.is_file() and not p.is_symlink()}
    source_owners = {"../tools/lint/golangci.template.yml", "../tools/lint/golangci-repos.tsv", "../tools/lint/render-golangci.sh"}
    candidates = sorted(visible | ignored_docs | source_owners)
    rules = []
    for old, new in OUTER.items():
        rules.append(("outer-diskpath", old, new))
        rules.append(("module-identity", MODULES[old], MODULES[old]))
    for old, new in INNER.items():
        rules.append(("inner-diskpath", old, new))
        rules.append(("inner-importpath", package_name(old), package_name(new)))
    for old, placeholder in SEMANTIC_SPLITS.items():
        rules.append(("semantic-split-disk-reference", old, placeholder))
        rules.append(("semantic-split-import-reference", package_name(old), placeholder))
    rules += [
        ("inner-relative-command", "./cmd/runtime/youtube-collector", "./cmd/youtube-collector"),
        ("inner-relative-command", "./cmd/runtime/healthcheck", "./cmd/healthcheck"),
        ("inner-relative-command", "./cmd/runtime/$(BINARY_NAME)", "./cmd/$(BINARY_NAME)"),
        ("inner-test-identity-suffix", "/cmd/runtime/healthcheck", "/cmd/healthcheck"),
    ]
    generic = {
        "outer-root-selector": re.compile(r"hololive/\*|(?:\$\{?[A-Za-z_]+\}?|ROOT_DIR|root)/hololive(?:[\"/ ]|$)|['\"]hololive/['\"]|\^\(hololive/"),
        "module-relative-replace": re.compile(r"^replace github\.com/kapu/hololive-.*=> \.\./"),
        "relative-root-depth": re.compile(r"(?:\.\./){2,}|CURDIR.*\.\./\.\.|GOLANGCI_CONFIG.*\.\./\.\.|repoRootMarker|migrationsRelDir|runtime\.Caller|findRepoRoot"),
        "package-local-asset": re.compile(r"go:embed|filepath\.Join\([^\n]*(?:testdata|epoch1_migrations)|(?:ReadFile|Open)\([\"'](?:testdata|queries|patterns)/"),
        "workspace-discovery": re.compile(r"DiskPath|go work edit -json|GO_WORKSPACE_MODULES|GO_MODULES=|go_workspace_package_patterns|is_workspace_module_file"),
    }
    rows = []
    corpus = []
    for path in candidates:
        if not safe_candidate(path):
            continue
        p = root / path
        if not p.is_file() or p.is_symlink():
            continue
        raw = p.read_bytes()
        if b"\0" in raw or len(raw) > 2_000_000:
            continue
        try:
            content = raw.decode("utf-8")
        except UnicodeDecodeError:
            continue
        ignored = path not in visible and path not in source_owners
        category = classify(path, ignored)
        provenance = "external-source-owner" if path in source_owners else ("tracked" if path in tracked else ("ignored-local" if ignored else "untracked-visible"))
        corpus.append({"file": path, "sha256": hashlib.sha256(raw).hexdigest(), "provenance": provenance, "bytes": len(raw)})
        for line_num, line in enumerate(content.splitlines(), 1):
            for kind, old, new in rules:
                # Physical spelling often occurs as the suffix of a Go import; do not conflate them.
                if old not in line:
                    continue
                if kind.startswith("semantic-split-"):
                    required = "search-only per-symbol review; not a whole-package move instruction"
                elif kind == "module-identity":
                    required = "no-for-outer-only; yes-if-package-prefix-moved"
                elif kind.startswith("inner-"):
                    required = "yes-for-matched-inner-candidate"
                else:
                    required = "yes-for-outer-move"
                if category in {"historical", "ignored-local"}:
                    required = "preserve-evidence; no-automatic-path-rewrite"
                elif category in {"current-doc", "proposal-doc", "module-doc"}:
                    required = "update-current-navigation-if-move-approved; not-runtime-consumer"
                rows.append({"category": category, "provenance": provenance, "file": path, "line": line_num, "kind": kind, "old": old, "candidate_new": new, "required": required, "snippet": line.strip()[:600]})
            for kind, pattern in generic.items():
                if pattern.search(line):
                    required = {
                        "workspace-discovery": "no-after-go.work-update; verify-selection",
                        "module-relative-replace": "yes-if-relative-neighbor-layout-changes",
                        "relative-root-depth": "review-depth; unchanged-for-two-level-outer-layout",
                        "package-local-asset": "move-asset-with-package; review-relative-fixtures",
                        "outer-root-selector": "yes-for-outer-move",
                    }[kind]
                    if category in {"historical", "ignored-local"}:
                        required = "preserve-evidence; no-automatic-path-rewrite"
                    rows.append({"category": category, "provenance": provenance, "file": path, "line": line_num, "kind": kind, "old": "", "candidate_new": "", "required": required, "snippet": line.strip()[:600]})
    columns = ["category", "provenance", "file", "line", "kind", "old", "candidate_new", "required", "snippet"]
    with (out / "consumer-references.tsv").open("w", newline="", encoding="utf-8") as handle:
        writer = csv.DictWriter(handle, fieldnames=columns, delimiter="\t")
        writer.writeheader()
        writer.writerows(rows)
    (out / "consumer-references.json").write_text(json.dumps(rows, ensure_ascii=False, indent=2) + "\n")
    (out / "scanned-files.json").write_text(json.dumps(corpus, ensure_ascii=False, indent=2) + "\n")
    summary = {
        "root": str(root),
        "method": "lexical references in present safe tracked/visible-untracked files plus explicit ignored project docs; not runtime reachability or compilation",
        "excluded": ["secrets/certificates/env contents", "binary files", "node_modules/target/artifacts", "docs evidence captures/security CSV", "go.sum/package-lock", "SQL asset bodies (asset membership separately covered by Go embed graph)", "non-whitelisted file types", "symlinks", "files > 2 MB"],
        "outer_candidates": OUTER,
        "inner_candidates": INNER,
        "semantic_split_search_placeholders": SEMANTIC_SPLITS,
        "interpretation": "outer-edit-files.tsv and inner-edit-files.tsv are lexical REVIEW SETS, not instructions that every file must be edited; reviewed-path-contracts.tsv provides individually reviewed actions. Historical/ignored references are preserved evidence, not active consumers. Inner candidate destinations remain proposals; semantic split placeholders are not valid destination paths.",
        "scanned_files": len(corpus),
        "reference_rows": len(rows),
        "unique_reference_files": len({r['file'] for r in rows}),
        "categories_rows": dict(Counter(r['category'] for r in rows)),
        "categories_unique_files": {c: len({r['file'] for r in rows if r['category'] == c}) for c in sorted({r['category'] for r in rows})},
        "kinds_rows": dict(Counter(r['kind'] for r in rows)),
        "head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
    }
    (out / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
