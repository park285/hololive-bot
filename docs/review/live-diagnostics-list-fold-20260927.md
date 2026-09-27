# 라이브 보존 진단·목록 접기 통합 검증

- Plan: `PLN-20260927-live-diagnostics-list-fold`
- Governing: `DEC-20260926-hololive-live-absence-evidence`, `DEC-20260926-hololive-list-reply-fold-default`
- 코드 릴리스: `hololive-bot@d0f8a2feccbb47001ba7193d0df641c9d803cbbc` (`fix/live-evidence-fold-20260927`). 원래 작업트리의 사용자 변경과 보호 파일을 제외한 별도 작업트리에 통합했다. Git publication은 수행하지 않았다.
- 구현 2 agent(진단·접기), 운영 준비 read-only 2 agent(rollout·실제 메시지 증명)를 사용했다. 각 agent는 Opus 5.5 사용을 보고했다. 빌드·테스트·smoke는 부모가 foreground에서 실행했다.

## T01 / AC01 / V01 — 보존 진단

`livequery/queries/snapshot.sql`은 ENDED session의 pending을 pending 채널과 canonical session 채널 각각에 귀속한다. 조회 밖 canonical 채널 때문에 pending 채널의 보존 근거가 사라지는 경우를 수정했다. 같은 채널은 한 번만 집계하며 pending 보존 및 D2 비차단은 유지한다.

실제 PostgreSQL fixture와 repository를 실행한 `TestRepositoryEndedPendingDiagnosticsFollowBothChannels` → PASS(명령 8.144초):

- X 단독, Y 단독, 두 채널 전체 조회: 해당 채널 각각 `EndedPendingEnds=1`, `complete`.
- Y가 운영 roster 밖: X 전체 조회와 Y 직접 조회 모두 진단 유지, `complete`.
- X=Y: 한 번만 집계.
- session LIVE의 채널 불일치: `confirming_end`/`inconsistent`, `unavailable` 유지.
- 모든 경우 pending 행은 보존됐다.

## T02 / AC02 / AC03 / V02 — 최종 텍스트 payload

기존 `BOT_SEE_MORE_FOLD`를 workerapp → dispatchrun 및 직접 YouTube outbox dispatcher로 연결했다. 공유 outbox formatter는 여러 항목 묶음에만 기존 `util`의 250 rune 초과·머리 문단 뒤 ZWSP 500개 알고리즘을 적용한다. 전송층의 모든 텍스트를 강제로 접지 않는다. bot/llm의 기존 목록 경로와 단일·상태·오류 경계를 유지한다. MESSAGE_STYLE_GUIDE와 API/worker/collector 문서에 소비 경계를 반영했다.

최종 smoke는 `internal/app/workerapp/alarm_dispatch_see_more_fold_test.go`에서 실제 DB template → 공개 `dispatchrun.Runner.Start` → 실제 `egress.IrisMessageSender`/kakaoformat → 기록용 Iris client 경로를 실행한다. 큐와 외부 발송만 fixture이며 렌더링은 실제 구현이다. `go test -mod=readonly -race -count=1 -run '^TestAlarmDispatchRunner(Folds|SeeMoreFold)' -v ./hololive/hololive-alarm-worker/internal/app/workerapp` → PASS(12.445초).

| 입력 | 머리 뒤 위치(rune) | padding run | 펼친 본문/최종 rune |
|---|---:|---:|---:|
| 화면 사례와 같은 쇼츠 10개 | 16 | 1 | 1210 / 1710 |
| 영상 10개 | 16 | 1 | 1070 / 1570 |
| 커뮤니티 6개 | 17 | 1 | 589 / 1089 |
| 방송 5분 전 알림 6개 | 14 | 1 | 604 / 1104 |
| 사용자 지정 묶음 본문 | 10 | 1 | 1058 / 1558 |

fold-on에서 패딩을 제거한 문자열은 fold-off 최종 문자열과 정확히 같고 모든 항목 URL이 유지됐다. 기존 template의 제목 길이 제한은 바꾸지 않았다. 짧은 묶음(124 rune), 긴 단일 커뮤니티(287), pre-rendered digest(369)는 패딩 0개와 동일 본문을 확인했다. 이미 접힌 override는 패딩 1개를 유지했다. 채널 override는 DB에 별도로 심었고 저장값을 변경하지 않는다.

