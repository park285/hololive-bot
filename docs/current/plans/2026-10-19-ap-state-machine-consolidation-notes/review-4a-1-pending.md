# 4a-1 리뷰·검증 기록(3차 세션 로컬 커밋 완료)

대상: `/home/kapu/work/iris-stack/hololive-bot-apsm-4a`, 브랜치 `refactor/worker-handoff-reconciler`, 기준 `169944c93`(3b + 3a 병합). 아래는 2차 세션의 미커밋 검증 기록이며, 3차 세션에서 입력 변경이 없음을 확인한 뒤 `7f81f1118`로 로컬 커밋했다.

## 상태 요약

- 구현(U4A1) → 1차 리뷰(R4A1, blocker 없음, minor 5·nit 2) → 1차 수정(U4A1fix) → 2차 리뷰(R4A1b, blocker 없음, minor 1·nit 4) → 2차 수정(U4A1fix2) → 부모가 NilAway 지적(`dispatchoutbox/handoff.go` scanHandoffRow의 nullable 관측 열 역참조)을 nil 검사로 고침.
- local-ci 기록: `/tmp/apsm-ci/4a1.log`(실패: integration-tag schema 목록에 329 누락 → 1차 수정에서 해결), `4a1-r2.log`(통과, 1차 수정 시점), `4a1-r3.log`(NilAway 실패 6건), `4a1-r4.log`(nil 검사 뒤 최종 상태, **통과** exit 0).
- 3차 세션: 스텝 3 적대적 리뷰·독립 반증·실DB 검증을 선행했다. worktree의 74개 변경 파일이 모두 최종 CI 완료 이전이고 staged 파일이 없음을 확인해 기존 CI를 재사용했다. 아래 메시지로 hooks를 실행해 `7f81f1118` 커밋을 만들었다. 추가 4a-1 리뷰나 local-ci 재실행은 하지 않았다.

## 부모 결정

- 텍스트 lane `SendMessageAccepted` 202는 bot plane `acceptStoredReply`와 같이 trim한 requestId만 요구한다(success·delivery 무시). 근거: iris-client-go 시험 fixture에 텍스트 202 `delivery:"async"`가 있고 Iris 계약은 delivery 값을 정하지 않는다. Markdown lane 검사(success·queued·requestId)는 그대로다. youtube·notification 텍스트 경로에도 적용된다.
- 설계 이탈 수용: (a) 토큰·요청은 맞지만 멤버가 바뀌었거나 모두 지워진 단위는 전이 없이 확인을 닫고 `ErrHandoffCheckClosed`(Error 로그)로 소유 상실(Warn)과 구분한다. 적용 SQL 5개가 `check_held` 열을 싣는다. (b) batch 32, 상태 조회는 병렬, 정산은 일반 연결 획득 + 동시 2건 슬롯, 조회 5s + 정산 5s(슬롯 대기 포함) < soft claim 15s. 예산 초과 정산은 적용하지 않고 Warn. (c) reconciler는 `dispatchoutbox.NewHandoffConsumer(HandoffRepository, ClaimKeyReleaser)`를 쓴다(별도 Consumer 인스턴스 없음).
- DLQ commit 뒤 claim 키 해제는 정산 예산과 분리된 5s 예산(`handoffClaimKeyReleaseTimeout`)으로 하고, 실패는 `ErrHandoffClaimKeysNotReleased`로 Error.
- migration 329: ADD는 `BEGIN; SET LOCAL lock_timeout '3s'` 블록, VALIDATE는 블록 밖 DO 안 `set_config('lock_timeout','3s',true)`(runner 세션 10s 보존). 329·337은 `public.` 한정 없이 쓴다(141·248 선례, runner search_path 기본값에서 public).
- stack 검사 3종은 경로가 메인 체크아웃으로 고정돼 worktree를 검사하지 않는다. 정적으로 보면 329·337은 그 검사 대상(manual replay 함수, Iris·SDK 코드)을 건드리지 않는다.

## 커밋 메시지 초안

