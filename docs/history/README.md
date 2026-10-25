# History Docs

더 이상 현재 SSOT가 아닌 문서를 둡니다.

## 포함 기준
- 완료된 migration 기록
- 과거 release/rollback 메모
- 더 이상 유효하지 않은 설계와 TODO 기록

## 배너 규칙
- historical 문서는 파일 상단에 현재 SSOT가 아님을 명시합니다.
- 가능하면 현재 문서 경로를 함께 적습니다.

## 예시 배너
> Historical document. Do not use as the current source of truth. See `docs/current/`.

## Index

### Alarm dispatch
- `alarm-dispatch/ALARM_DISPATCH_REMEDIATION_20260414.md` - alarm/dispatcher remediation completion record

### Runtime split
- `runtime-split/RUNTIME_SPLIT_HANDOFF_20260416.md` - runtime split handoff record
- `runtime-split/RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md` - PR-07 blocker resolution record

### Repo review closeout
- `repo-review/CRITICAL_REVIEW_RESIDUAL_ISSUES_20260415.md` - critical review residual issue closeout
- `repo-review/REMAINING_RISKS_EXECUTION_GUIDE_20260415.md` - repo-wide remaining risk closeout
- `repo-review/LEGACY_LINT_RESUME_20260415.md` - legacy lint cleanup closeout

### YouTube
- `youtube/YOUTUBE_SCRAPER_ALARM_HARDENING_20260421.md` - YouTube scraper and alarm delivery hardening record
- `youtube/YOUTUBE_SCRAPER_DELAY_REVIEW.md` - YouTube scraper delay review and applied-change record

### Architecture and handoffs
- `architecture/` - superseded architecture snapshots and retired runtime plans
- [홀로봇 상세 리뷰와 리팩토링안](architecture/2026-10-15-hololive-code-review-and-refactoring.md) - 2026-10-15 재현 결함 4건, 구조 평가, 후속 개선안, 실제 검증 결과와 overlay 진단 코드
- [YouTube collector Go·JS 경계 구조 리뷰](architecture/2026-10-25-youtube-collector-go-js-boundary-review.md) - 2026-10-25 helper·PO issuer·wire 계약 경계의 읽기 전용 리뷰 결과와 권장 순서
- `architecture/DEDUP_REMEDIATION_PLAN.md` - superseded 2026-05-23 dedup remediation proposal
- `plans/2026-07-10-birthday-stream-alarm.md` - superseded birthday stream alarm v2 plan
- `youtube-convergence/handoffs/` - completed YouTube convergence implementation handoffs
- `runbooks/` - completed incident and operational review records
- `runbooks/IRIS_CLIENT_GO_V3_WEBHOOK_INBOX_DRAIN_20260928.md` - one-time webhook inbox drain before the iris-client-go v3 cutover
- `hardening-evidence/` - historical PR hardening command and result captures

### Plan kits
- `plan-kits/README.md` - legacy plan-kit bundle index
