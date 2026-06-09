#!/usr/bin/env python3
"""Cross-repository guardrails for manually-applied AOP-style boundaries.

This checker is intentionally conservative. It does not try to prove complete
program correctness; it catches the high-signal places where a new entrypoint,
background worker, transport, or FFI boundary can bypass the shared wrapper that
should have been applied.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

SKIP_DIRS = {
    ".git",
    ".gradle",
    ".idea",
    ".tmp",
    "artifacts",
    "backups",
    "bin",
    "build",
    "code-sketches",
    "data",
    "dist",
    "logs",
    "node_modules",
    "out",
    "target",
    "vendor",
}

GO_RECOVERY_TOKENS = (
    "ApplyBaseMiddleware(",
    "RecoveryMiddleware(",
    "gin.Recovery(",
    "NewRuntimeRouter(",
    "RecoverHTTP(",
    "HTTPBoundary(",
)

GOROUTINE_RECOVERY_TOKENS = (
    "RecoverLogged(",
    "recover()",
    "runtimekit.Go(",
    "safeGo(",
    "RunProtected(",
)

RUST_JNI_BOUNDARY_TOKENS = (
    "catch_unwind",
    "recover(",
    "jni_entrypoint",
    "jni_safe",
    "run_jni",
    "with_jni_boundary",
)

REPO_PROFILES = {
    "Iris",
    "chat-bot-go-kakao",
    "hololive-bot",
    "iris-client-go",
    "generic",
}


@dataclass(frozen=True)
class Finding:
    severity: str
    path: str
    line: int
    message: str

    def render(self) -> str:
        location = self.path
        if self.line > 0:
            location = f"{location}:{self.line}"
        return f"[{self.severity}] {location}: {self.message}"


@dataclass(frozen=True)
class SourceView:
    text: str
    code: str
    lines: list[str]
    code_lines: list[str]
    allow_lines: set[int]


def rel(path: Path, root: Path) -> str:
    try:
        return path.relative_to(root).as_posix()
    except ValueError:
        return path.as_posix()


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


ALLOW_RE = re.compile(r"//\s*crosscutting:allow\b")


def build_code_view(text: str) -> tuple[str, set[int]]:
    out: list[str] = []
    allow_lines: set[int] = set()
    i = 0
    line = 1
    n = len(text)

    while i < n:
        ch = text[i]
        nxt = text[i + 1] if i + 1 < n else ""

        if ch == "/" and nxt == "/":
            comment_line = line
            comment = ["//"]
            out.extend([" ", " "])
            i += 2
            while i < n and text[i] != "\n":
                comment.append(text[i])
                out.append(" ")
                i += 1
            if ALLOW_RE.search("".join(comment)):
                allow_lines.add(comment_line)
            continue

        if ch == "/" and nxt == "*":
            out.extend([" ", " "])
            i += 2
            while i < n:
                ch = text[i]
                nxt = text[i + 1] if i + 1 < n else ""
                if ch == "*" and nxt == "/":
                    out.extend([" ", " "])
                    i += 2
                    break
                out.append("\n" if ch == "\n" else " ")
                if ch == "\n":
                    line += 1
                i += 1
            continue

        if ch == '"':
            out.append(" ")
            i += 1
            while i < n:
                ch = text[i]
                out.append("\n" if ch == "\n" else " ")
                if ch == "\n":
                    line += 1
                i += 1
                if ch == "\\" and i < n:
                    ch = text[i]
                    out.append("\n" if ch == "\n" else " ")
                    if ch == "\n":
                        line += 1
                    i += 1
                    continue
                if ch == '"':
                    break
            continue

        if ch == "`":
            out.append(" ")
            i += 1
            while i < n:
                ch = text[i]
                out.append("\n" if ch == "\n" else " ")
                if ch == "\n":
                    line += 1
                i += 1
                if ch == "`":
                    break
            continue

        out.append(ch)
        if ch == "\n":
            line += 1
        i += 1

    return "".join(out), allow_lines


def read_source(path: Path) -> SourceView:
    text = read_text(path)
    code, allow_lines = build_code_view(text)
    return SourceView(text, code, text.splitlines(), code.splitlines(), allow_lines)


def iter_files(root: Path, suffixes: Iterable[str]) -> Iterable[Path]:
    suffixes = tuple(suffixes)
    for path in root.rglob("*"):
        if not path.is_file() or path.suffix not in suffixes:
            continue
        parts = set(path.relative_to(root).parts)
        if parts & SKIP_DIRS:
            continue
        yield path


def line_no(text: str, index: int) -> int:
    return text.count("\n", 0, max(index, 0)) + 1


def detect_profile(root: Path) -> str:
    go_mod = root / "go.mod"
    go_mod_text = read_text(go_mod) if go_mod.exists() else ""

    if (root / "hololive/hololive-shared/pkg/server/internal/httpserver/router_base.go").exists():
        return "hololive-bot"
    if (root / "native/iris-runtime/src/ffi/exports.rs").exists():
        return "Iris"
    if "github.com/kapu/chat-bot-go-kakao" in go_mod_text or (root / "internal/app/app_routes.go").exists():
        return "chat-bot-go-kakao"
    if "github.com/park285/iris-client-go" in go_mod_text or (root / "webhook/handler.go").exists():
        return "iris-client-go"
    return "generic"


def extract_brace_block(lines: list[str], start: int) -> str:
    block: list[str] = []
    depth = 0
    started = False
    for line in lines[start:]:
        block.append(line)
        depth += line.count("{")
        if line.count("{"):
            started = True
        depth -= line.count("}")
        if started and depth <= 0:
            break
    return "\n".join(block)


def allowed(allow_lines: set[int], line: int) -> bool:
    return line in allow_lines or line - 1 in allow_lines


def add(
    findings: list[Finding],
    severity: str,
    root: Path,
    path: Path,
    line: int,
    message: str,
    allow_lines: set[int] | None = None,
) -> None:
    if allow_lines and allowed(allow_lines, line):
        return
    findings.append(Finding(severity, rel(path, root), line, message))


def check_gin_router_recovery(root: Path, findings: list[Finding]) -> None:
    for path in iter_files(root, [".go"]):
        if path.name.endswith("_test.go"):
            continue
        source = read_source(path)
        if "gin.New()" not in source.code:
            continue
        if any(token in source.code for token in GO_RECOVERY_TOKENS):
            continue
        add(
            findings,
            "error",
            root,
            path,
            line_no(source.code, source.code.find("gin.New()")),
            "gin.New() is used without a same-file recovery/base middleware boundary",
            source.allow_lines,
        )


def check_hololive_base_middleware(root: Path, findings: list[Finding]) -> None:
    base = root / "hololive/hololive-shared/pkg/server/internal/httpserver/router_base.go"
    if base.exists():
        source = read_source(base)
        if "func ApplyBaseMiddleware" in source.code and not (
            "RecoveryMiddleware(" in source.code or "gin.Recovery(" in source.code
        ):
            add(
                findings,
                "warning",
                root,
                base,
                line_no(source.code, source.code.find("func ApplyBaseMiddleware")),
                "ApplyBaseMiddleware does not install recovery; recovery can be forgotten by new router builders",
                source.allow_lines,
            )

    runtime = root / "hololive/hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go"
    if runtime.exists():
        source = read_source(runtime)
        idx = source.code.find("router.Use(gin.Recovery())")
        if idx >= 0:
            add(
                findings,
                "warning",
                root,
                runtime,
                line_no(source.code, idx),
                "recovery is installed in runtime helper instead of the common base middleware pipeline",
                source.allow_lines,
            )


def check_hololive_holo_api_auth(root: Path, findings: list[Finding]) -> None:
    path = root / "hololive/hololive-admin-api/internal/app/http/registration.go"
    if not path.exists():
        return
    lines = read_text(path).splitlines()
    for i, line in enumerate(lines):
        if 'router.Group("/api/holo")' not in line:
            continue
        window = "\n".join(lines[i : i + 24])
        if "APIKeyAuthMiddleware" not in window:
            add(
                findings,
                "error",
                root,
                path,
                i + 1,
                "/api/holo route group must install APIKeyAuthMiddleware near group creation",
            )


def check_net_http_boundary(root: Path, findings: list[Finding], profile: str) -> None:
    for path in iter_files(root, [".go"]):
        if path.name.endswith("_test.go"):
            continue
        source = read_source(path)
        if "http.NewServeMux()" not in source.code:
            continue
        if "HTTPBoundary(" in source.code or "RecoverHTTP(" in source.code:
            continue
        severity = "warning"
        message = "http.NewServeMux root is not visibly wrapped by a recovery/log/security HTTP boundary"
        if profile == "chat-bot-go-kakao" and "otelhttp.NewHandler(mux" in source.code:
            message += "; OpenTelemetry wraps tracing only and should not be treated as a panic/security wrapper"
        add(
            findings,
            severity,
            root,
            path,
            line_no(source.code, source.code.find("http.NewServeMux()")),
            message,
            source.allow_lines,
        )


def function_block_for_target(source: SourceView, target: str) -> str | None:
    func_re = re.compile(
        r"\bfunc\s+(?:\([^)]*\)\s*)?"
        + re.escape(target)
        + r"(?:\s*\[[^\]]+\])?\s*\("
    )
    match = func_re.search(source.code)
    if not match:
        return None
    start = line_no(source.code, match.start()) - 1
    return extract_brace_block(source.code_lines, start)


def check_go_goroutine_boundaries(root: Path, findings: list[Finding]) -> None:
    go_func_re = re.compile(r"\bgo\s+func\s*\(")
    group_go_re = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*\.Go\s*\(\s*func\s*\(")
    direct_go_re = re.compile(r"\bgo\s+(?!func\b)(?P<target>[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\(")
    for path in iter_files(root, [".go"]):
        if path.name.endswith("_test.go") or rel(path, root).startswith("scripts/"):
            continue
        source = read_source(path)

        inline_starts: list[int] = []
        for regex in (go_func_re, group_go_re):
            for match in regex.finditer(source.code):
                inline_starts.append(line_no(source.code, match.start()) - 1)
        for start in sorted(set(inline_starts)):
            block = extract_brace_block(source.code_lines, start)
            if any(token in block for token in GOROUTINE_RECOVERY_TOKENS):
                continue
            add(
                findings,
                "warning",
                root,
                path,
                start + 1,
                "background goroutine has no visible panic recovery wrapper; use RecoverLogged/runtimekit.Go or add an allowlist comment",
                source.allow_lines,
            )

        for match in direct_go_re.finditer(source.code):
            start = line_no(source.code, match.start()) - 1
            target = match.group("target").split(".")[-1]
            line = source.code_lines[start] if start < len(source.code_lines) else ""
            block = function_block_for_target(source, target) or line
            if any(token in block for token in GOROUTINE_RECOVERY_TOKENS):
                continue
            add(
                findings,
                "warning",
                root,
                path,
                start + 1,
                "background goroutine has no visible panic recovery wrapper; use RecoverLogged/runtimekit.Go or add an allowlist comment",
                source.allow_lines,
            )


def check_iris_jni_boundaries(root: Path, findings: list[Finding]) -> None:
    exports = root / "native/iris-runtime/src/ffi/exports.rs"
    if not exports.exists():
        return
    source = read_source(exports)
    lines = source.code_lines
    extern_re = re.compile(r"\bextern\s+fn\s+([A-Za-z0-9_]+)")
    delegated = False
    for i, line in enumerate(lines):
        match = extern_re.search(line)
        if not match:
            continue
        name = match.group(1)
        block = extract_brace_block(lines, i)
        if any(token in block for token in RUST_JNI_BOUNDARY_TOKENS):
            continue
        if "super::entrypoints::" in block:
            delegated = True
            continue
        add(
            findings,
            "error",
            root,
            exports,
            i + 1,
            f"JNI export {name} must either contain catch_unwind/recover or delegate only to ffi::entrypoints",
            source.allow_lines,
        )

    if delegated:
        entrypoints = root / "native/iris-runtime/src/ffi/entrypoints.rs"
        support = root / "native/iris-runtime/src/ffi/support.rs"
        entrypoints_code = read_source(entrypoints).code if entrypoints.exists() else ""
        support_code = read_source(support).code if support.exists() else ""
        if not entrypoints.exists() or "recover(" not in entrypoints_code:
            add(
                findings,
                "error",
                root,
                entrypoints,
                1,
                "JNI exports delegate to ffi::entrypoints, but entrypoints do not visibly use recover()",
            )
        if not support.exists() or "catch_unwind" not in support_code:
            add(
                findings,
                "error",
                root,
                support,
                1,
                "JNI recover helper must be backed by std::panic::catch_unwind",
            )


TLS_INSECURE_ASSIGNMENT_RE = re.compile(
    r"\b(?P<ident>(?:[A-Za-z_][A-Za-z0-9_]*\.)*InsecureSkipVerify(?:ForTests?)?)\s*[:=]\s*true\b"
)
TLS_FOR_TESTS_ALIAS_RE = re.compile(
    r"^\s*[A-Za-z_][A-Za-z0-9_]*\s*=\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)+[A-Za-z_][A-Za-z0-9_]*ForTests?\s*$"
)
TLS_TEST_GATE_RE = re.compile(
    r"\bif\b[^{]*(?:\b(?:[A-Za-z_][A-Za-z0-9_]*\.)?allowInsecureForTests\b|\b[A-Za-z_][A-Za-z0-9_]*\.AllowInsecure[A-Za-z0-9_]*\b)[^{]*\{"
)
TLS_NEGATIVE_TEST_GUARD_RE = re.compile(
    r"\bif\b[^{]*!\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?(?:allowInsecureForTests|AllowInsecure[A-Za-z0-9_]*)\b[^{]*\{"
)


def enclosing_block_start(code_lines: list[str], line_index: int, column: int) -> int:
    depth = 0
    for i in range(line_index, -1, -1):
        segment = code_lines[i]
        if i == line_index:
            segment = segment[:column]
        for ch in reversed(segment):
            if ch == "}":
                depth += 1
            elif ch == "{":
                if depth == 0:
                    return i
                depth -= 1
    return 0


def tls_assignment_in_test_gate(code_lines: list[str], line_index: int, column: int) -> bool:
    depth = 0
    for i in range(line_index, -1, -1):
        segment = code_lines[i]
        if i == line_index:
            segment = segment[:column]
        for j in range(len(segment) - 1, -1, -1):
            ch = segment[j]
            if ch == "}":
                depth += 1
            elif ch == "{":
                if depth == 0:
                    if TLS_TEST_GATE_RE.search(segment[: j + 1]):
                        return True
                else:
                    depth -= 1
    return False


def tls_assignment_has_prior_test_guard(code_lines: list[str], line_index: int, column: int) -> bool:
    block_start = enclosing_block_start(code_lines, line_index, column)
    for i in range(block_start + 1, line_index):
        line = code_lines[i]
        if not TLS_NEGATIVE_TEST_GUARD_RE.search(line):
            continue
        guard_block = extract_brace_block(code_lines, i)
        guard_end = i + len(guard_block.splitlines()) - 1
        if guard_end < line_index and "return" in guard_block:
            return True
    return False


def check_tls_insecure_skip(root: Path, findings: list[Finding]) -> None:
    for path in iter_files(root, [".go"]):
        source = read_source(path)
        if path.name.endswith("_test.go"):
            continue
        for i, line in enumerate(source.code_lines):
            if TLS_FOR_TESTS_ALIAS_RE.match(line):
                continue
            for match in TLS_INSECURE_ASSIGNMENT_RE.finditer(line):
                ident = match.group("ident")
                if ident.endswith(("InsecureSkipVerifyForTests", "InsecureSkipVerifyForTest")):
                    continue
                if tls_assignment_in_test_gate(
                    source.code_lines, i, match.start()
                ) or tls_assignment_has_prior_test_guard(source.code_lines, i, match.start()):
                    continue
                add(
                    findings,
                    "error",
                    root,
                    path,
                    i + 1,
                    "TLS InsecureSkipVerify must be gated by an explicit test/local-only option",
                    source.allow_lines,
                )


def check_iris_client_webhook(root: Path, findings: list[Finding]) -> None:
    handler = root / "webhook/handler.go"
    if not handler.exists():
        return
    text = read_text(handler)
    if "func (h *Handler) ServeHTTP" in text:
        if "http.MaxBytesReader" not in text:
            add(findings, "error", root, handler, line_no(text, text.find("ServeHTTP")), "webhook ServeHTTP must enforce MaxBytesReader")
        if "ConstantTimeCompare" not in text:
            add(findings, "error", root, handler, line_no(text, text.find("rejectUnauthorized")), "webhook token comparison must stay constant-time")
    if "func (h *Handler) runTask" in text and "recover(); recovered != nil" not in text:
        add(findings, "error", root, handler, line_no(text, text.find("func (h *Handler) runTask")), "webhook worker task runner must recover handler panics")


def check_local_retry_shapes(root: Path, findings: list[Finding]) -> None:
    allow_exact = {
        "hololive/hololive-shared/internal/retry/retry.go",
    }
    for path in iter_files(root, [".go", ".kt", ".rs"]):
        r = rel(path, root)
        if path.name.endswith(("_test.go", "Test.kt")) or r in allow_exact:
            continue
        source = read_source(path)
        lower_name = path.name.lower()
        sleep_tokens = (
            "time.Sleep(",
            "time.NewTimer(",
            "time.After(",
            "delay(",
            "tokio::time::sleep",
        )
        shape = (
            any(token in source.code for token in sleep_tokens)
        ) and ("retry" in lower_name or "attempt" in source.code or "backoff" in source.code)
        if not shape:
            continue
        if "WithRetry(" in source.code or "retry.WithRetry(" in source.code or "retryPing(" in source.code:
            continue
        indices = [source.code.find(token) for token in sleep_tokens if source.code.find(token) >= 0]
        finding_line = line_no(source.code, min(indices)) if indices else 1
        add(
            findings,
            "warning",
            root,
            path,
            finding_line,
            "local retry/backoff shape detected; converge to a shared retry policy or add an allowlist rationale",
            source.allow_lines,
        )


def check_ci_wiring(root: Path, findings: list[Finding], profile: str) -> None:
    candidates: list[Path]
    if profile == "hololive-bot":
        candidates = [root / "scripts/architecture/ci-boundary-gate.sh"]
    elif profile == "Iris":
        candidates = [root / "scripts/check-architecture-guardrails.sh", root / "scripts/ci/ci-fast-gate.sh"]
    elif profile == "chat-bot-go-kakao":
        candidates = [root / "scripts/build-go-binaries.sh", root / "scripts/ci/pre-push-gate.sh"]
    elif profile == "iris-client-go":
        candidates = [root / ".github/workflows/ci.yml"]
    else:
        candidates = []

    marker_re = re.compile(r"cross[-_ ]?cutting|crosscutting", re.I)
    for path in candidates:
        if not path.exists():
            continue
        text = read_text(path)
        if not marker_re.search(text):
            add(
                findings,
                "warning",
                root,
                path,
                1,
                "CI/local gate does not yet wire the cross-cutting boundary checker",
            )


def run_checks(root: Path, profile: str) -> list[Finding]:
    findings: list[Finding] = []
    check_gin_router_recovery(root, findings)
    check_net_http_boundary(root, findings, profile)
    check_go_goroutine_boundaries(root, findings)
    check_tls_insecure_skip(root, findings)
    check_local_retry_shapes(root, findings)
    check_ci_wiring(root, findings, profile)

    if profile == "hololive-bot":
        check_hololive_base_middleware(root, findings)
        check_hololive_holo_api_auth(root, findings)
    elif profile == "Iris":
        check_iris_jni_boundaries(root, findings)
    elif profile == "iris-client-go":
        check_iris_client_webhook(root, findings)

    return findings


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".", help="repository root")
    parser.add_argument("--profile", choices=["auto", *sorted(REPO_PROFILES)], default="auto")
    parser.add_argument("--strict", action="store_true", help="treat warnings as failures")
    parser.add_argument("--format", choices=("text", "github"), default="text")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    profile = detect_profile(root) if args.profile == "auto" else args.profile
    findings = run_checks(root, profile)

    if findings:
        print(f"[crosscutting] profile={profile}", file=sys.stderr)
        for finding in findings:
            if args.format == "github":
                level = "error" if finding.severity == "error" else "warning"
                print(f"::{level} file={finding.path},line={finding.line}::{finding.message}", file=sys.stderr)
            else:
                print(f"  - {finding.render()}", file=sys.stderr)

    strict = args.strict or os.environ.get("STRICT_CROSSCUTTING", "false").lower() == "true"
    has_error = any(f.severity == "error" for f in findings)
    has_warning = any(f.severity == "warning" for f in findings)
    if has_error or (strict and has_warning):
        return 1

    if not findings:
        print(f"[crosscutting] profile={profile} passed")
    else:
        print(f"[crosscutting] profile={profile} passed with warnings")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