```
refactor(worker): alarm dispatch Iris handoff 확인을 reconciler로 분리

텍스트·Markdown 발송의 202 Iris requestId를 MarkSent와 같은 문장에서 send unit에
기록하고, 새 alarm-dispatch-handoff-reconciler가 SKIP LOCKED soft claim과
트랜잭션 밖 /reply-status 조회로 5s·15s·60s·5분 간격, 지평(attempt_timeout)까지
확인한다. failed는 세대·시도 예산 안이면 :r1/:r2 재발급, 아니면 DLQ와 claim 키
해제, 결과 불명·404·지평 미확인은 격리한다. 확인 claim과 정산은 일반 연결
획득을 쓰고, 상태 조회는 병렬로 두되 정산은 동시 2건으로 묶어 조회 5s + 정산
5s(슬롯 대기 포함)가 soft claim 15s 안에 끝나게 한다. 예산을 넘긴 정산은 적용하지
않고 다음 확인이 다시 판정하므로 경고로 남긴다. DLQ commit 뒤 claim 키 해제는
정산 예산과 떼어 runner 상태 기록과 같은 별도 5s 예산으로 하고, 실패하면
ErrHandoffClaimKeysNotReleased로 오류를 남긴다.

토큰·요청은 맞지만 멤버가 바뀌거나 모두 지워진 단위는 전이 없이 확인을 닫고
ErrHandoffCheckClosed와 별도 오류 로그로 소유 상실과 구분한다. handoff 적용
문장은 확인 소유를 check_held 열로 함께 돌려주어 관측 행이 없어도 이 판별이
맞다. 조회 오류 로그에는 requestId 원문 대신 HTTP 상태와 오류 class만 남긴다.

텍스트 lane은 SendMessageAccepted 202에서 bot plane과 같이 requestId만 요구해
delivery 값과 무관하게 받고, 이 규칙은 youtube·notification 텍스트 발송에도
적용된다. Markdown 검사는 그대로다. SendPreparedMessage가 requestId를 돌려주고,
reconciler는 HandoffRepository를 명시적으로 받는 dispatchoutbox.HandoffConsumer를
쓴다. 순수 판정 패키지 handoffcheck, migration 329(ADD는 SET LOCAL lock_timeout 3s
블록, VALIDATE는 DO 안 set_config 지역값으로 runner 세션 값 보존)·337,
CLOCK_QUERY_POLICIES, integration schema 목록, 계약·runbook·서비스 문서와
T-R1~R3·T-W4·T-M1·정산 동시성·예산 초과·실패 분류·멤버 소거·DLQ 키 해제 예산
시험을 추가한다. Markdown 인라인 polling은 유지한다.
```

## 1차 리뷰(R4A1) 지적 — 모두 반영

- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-api/scripts/migrations/329_alarm_send_unit_handoff_check.sql:7-10, 20-29`
  - 문제: 329는 운영 중에 계속 쓰이는 alarm_dispatch_send_units에 ACCESS EXCLUSIVE를 잡는 DDL 3개를 lock_timeout 없이 실행합니다(ADD COLUMN, NOT VALID ADD CONSTRAINT, VALIDATE). 291 이후 ADD COLUMN·ADD CONSTRAINT를 쓰는 migration은 모두 lock_timeout을 둡니다(291·301·306·307·308·310·313·317·318·321·323). 잠금 대기가 길어지면 그 뒤에 줄 선 runner claim·MarkSent·admin 조회가 모두 멈춥니다. 마이그레이션은 이전 worker가 돌고 있는 동안 적용됩니다.
  - 제안: ADD COLUMN과 NOT VALID 제약 DO 블록을 `BEGIN; SET LOCAL lock_timeout = '3s'; … COMMIT;`으로 감싸 짧은 트랜잭션으로 만듭니다. VALIDATE DO 블록은 CONVENTIONS.md(178 선례)대로 블록 밖 autocommit 문장으로 두되, 앞에 `SET lock_timeout = '3s'`·뒤에 `RESET lock_timeout`을 둡니다(238 선례). 모든 문장이 IF [NOT] EXISTS라 재실행은 그대로 안전합니다.
- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff.go:173-175`
  - 문제: 조회 오류로 재예약할 때마다 원본 queryErr를 Warn 로그에 그대로 남깁니다. SDK의 HTTPError·TransportError는 URL 필드에 `/reply-status/{requestId}` 경로를 담으므로 Iris requestId 원문이 로그에 남습니다. 기존 발송 경로는 이 값을 일부러 가리고(iris_sender.go `replyPollingEndedError`: "반환 오류에는 request id를 남기지 않는 계약"), 같은 PR의 handoffcheck.queryErrorDetail도 저장 문구에서 같은 이유로 HTTP 상태만 남깁니다. Iris 장애 동안에는 단위마다 지평까지 이 로그가 반복됩니다.
  - 제안: 로그에는 원본 오류 대신 handoffcheck.queryErrorDetail과 같은 축약(HTTP 상태나 오류 class)만 남깁니다. 원문이 꼭 필요하면 `strings.ReplaceAll(queryErr.Error(), claim.IrisRequestID, "<request_id>")`로 가린 문자열을 씁니다.
- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff.go:113, 125, 158`
  - 문제: reconciler는 poll 주기마다 도는 배치 claim(LIMIT 32)에 pgxdb.WithControlAcquire를 붙이고, 이어지는 최대 32개 병렬 정산에도 모두 control 획득을 씁니다. pgxdb 계약상 control 표시는 일반 게이트(MaxConns−Reserved)를 우회하는 짧은 정산 전용이고, "입장·조회·배치·유지보수는 일반 작업으로 남긴다"고 되어 있습니다. alarm_worker 풀은 ReservedConns=1입니다. 32개 control 획득이 몰리면 runner의 외부 발송 뒤 MarkSent(같은 control 획득, 5s 예산)와 물리 연결을 다툽니다. MarkSent가 획득 시간을 넘기면 실제로 성공한 발송이 sending에 남아 stale 격리로 굳습니다. runner의 DrainBatch claim은 일반 획득을 씁니다.
  - 제안: ClaimHandoffDue는 일반 획득(`context.WithTimeout(ctx, …)`)으로 바꿉니다. 정산도 병렬 상한을 풀 크기에 맞게 두거나(semaphore) 일반 획득을 쓰는 편이 계약에 맞습니다. 정산은 soft claim·토큰 CAS가 보호하므로 획득이 늦어져도 다음 claim이 다시 판정하고, 안전성은 control 예약에 의존하지 않습니다.
- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md:51`
  - 문제: alarm dispatch의 `last_error_code` 어휘가 계약 문서 두 곳에 있는데, alarm.md만 갱신하고 QUEUE_AND_PUBSUB_CONTRACTS.md는 이전 목록 그대로입니다. 이제 handoff_reissue가 retry 행에 `handoff_failed`를 남기고, claim된 봉투의 Retry.LastErrorCode로 소비자에게 전달됩니다.
  - 제안: QUEUE_AND_PUBSUB_CONTRACTS.md:51 목록에도 handoff 코드 4종을 추가하거나, alarm.md 목록을 참조하도록 바꿔 중복 정의를 없앱니다.
- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/iris_sender.go:71-79, 101-113 (admittedRequestID·acceptedReplyRequestID), CHANGELOG.md:11`
  - 문제: 텍스트 lane을 SendMessageAccepted로 바꾸고 `success && delivery==queued && requestId`를 요구하게 했습니다. IrisMessageSender는 youtube_delivery·notification_delivery와 공유되므로(build_egress.go:56·107·120), 이전에는 성공이던 202가 세 경로 모두에서 결과 불명이 됩니다. 각 경로의 결과는 alarm 격리, youtube FailureOutcomeUnknown, notification QUARANTINED입니다. Iris 계약(current.md:221-222)은 "`/reply` 202 is queue admission only"만 말하고 텍스트 202의 `delivery` 값이나 같은 ID 재전송(duplicate) 응답의 형태는 정하지 않습니다. 이미 운영 중인 bot plane 텍스트 경로(reply_outbox.go acceptStoredReply)는 requestId만 검사하고 `queued`는 요구하지 않습니다. 설계 S4 §1.3이 Q6(Iris 소유자 확인)을 운영 전 조건으로 남겼는데, 구현 보고 risks와 CHANGELOG에는 이 조건과 youtube·notification 영향이 없습니다. CHANGELOG 항목도 제목이 "alarm dispatch"로 한정돼 있습니다.
  - 제안: CHANGELOG·services/alarm-worker.md에 youtube·notification 텍스트 경로의 동작 변화를 적고, Q6(텍스트 202와 duplicate 202의 `delivery` 값)을 배포 전 조건으로 risks에 명시합니다. Iris 쪽 확인 전에는 텍스트 lane만 bot plane처럼 requestId만 검증하는 안도 검토할 만합니다(설계 결정 사항이므로 부모 판단).
- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/handoff.go:175-181`
  - 문제: Consumer의 handoff 메서드 6개는 매 호출마다 `c.repository.(HandoffRepository)` 런타임 단언을 하고, 실패하면 오류를 냅니다. 운영 저장소(PgxRepository)는 항상 이 인터페이스를 구현하므로 실행되지 않는 방어 경로이고, 배선 오류가 기동이 아니라 첫 확인에서야 드러납니다. 규칙의 "증명된 필요 없이 방어 경로를 추가하지 않는다"와도 맞지 않습니다.
  - 제안: NewConsumer(또는 build 단계)에서 한 번 단언해 생성 오류로 만들거나, reconciler용 Consumer를 만들 때 HandoffRepository를 명시적으로 받도록 합니다.
- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff.go:206-209`
  - 문제: 이탈 (a)에서는 토큰과 요청이 남았지만 멤버가 모두 sent가 아닐 때 확인을 영구히 닫고 MissError를 돌려줍니다. 그런데 이 경우에도 "lost its claim or send unit; not applied" Warn 로그 하나만 남습니다. failed·unknown 관측인데도 남은 sent 멤버는 sent로 끝나고, 운영자는 소유를 잃은 경우와 영구 종료된 경우를 로그로 구분할 수 없습니다.
  - 제안: 닫힌 경우를 구분할 수 있게 합니다. 예를 들어 관측 결과(decision.Code)와 함께 Error 수준으로 "check closed without transition"을 남기거나, MissError 사유로 판별해 문구를 나눕니다.

## 2차 리뷰(R4A1b) 지적 — 모두 반영

- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-api/scripts/migrations/329_alarm_send_unit_handoff_check.sql:45-61 (`SET lock_timeout = '3s';` … `RESET lock_timeout;`)`
  - 문제: 운영 runner는 migration 세션의 lock_timeout을 `set_config(..., false)`로 10s로 설정합니다. 이는 세션 수준 SET이라 RESET이 되돌리는 기본값에 들어가지 않습니다. 329 끝의 `RESET lock_timeout`은 값을 runner의 10s가 아니라 서버·role 기본값(시작 매개변수 없음)으로 돌립니다. 그래서 같은 실행에서 뒤에 오는 파일(바로 다음 337 CONCURRENTLY와 그 뒤 파일들)은 runner 계약의 lock_timeout 10s 없이 돕니다. statement_timeout 4m은 남습니다.
  - 제안: VALIDATE를 감싼 SET/RESET을 지우고, DO 블록 안에서 `PERFORM set_config('lock_timeout', '3s', true);`를 먼저 실행합니다(305:43 선례). 그러면 그 문장의 암묵 트랜잭션에만 적용되어 runner 세션 값이 그대로 남습니다. 다른 방법으로는 VALIDATE를 별도의 `BEGIN; SET LOCAL lock_timeout = '3s'; DO …; COMMIT;` 블록으로 둡니다. ADD와 다른 트랜잭션이므로 CONVENTIONS의 잠금 완화 조건도 지킵니다.
- **[minor]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff.go:164-185 (reconcile·settle), /home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/handoff.go:255-265 (DLQAfterHandoff)`
  - 문제: 이번 라운드에서 정산 슬롯 대기를 정산 예산 5s 안에 넣었습니다. DLQAfterHandoff는 DB 전이를 commit한 뒤 같은 ctx로 Valkey `DelMany`를 부릅니다. 슬롯을 늦게 얻어 예산이 거의 남지 않았으면, DLQ 문장은 commit되고 키 해제만 deadline으로 실패할 수 있습니다. 이 단위는 확인이 닫혀(`handoff_next_check_at = NULL`) 다시 claim되지 않으므로, 해제를 다시 시도할 경로가 없습니다. dedup claim 키는 TTL까지 남습니다. 이전 라운드에서는 슬롯 대기가 없어 키 해제에 거의 5s 전부가 남았습니다. runner의 DLQ 해제는 매번 새로 만든 5s state ctx를 씁니다(alarm_dispatch_runner.go:181-185 withStateContext).
  - 제안: DLQ commit 뒤 키 해제는 정산 예산과 떼어 놓습니다. 예를 들어 DLQAfterHandoff 안에서 `releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), <해제 예산>)`를 따로 만듭니다. 아니면 reconciler가 슬롯을 놓은 뒤 새 ctx로 해제하도록 순서를 나눕니다.
- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/handoff.go:183-196 (applyHandoff·handoffCheckHeld), docs/current/runbooks/alarm-worker.md (lost its claim 경고는 '조치 필요 없음')`
  - 문제: 잠근 멤버가 0개이면 SQL의 `held`는 unit_checked만 보고 참이 될 수 있습니다. 그러면 unit_updated가 확인을 영구히 닫습니다. 그런데 Go는 `len(observed) > 0` 조건 때문에 ErrHandoffCheckClosed를 붙이지 않아, reconciler가 'lost its claim … not applied' Warn을 남깁니다. runbook은 이 경고에 '다음 확인이 이어받으므로 조치가 필요 없다'고 안내하는데, 이 경우에는 다음 확인이 없습니다. 실제로 생길 수 있는 경로가 있습니다. retention이 오래된 sent 멤버를 지우는데(alarm_dispatch_retention_delete_sent.sql), 그 시점에 확인이 열린 채 남아 있으면 됩니다. reconciler가 비활성이거나 구 바이너리로 롤백된 채 며칠이 지나면 그렇게 됩니다.
  - 제안: 관측 행이 없을 때 handoff 문장들이 held 값을 함께 돌려주게 합니다. 예를 들어 checked가 비면 `held`만 담은 행을 UNION하거나, 별도 열로 반환합니다. 비용이 크면 runbook의 'lost its claim' 설명에 '멤버가 모두 지워진 단위는 확인이 닫힌다'는 예외를 적습니다.
- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff_test.go:206-250 (TestHandoffReconcilerBoundsConcurrentSettlements)`
  - 문제: 설계에서 안전성을 주장하는 부분, 즉 '슬롯 대기가 예산 5s를 넘긴 정산은 적용하지 않는다'를 고정하는 시험이 없습니다. 지금 시험은 gate를 시간 경과 없이 곧바로 풀기 때문에 동시 2건과 '모두 적용'만 확인합니다. reconciler 수준에서 ErrHandoffCheckClosed가 'check closed' Error 경로로 가는지도 시험하지 않습니다(저장소 dbtest의 ErrorIs만 있음).
  - 제안: 같은 synctest 안에서 gate를 잡은 채 `time.Sleep(alarmDispatchStateTimeout + time.Millisecond)`로 시간을 진행시킵니다. 그다음 release하고, 슬롯 2건만 적용되며 나머지 6건은 consumer 호출이 없는지 확인하는 사례를 더합니다. 또 consumer가 `fmt.Errorf("%w: %w", dispatchoutbox.ErrHandoffCheckClosed, &sendfence.MissError{…})`를 돌려줄 때 Error 로그가 남는지 확인하는 사례를 더합니다.
- **[nit]** `/home/kapu/work/iris-stack/hololive-bot-apsm-4a/hololive/hololive-alarm-worker/internal/egress/alarmdispatch/alarm_dispatch_handoff.go:240-247 (logOutcome 기본 분기)`
  - 문제: 슬롯 대기나 연결 획득으로 예산을 넘긴 정산은, 설계상 soft claim 만료 뒤 다음 확인이 다시 판정하는 정상적인 지연입니다. 그런데도 'alarm dispatch handoff check failed'를 Error 수준으로 남깁니다. DB가 느려 한 batch 32건 가운데 뒤쪽 정산들이 예산을 넘기면, 주기마다 Error가 여러 건 쌓일 수 있습니다.
  - 제안: `errors.Is(err, context.DeadlineExceeded)`이면 Warn으로 낮추고 '다음 확인이 다시 판정' 문구를 붙입니다.

## 남은 위험(구현·리뷰 보고)

- R3: handoff 미확인 sent 단위가 지평까지 `sent`로 보인다(birthday_stream_runner_0104_03.sql, youtube_live_session_source_0231_04.sql 등 sent를 완료로 읽는 소비자).
- 텍스트 202 requestId만 요구: duplicate 202에 requestId가 없으면 결과 불명(Q6, Iris 확인 필요).
- MarkSent·handoff 문장의 잠금 순서(unit → member)는 initplan 평가 순서에 기대며 실측하지 않았다 [INFERENCE].
- 정산 동시성 2는 풀 설정에서 파생하지 않은 고정값이다. 32건이 5s 안에 끝난다는 판단은 측정하지 않았다 [INFERENCE].
- 337은 CONCURRENTLY 단일 문장이므로 autocommit으로 적용되고, migration을 바이너리보다 먼저 적용한다.
