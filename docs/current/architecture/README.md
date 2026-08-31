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
- `hololive-api-trust-domain.md` - consolidated bot/admin/LLM process boundary, controls, and split trigger
- `non-secret-history-risk-decisions-20260713.md` - separate #087/#088 current-tree and Git-history risk decisions
- `alarm-egress-scale-out-decisions-20260730.md` - alarm-worker egress lease removal, production role profiles, and the replica>1 gate list
- `youtube-egress-lifecycle-transition-ownership-20260831.md` - YouTube outbox/delivery 전이 정책과 PostgreSQL 집행의 소유권, package·schema 경계
- `youtube-egress-lifecycle-contract-20260831.md` - delivery/outbox 상태, token, attempt, operation atomicity, CAS, quarantine/revive의 규범 계약
- `youtube-egress-lifecycle-commit-adjudication-20260831.md` - effect 인접 transaction의 commit ambiguity, primary read-back과 금지 retry 규약
- `youtube-egress-lifecycle-library-review-20260831.md` - 직접 planner와 Go FSM/persistence 후보의 hard gate, 비용, 재검토 조건

## Gate Assets

- `../../architecture/file-loc-thresholds.txt`
- `../../architecture/release-governance-assets.txt`
- `../../architecture/shared-go-package-allowlist.txt`

## Rule

- 현재 governance 자산은 이 인덱스에서 추적 가능해야 합니다.
- CI에서 쓰는 기준 파일은 current-layer에서 발견 가능해야 합니다.
- 문서/계약 gate는 `scripts/architecture/ci-boundary-gate.sh`를 통해 실행되어야 합니다.
