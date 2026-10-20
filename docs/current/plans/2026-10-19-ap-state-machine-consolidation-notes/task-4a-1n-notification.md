<!-- 4a-1n 하위 작업 지시. 4a-1을 커밋한 뒤 <4a-1 커밋 SHA>를 채워 task 에이전트에 그대로 준다. 최종 응답은 JSON(status, summary, design_decision, files_changed, tests, verification[{command,result}], contract_changes, risks, blockers, commit_message). -->

<task>
배정 worktree: /home/kapu/work/iris-stack/hololive-bot-apsm-4a (브랜치 refactor/worker-handoff-reconciler, HEAD <4a-1 커밋 SHA> = 4a-1 alarm-dispatch handoff reconciler 커밋, 깨끗함). 모든 편집은 이 worktree의 절대 경로로 한다.
CHANGELOG 담당: 예(`## 미출시` 맨 위에 한 항목).

이번 단위는 **4a-1n: notification_delivery_outbox용 Iris handoff reconciler 어댑터**다(migration 330 열, 338 handoff due index).
배경 자료(먼저 읽는다):
- 하위 작업 지시 /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/task-4a-worker-reconciler.md (4a-1n 줄), 인계 문서 §2(결정)·§3.4 2번.
- 설계 S4 /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S4-iris202-reconciler.md §1(폴링 전수, notification 열 포함)·§2.1~2.5·§3·§5·§7. S4 §2.6의 notification 문단(흡수 전제)은 **무효**다.
- 설계 S3A /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S3A-sendfence-notification.md §3(notification 현재 구조: 상태·request JSON `{body, route, base_id, generation, exhausted}`·producer rearm(FAILED이고 exhausted가 아닌 행만 PENDING으로)·room FIFO `outbox_ready_predicate.sql`)·§5(4단계 확장 지점: 소유자 없는 전이 fence, OwnerMatches=TRUE, clock 규약).
- 사용자 결정 /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/decisions.md: notification은 alarm-dispatch로 흡수하지 않는다(결정 (i)); 4단계 reconciler는 notification 원장에도 어댑터(handoff 확인 열 포함)를 둔다; Iris failed → 재발급(generation<2이고 예산이 남으면 retry + generation+1).
- 이미 커밋된 4a-1 구현(이 브랜치 HEAD): `internal/egress/handoffcheck`(공유 순수 판정 `Decide`, `NextStep`, 코드 4종), `internal/service/alarm/dispatchoutbox/handoff.go`·`queries/handoff_*.sql`(claim SKIP LOCKED soft claim·적용 문장·sendfence 관측), `internal/egress/alarmdispatch/alarm_dispatch_handoff.go`(reconciler), migration 329/337, `build_egress.go` 등록. **이 구조와 규약을 그대로 재사용·대칭으로 만든다**(공유할 것은 handoffcheck에만 두고, 테이블별 SQL·저장소는 notification 쪽에 둔다. 범용 상태머신 엔진 금지).

부모가 정한 notification 전이(인계 문서 §3.4 2번):
- completed(requestId 일치) → 확인 기록(handoff_checked_at, next_check NULL).
- failed → request.generation < 2이고 시도 예산(notification_delivery max_retries 규칙, 현재 MarkFailed와 같은 기준)이 남으면 SENT→PENDING + request.generation+1(새 client_request_id `:rN`, 기존 notification reissue 규약 재사용), 아니면 `exhausted=true`로 FAILED(producer rearm이 되살리지 않게).
- outcome_unknown·모르는 상태·nil·requestId 불일치·404·지평 초과 → QUARANTINED(handoff 사유 코드는 4a-1과 같은 어휘).
- 조회 오류(404 외) → 지평 전이면 전이 없이 재예약, 지평 뒤면 QUARANTINED.
- 지평 = workers.notification_delivery.executor.attempt_timeout(새 설정 키 없음), ladder = handoffcheck.NextStep(4a-1과 동일).