직접 dispatcher의 `TestDispatchDeliveryRowsFoldsLongGroupedShortsAtFinalPayload`도 실제 DB/outbox/egress 경로에서 PASS(11.742초)했다. 최초 fixture에는 shorts canonical identity가 없어 admission에서 차단됐으며, fixture에 정당한 canonical ID를 제공해 수정했다. production admission을 완화하지 않았다.

## T03 / V03 — 결합 gate와 예산

Go 공통 환경: `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`.

- 영향 9패키지 `go test -mod=readonly -race -count=1`: API livequery/formatter/llm runtime, worker dispatchrun/youtubedispatch/workerapp/egress, shared outbox format/util → PASS(41.619초). 이후 변경한 workerapp smoke는 위 명령으로 다시 통과했고 최종 전체 race에 포함했다.
- 최종 release worktree에서 `COMPOSE_ENV_FILE=$PWD/deploy/compose/build-only.env.sample ./build-all.sh --build-only --no-bump` → **exit 0, 1112.125초**, `[LOCAL CI] Passed`, `[DONE] Image build complete`.
- 전체 workspace vet/staticcheck/golangci-lint(0 issues)/pinned NilAway/build/일반 테스트/race(-p 4), SQL ownership, hard structure, AP manifest, performance budget를 통과했다. 선택적 `RUN_INTEGRATION_TESTS`는 켜지 않았고 위 DB smoke는 별도로 실행했다.
- 실패 원인을 수정한 뒤 재검증했다: release sibling symlink가 workspace 밖으로 해석되는 문제는 고정 SHA의 실제 dependency worktree로 교체했다. service 테스트의 egress 직접 생성은 workerapp으로 이동하고 공개 runner lifecycle을 실행하도록 고쳤다. gate 예외를 추가하지 않았다.
- dependency worktree: shared-go `66a899270a1129f18537d90aae39adb95a5f3aa5`, iris-client-go `ee375faeeb375c0a17efaee0fd80be1907fea9d1`. 원본 dependency 작업트리의 미커밋 파일을 복사하거나 수정하지 않았다.
- meta `check-stack-db-access-policy`, `check-stack-retry-contract`, `check-stack-projection-tables`, `check-stack-worker-contract` → 모두 exit 0.
- 사용자 보호 파일 5개의 원본 SHA-256은 빌드 전후 동일했다. bulk-stage guard의 문서화된 `IRIS_STACK_ALLOW_BULK_STAGE=1`은 검토한 별도 release 커밋에만 사용했다. 검사/hook을 건너뛰지 않았다.

같은 SQL의 `BenchmarkRepository(LongHistory|UnobservedPending)`, `-benchtime=3x` → PASS(49.136초). 74채널·50,000 종료 이력·1,500,000 absence slot, orphan case는 pending 50,000개 추가. p95는 3회 소표본 최댓값이지 운영 SLO가 아니다.

| case | wall p50 ms | wall p95 ms | EXPLAIN ANALYZE ms | shared hit/read |
|---|---:|---:|---:|---:|
| sparse LIVE | 438.1 | 495.2 | 452.4 | 3157/0 |
| all LIVE | 244.7 | 257.1 | 264.6 | 4003/0 |
| orphan / all | 309.5 | 331.8 | 324.7 | 4752/0 |
| orphan / member | 70.54 | 75.36 | 67.26 | 6620/0 |

## 한계와 운영 경계

이 문서는 로컬 T01~T03/AC01~AC03/V01~V03의 근거다. 실제 카카오톡 화면을 검증했다고 주장하지 않는다. 유효한 지정 테스트 방이 없어 실제 1건 발송/방 열기는 대기한다. 사용자에게 chatId 또는 해당 방의 `!라이브` 실행 KST 시각을 요청했고 읽음 영향도 고지했다. 기존 기본 수신방이나 과거 일회성 승인을 임의로 재사용하지 않는다.

사용자의 “필요한건 전부 승인”에 따른 운영 반영은 별도 release 계획으로 관리한다. 이 로컬 계획의 발송 금지 범위를 소급 변경하지 않는다. 사전 고지된 전체 catalog의 외부 결함(#532 → 누락 DEC)은 재실행하거나 수정하지 않았다. selected plan gate 통과와 전체 catalog 통과는 구분한다.

Fallback delta: 새 fallback/retry/호환 shim 없음. D2 비차단·pending 보존·단일 알림·기존 접기 알고리즘·disable switch를 유지한다.
