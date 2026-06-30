# Current Architecture Governance

현재 architecture/governance 기준의 루트 인덱스입니다.

## Gate Documents

- `ci-gates.md` - architecture/doc gate 목적, 실패 조건, 실행 순서
- `llm-work-rules.md` - LLM 문서/계약 작업 규칙
- `repo-tree-policy.md` - repository tree classification and cleanup policy
- `repo-refactor-audit.md` - active refactor findings, completed cleanup, and deferred risk register
- `app-bootstrap-boundary-guide.md` - Kakao bot bootstrap boundary guide
- `review-bundles.md` - review source/full bundle export policy
- `h3-runtime-smoke-cross-debate-20260630.md` - H3 runtime smoke cross-debate result and remaining closure checklist

## Gate Assets

- `../../architecture/go-module-loc-thresholds.txt`
- `../../architecture/file-loc-thresholds.txt`
- `../../architecture/release-governance-assets.txt`
- `../../architecture/shared-go-package-allowlist.txt`

## Rule

- 현재 governance 자산은 이 인덱스에서 추적 가능해야 합니다.
- CI에서 쓰는 기준 파일은 current-layer에서 발견 가능해야 합니다.
- 문서/계약 gate는 `scripts/architecture/ci-boundary-gate.sh`를 통해 실행되어야 합니다.