해야 할 일:
1. 코드 조사 후 결정·보고: (a) 현재 notification 발송 경로가 SendPreparedMessage requestId를 어떻게 버리는지(4a-1이 youtube·notification에서 값을 버렸다), MarkSent에 requestId·첫 확인 시각을 같은 문장으로 기록하도록 확장, (b) **현재 room FIFO(`outbox_ready_predicate.sql` 등)가 handoff 완료까지 다음 메시지를 보류하는지**(지금은 Markdown 인라인 폴링이 발송 안에서 handoff를 기다리므로 사실상 보류된다는 가설을 코드로 확인), (c) 4a-3에서 인라인 폴링을 지운 뒤에도 같은 순서 의미를 유지하려면 FIFO 술어가 "같은 room의 SENT이면서 handoff 미확인 행"을 앞선 미완료로 보도록 바꿔야 하는지 판단하고, 필요하면 이번 단위에서 술어를 바꾼다(폴링이 남아 있는 동안에도 안전해야 한다). 결정과 근거를 design_decision에 쓴다.
2. migration 330(`notification_delivery_outbox`에 handoff 열·shape CHECK, 4a-1의 329와 같은 lock_timeout·NOT VALID→VALIDATE 형식), 338(handoff due 부분 index, CREATE INDEX CONCURRENTLY 단일 문장 파일). manifest.txt는 현재 마지막 ordinal 다음에 이어 붙인다. golden 재생성. dbtest migration 시험. integration-tag 시험이 schema를 고정 migration 목록으로 만들면 거기에도 추가한다.
3. 저장소: claim(SKIP LOCKED soft claim, `status='SENT' AND handoff_checked_at IS NULL`), 적용 문장(complete/reschedule/reissue/fail-exhausted/quarantine)을 3a sendfence 규약(잠금 뒤 clock 하나, 관측 열, sendfence.Check, MissError, OwnerMatches=TRUE)으로.
4. reconciler: 4a-1 reconciler의 최종 규약(외부 GetReplyStatus는 트랜잭션 밖, claim·정산은 일반 연결 획득, 상태 조회 병렬 + 정산 동시성 슬롯, 조회 5s + 정산 5s < soft claim 15s, 예산 초과 정산은 Warn, 적용 문장의 check_held로 확인 종료와 소유 상실 구분, requestId 원문을 로그에 남기지 않음; 상세는 /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/review-4a-1-pending.md)으로 notification용을 만들고 `build_egress.go`에 notification delivery가 활성일 때만 등록한다. 4a-1 reconciler와 코드 중복이 크면 handoffcheck에 순수 함수만 더 옮기는 정도로 줄인다(원장별 SQL은 공유하지 않는다).
5. 시각은 SQL clock_timestamp() 한 곳. clock CTE는 인덱스 범위 조건에서 `(SELECT at FROM clock)` 스칼라 하위 질의. `CLOCK_QUERY_POLICIES` 등록.
6. 시험: 판정 라우팅(완료/재발급/소진→exhausted FAILED/격리/재예약/지평), 재발급 뒤 다음 claim이 새 `:rN` ID로 같은 본문을 보냄, exhausted FAILED는 producer rearm(`outbox_enqueue_batch_upsert.sql`)이 되살리지 않음(실제 SQL로), 소유 상실 미스, FIFO 결정에 따른 순서 시험, migration shape 시험. 소비자 관점 동작만.
7. 문서: notification 관련 계약·runbook·services 문서(docs/current/contracts, docs/current/services/alarm-worker.md, runbooks), QUEUE_AND_PUBSUB_CONTRACTS.md, CHANGELOG.

검증(관찰 결과로 보고): gofmt -l, golangci-lint(바꾼 모듈) 0 issues, alarm-worker `go test -race -count=1 ./internal/egress/... ./internal/service/... ./internal/app/workerapp/...`(시간이 너무 길면 notification·alarm 관련 패키지로 좁히고 범위를 밝힌다), `./scripts/ci/local-ci.sh --integration-tests-only`(worktree 루트, 너가 실행), dbtest `-run 'NotificationHandoff|AlarmSendUnitHandoff|TestSchemaSnapshotGolden|Migration'`, `python3 scripts/architecture/check-sql-ownership.py`, `scripts/architecture/check-migration-manifest.sh`.
</task>
<constraints>
- 먼저 공통 규칙 /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/subagent-rules.md 를 끝까지 읽고 전부 따른다(절대 경로 편집, lint, Modern Go, 한국어 주석, 시험 품질, 경계).
- 품질 우선: 정확성·완전성·유지보수성이 속도보다 우선. 범위를 몰래 줄이지 않는다. stub·TODO 금지. 막히면 정확한 장애물과 시도한 것을 보고한다.
- 다른 에이전트 위임 금지. 모든 작업을 직접 한다.
- git commit·branch·checkout·reset·stash·rebase·merge·push 금지(부모가 커밋). git diff/status/log 읽기만.
- 운영 DB·원격 호스트·배포·secret 접근 금지. 시험 DB는 testcontainers만. 새 의존성·go.work 수정 금지.
- local-ci(scripts/ci/local-ci.sh)는 부모가 실행한다. 너는 실행하지 않는다.
- 관찰한 명령·결과만 보고하고 관찰하지 않은 것은 [INFERENCE]로 표시한다.
- 최종 응답: JSON 하나(스키마 필드). 자연어 필드는 한국어. commit_message는 `<type>(<scope>): 한국어 요약` + 빈 줄 + 본문.
local:// 참조 대응(노트 파일 안의 local://apsm-*.md는 아래 절대 경로다):
- local://apsm-rules.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/subagent-rules.md
- local://apsm-decisions.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/decisions.md
- local://apsm-design-S2.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S2-runtime-state.md
- local://apsm-design-S3A.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S3A-sendfence-notification.md
- local://apsm-design-S3B.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S3B-youtube-alarm-fence.md
- local://apsm-design-S4.md → /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-notes/design-S4-iris202-reconciler.md
- 인계 문서: /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-handoff.md
- 전체 local-ci(인자 없는 실행)는 부모가 한다. `--integration-tests-only`는 너가 실행한다.
- 인라인 Markdown 폴링 제거는 4a-3 범위다. 이번 단위에서 지우지 않는다.
- youtube(4a-2)는 하지 않는다.
- sendfence 공개 API는 바꾸지 않는다. 바꿔야 하면 이유를 보고하고 멈춘다.
</constraints>