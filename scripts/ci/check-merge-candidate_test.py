#!/usr/bin/env python3
"""실제 workflow의 SHA guard를 merge/base/head fixture에 적용한다."""

import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = (ROOT / ".github/workflows/ci.yml").read_text()


class MergeCandidateTest(unittest.TestCase):
    def test_every_job_checks_the_event_sha(self):
        refs = re.findall(r"^\s+ref: (.+)$", WORKFLOW, re.MULTILINE)
        expected = re.findall(r"^\s+EXPECTED_SHA: (.+)$", WORKFLOW, re.MULTILINE)
        self.assertEqual(len(refs), 4)
        self.assertEqual(refs, ["${{ github.sha }}"] * 4)
        self.assertEqual(expected, refs)
        self.assertNotIn("github.event.pull_request.head.sha", WORKFLOW)

    def test_head_only_and_base_only_regression_and_conflict(self):
        guards = re.findall(
            r'          actual_sha="\$\(git rev-parse HEAD\)"\n'
            r'.*?          test "\$\{actual_sha\}" = "\$\{EXPECTED_SHA\}"',
            WORKFLOW, re.DOTALL,
        )
        self.assertEqual(len(guards), 4)
        with tempfile.TemporaryDirectory(prefix="merge-candidate-") as raw:
            root = Path(raw)
            # 실제 저장소의 인증·원격·hooks를 사용하지 않는 폐기 가능한 Git object fixture다.
            env = dict(os.environ, GIT_AUTHOR_NAME="fixture", GIT_AUTHOR_EMAIL="fixture@example.invalid",
                       GIT_COMMITTER_NAME="fixture", GIT_COMMITTER_EMAIL="fixture@example.invalid")

            def git(*args, content=None, check=True):
                return subprocess.run(["git", *args], cwd=root, env=env, input=content,
                                      text=True, capture_output=True, check=check)

            def commit(files, parents=()):
                entries = []
                for name, value in sorted(files.items()):
                    blob = git("hash-object", "-w", "--stdin", content=value).stdout.strip()
                    entries.append(f"100644 blob {blob}\t{name}\n")
                tree = git("mktree", content="".join(entries)).stdout.strip()
                args = ["commit-tree", tree]
                for parent in parents:
                    args += ["-p", parent]
                return git(*args, content="synthetic fixture\n").stdout.strip()

            git("init", "-q")
            common = commit({"policy": "allow\n", "implementation": "old\n"})
            base = commit({"policy": "deny\n", "implementation": "old\n"}, (common,))
            head = commit({"policy": "allow\n", "implementation": "new\n"}, (common,))
            tree = git("merge-tree", "--write-tree", base, head).stdout.strip()
            candidate = git("commit-tree", tree, "-p", base, "-p", head,
                            content="synthetic merge\n").stdout.strip()

            def guard(script, expected_sha):
                return subprocess.run(["bash", "-e", "-c", script], cwd=root,
                                      env=dict(env, EXPECTED_SHA=expected_sha, GITHUB_EVENT_NAME="pull_request"),
                                      capture_output=True, text=True)

            git("checkout", "--detach", head)
            for script in guards:
                self.assertNotEqual(guard(script, candidate).returncode, 0, "head-only checkout accepted")
            self.assertEqual((root / "policy").read_text(), "allow\n")
            git("checkout", "--detach", candidate)
            for script in guards:
                self.assertEqual(guard(script, candidate).returncode, 0)
                # PR head에서는 통과하지만 base 변경과 결합하면 실패하는 정책 검사를 실행한다.
                self.assertNotEqual(guard(script + '\ntest "$(cat policy)" = allow', candidate).returncode, 0)

            conflicting = commit({"policy": "another-policy\n", "implementation": "new\n"}, (common,))
            self.assertNotEqual(git("merge-tree", "--write-tree", base, conflicting, check=False).returncode, 0)
            # 충돌 시 존재하지 않는 merge SHA 대신 head를 성공으로 보고할 수 없다.
            git("checkout", "--detach", conflicting)
            for script in guards:
                self.assertNotEqual(guard(script, candidate).returncode, 0)


if __name__ == "__main__":
    unittest.main()
