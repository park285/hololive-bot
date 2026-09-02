#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path


ANALYZER = Path(__file__).with_name("check-structure-budget.py")


class StructureBudgetTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "scripts" / "architecture").mkdir(parents=True)
        (self.root / "docs" / "architecture").mkdir(parents=True)
        self.policy = self.root / "scripts" / "architecture" / "structure-budget-policy.json"
        self.thresholds = self.root / "docs" / "architecture" / "file-loc-thresholds.txt"
        self.write_policy()
        self.thresholds.write_text("# fixture thresholds\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temp.cleanup()

    def write_policy(self, *, forbid_partition_files: bool = False) -> None:
        self.policy.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "file": {
                        "default": {"advisory": 400, "hard": 800},
                        "threshold_file": "docs/architecture/file-loc-thresholds.txt",
                        "extensions": [".go", ".rs", ".sh", ".ts", ".tsx"],
                    },
                    "functions": {
                        "lines": {"advisory": 60, "hard": 120},
                        "complexity": {"advisory": 8, "hard": 16},
                        "nesting": {"advisory": 5, "hard": 8},
                    },
                    "forbid_partition_files": forbid_partition_files,
                }
            ),
            encoding="utf-8",
        )

    def run_analyzer(
        self,
        mode: str,
        *,
        changed: list[str] | None = None,
        component: str = "all",
    ) -> subprocess.CompletedProcess[str]:
        command = [
            "python3",
            str(ANALYZER),
            "--root",
            str(self.root),
            "--policy",
            str(self.policy),
            "--mode",
            mode,
            "--format",
            "json",
            "--component",
            component,
        ]
        if changed is not None:
            path = self.root / "changed"
            path.write_bytes(b"\0".join(item.encode() for item in changed) + b"\0")
            command += ["--changed-paths-file", str(path)]
        return subprocess.run(
            command,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )

    def write_lines(self, path: str, count: int) -> None:
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text("package p\n" + "// padding\n" * (count - 1), encoding="utf-8")

    def test_soft_and_hard_file_ceilings(self) -> None:
        self.write_lines("soft.go", 401)
        advisory = self.run_analyzer("advisory", component="files")
        self.assertEqual(advisory.returncode, 0)
        self.assertEqual(json.loads(advisory.stdout)["findings"][0]["level"], "advisory")

        self.write_lines("hard.go", 801)
        hard = self.run_analyzer("hard", component="files")
        self.assertEqual(hard.returncode, 1)
        self.assertTrue(
            any(item["level"] == "hard_ceiling" for item in json.loads(hard.stdout)["findings"])
        )

    def test_configured_path_is_advisory_and_stale_path_is_error(self) -> None:
        self.write_lines("configured.go", 6)
        self.thresholds.write_text("configured.go:5\n", encoding="utf-8")
        report = self.run_analyzer("hard", component="files")
        self.assertEqual(report.returncode, 0)
        finding = json.loads(report.stdout)["findings"][0]
        self.assertEqual((finding["advisory_limit"], finding["hard_limit"]), (5, 800))

        self.thresholds.write_text("missing.go:5\n", encoding="utf-8")
        self.assertEqual(self.run_analyzer("hard", component="files").returncode, 2)

    def test_duplicate_and_escape_thresholds_are_policy_errors(self) -> None:
        self.write_lines("configured.go", 2)
        invalid_values = (
            "configured.go:5\nconfigured.go:6\n",
            "../escape.go:5\n",
        )
        for value in invalid_values:
            with self.subTest(value=value):
                self.thresholds.write_text(value, encoding="utf-8")
                self.assertEqual(self.run_analyzer("hard", component="files").returncode, 2)

    def test_changed_filter_and_json_ordering(self) -> None:
        self.write_lines("z.go", 401)
        self.write_lines("a.go", 401)
        report = self.run_analyzer("advisory", changed=["z.go"], component="files")
        payload = json.loads(report.stdout)
        self.assertEqual([item["path"] for item in payload["findings"]], ["z.go"])

        full = json.loads(self.run_analyzer("advisory", component="files").stdout)
        ids = [item["id"] for item in full["findings"]]
        self.assertEqual(ids, sorted(ids))

    def test_function_stable_id_omits_line_number_and_hard_ceiling_blocks(self) -> None:
        target = self.root / "long.go"
        target.write_text(
            "package p\nfunc Huge() {\n" + "    value := 1\n" * 119 + "}\n",
            encoding="utf-8",
        )
        report = self.run_analyzer("hard", component="functions")
        self.assertEqual(report.returncode, 1)
        findings = json.loads(report.stdout)["findings"]
        line_finding = next(item for item in findings if item["rule"] == "function_lines")
        self.assertEqual(line_finding["id"], "function_lines:long.go:Huge")


    def test_partition_file_suffix_is_hard_invariant(self) -> None:
        self.write_lines("internal/a_part2.go", 3)
        self.assertEqual(self.run_analyzer("hard", component="files").returncode, 0)
        self.write_policy(forbid_partition_files=True)
        report = self.run_analyzer("hard", component="files")
        self.assertEqual(report.returncode, 1)
        finding = json.loads(report.stdout)["findings"][0]
        self.assertEqual(finding["id"], "partition_file:internal/a_part2.go")
        self.assertEqual(finding["level"], "hard_invariant")

if __name__ == "__main__":
    unittest.main()
