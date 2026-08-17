#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import tempfile
from pathlib import Path

CHECKER = Path(__file__).with_name("check-go-test-json.py")


def write_jsonl(path: Path, lines: list[str]) -> None:
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def run(path: Path, extra: list[str] | None = None) -> subprocess.CompletedProcess[str]:
    argv = ["python3", str(CHECKER), "--input", str(path), "--require-pass"]
    if extra:
        argv.extend(extra)
    return subprocess.run(argv, text=True, capture_output=True, check=False)


def expect_success(label: str, result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode != 0:
        raise AssertionError(f"{label}: expected success\nstdout={result.stdout}\nstderr={result.stderr}")


def expect_failure(label: str, result: subprocess.CompletedProcess[str]) -> None:
    if result.returncode == 0:
        raise AssertionError(f"{label}: expected failure\nstdout={result.stdout}\nstderr={result.stderr}")


def main() -> int:
    with tempfile.TemporaryDirectory() as raw:
        base = Path(raw)

        passing = base / "pass.jsonl"
        write_jsonl(
            passing,
            [
                '{"Action":"start","Package":"example.com/mod"}',
                '{"Action":"run","Package":"example.com/mod","Test":"TestOK"}',
                '{"Action":"pass","Package":"example.com/mod","Test":"TestOK"}',
                '{"Action":"pass","Package":"example.com/mod"}',
            ],
        )
        expect_success("passing package", run(passing))

        failing = base / "fail.jsonl"
        write_jsonl(
            failing,
            [
                '{"Action":"start","Package":"example.com/mod"}',
                '{"Action":"run","Package":"example.com/mod","Test":"TestFail"}',
                '{"Action":"fail","Package":"example.com/mod","Test":"TestFail"}',
                '{"Action":"fail","Package":"example.com/mod"}',
            ],
        )
        expect_failure("failed test", run(failing))

        skipped = base / "skip.jsonl"
        write_jsonl(
            skipped,
            [
                '{"Action":"start","Package":"example.com/mod"}',
                '{"Action":"run","Package":"example.com/mod","Test":"TestSkip"}',
                '{"Action":"skip","Package":"example.com/mod","Test":"TestSkip"}',
                '{"Action":"pass","Package":"example.com/mod"}',
            ],
        )
        expect_failure("unexpected skip", run(skipped))

        allow = base / "allow.txt"
        allow.write_text("example.com/mod/TestSkip\n", encoding="utf-8")
        expect_success("allow-listed skip", run(skipped, ["--allow-skip-file", str(allow)]))

        no_test_files = base / "no-test-files.jsonl"
        write_jsonl(
            no_test_files,
            [
                '{"Action":"start","Package":"example.com/mod/internal/testutil"}',
                '{"Action":"output","Package":"example.com/mod/internal/testutil","Output":"?   \\texample.com/mod/internal/testutil\\t[no test files]\\n"}',
                '{"Action":"skip","Package":"example.com/mod/internal/testutil"}',
            ],
        )
        expect_success("package with no test files", run(no_test_files))

        package_skip = base / "package-skip.jsonl"
        write_jsonl(
            package_skip,
            [
                '{"Action":"start","Package":"example.com/mod"}',
                '{"Action":"skip","Package":"example.com/mod"}',
            ],
        )
        expect_failure("package skip without no-test-files evidence", run(package_skip))

        panic = base / "panic.jsonl"
        write_jsonl(
            panic,
            [
                '{"Action":"start","Package":"example.com/mod"}',
                '{"Action":"run","Package":"example.com/mod","Test":"TestPanic"}',
                '{"Action":"output","Package":"example.com/mod","Test":"TestPanic","Output":"panic: boom\\n"}',
                '{"Action":"fail","Package":"example.com/mod","Test":"TestPanic"}',
                '{"Action":"fail","Package":"example.com/mod"}',
            ],
        )
        expect_failure("panic output", run(panic))

        garbage = base / "garbage.jsonl"
        write_jsonl(garbage, ['{"Action":"pass","Package":"example.com/mod"} trailing'])
        expect_failure("trailing garbage", run(garbage))

        incomplete = base / "incomplete.jsonl"
        incomplete.write_text('{"Action":"pass","Package":"example.com/mod"\n', encoding="utf-8")
        expect_failure("truncated JSON", run(incomplete))

    print("ok: check-go-test-json fixtures passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
