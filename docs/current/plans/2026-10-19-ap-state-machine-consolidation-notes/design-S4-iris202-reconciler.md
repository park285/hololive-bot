# S4 설계: Iris 202 동기 추상화(B안) — 워커 reconciler와 봇 plane

기준 코드: `/home/kapu/work/iris-stack/hololive-bot-apsm-1` (= origin/main `cb105cc97`). 계획 원문: `docs/current/plans/2026-10-19-ap-state-machine-consolidation.md`(이하 "계획") 4.2·6절 4단계·8절.
경로 약어: `W/` = `hololive/hololive-alarm-worker/internal/`, `B/` = `hololive/hololive-api/internal/planes/bot/`, `SH/` = `hololive/hololive-shared/pkg/`, `MIG/` = `hololive/hololive-api/scripts/migrations/`, `ICG/` = `/home/kapu/work/iris-stack/iris-client-go/`.
저장소는 읽기만 했다. 설정 값(stack-secrets 프로필)은 읽지 않았다.

---

## 0. 계획 전제와 코드가 어긋나는 점 (구현 전에 알아야 함)

| # | 계획 문장 | 코드 사실 | 영향 |
|---|---|---|---|
| C1 | 4.2 "오늘의 워커 동작(폴링 → `failed` → 같은 ID 재시도 → 409 → 재발급)과 결과가 같다"(계획:109) | markdown `failed`는 경로마다 다르게 끝난다. **alarm-dispatch는 quarantine**: `ErrHandoffFailed`는 `Classify`=Failed이고(`W/egress/sendoutcome/outcome.go:55-57`) 409 FAILED도, 429/502/503도, TransportAmbiguous도 아니어서 `persistPostSendingFailure`의 마지막 줄로 격리된다(`W/egress/alarmdispatch/alarm_dispatch_runner_failure.go:107-130`, 129행). **youtube는 영구 실패**(`W/egress/youtubedispatch/send_engine_lifecycle.go:189-191`). **notification만** `markItemFailed`로 같은 ID 재시도 → 409 → 재발급이다(`W/egress/notificationdelivery/dispatcher.go:327-339`). **봇 plane은 `dead`**(`B/runtime/durable_outbox_policy.go:27-29`). | B안의 `failed → retry/재발급`은 alarm-dispatch·youtube·봇에서는 **새로 생기는 재발송**이다. 결정 자체(B안)는 확정이므로 설계는 그대로 따르되, 결과가 "같다"는 근거는 notification에만 맞다. 봇은 open question Q2. |
| C2 | 6절 "`sendoutcome`을 Success·Failed·TransportAmbiguous로 축소" | `OutcomeUnknown`의 출처는 handoff 폴링만이 아니다. admission 단계의 409 `CLIENT_REQUEST_ID_OUTCOME_UNKNOWN`(`outcome.go:37`, `containsStructuredUnknown` 98-111)과 형식이 깨진 202(`W/egress/iris_sender.go:77-79,97-108`)도 결과 불명이다. 이를 Failed로 합치면 notification은 `FAILED`로 재시도하고(`dispatcher.go:338-339`) 격리 대신 실패로 기록한다. TransportAmbiguous로 합치면 alarm-dispatch가 같은 ID로 재시도한다(`alarm_dispatch_runner_failure.go:119-126`). 둘 다 `DEC-20260731-reply-outcome-unknown-fail-closed` 의도와 맞지 않는다. | Kind 4종은 유지하고 **handoff 계열 sentinel만 제거**한다(§1.3). 계획 문장과 다르므로 Q1로 확인을 요청한다. |
| C3 | 6절 "dispatchoutbox quarantine 임계 3배 규칙 제거" | 3배는 생성자 기본값일 뿐이다(`W/service/alarm/dispatchoutbox/consumer.go:103-111`). 운영 값은 프로필 `alarm_dispatch.quarantine_threshold_ms`이고 양수 필수다(`W/config/worker_profile.go:61`, `worker_profile_types.go:23`, 주입 `W/app/workerapp/build_egress.go:302`). 그래서 기본값 분기는 운영에서 실행되지 않는다. 프로필 settings는 strict decode라서 키 집합이 struct와 같아야 한다(shared-go v2.14.0 `pkg/workercontract/settings.go` `DecodeWorkerSettings` 주석). | 코드에서 할 일: 기본값·주석·clamp 제거와 관계 검증 추가(§1.4). 운영 값을 3×lease에서 낮출지는 stack-secrets 변경이라 별도 승인 대상이다(Q4). |
| C4 | 6절 reconciler 대상 "`status='sent' AND handoff_checked_at IS NULL AND sent_at <= …`"(delivery 행 기준) | Iris 요청 하나는 **send unit** 하나다. unit에는 `client_request_id`·`request_generation`·`request_delivery_ids`(1..10)가 있다(`hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql:162-182`). delivery마다 확인하면 같은 요청을 최대 10번 조회하고 멤버 상태가 갈라질 수 있다. | handoff 열은 `alarm_dispatch_send_units`에 둔다(§2.2). 조건의 의미("sent 이고 미확인이며 5초 경과")는 같다. 계획의 방향 변경이 아니라 저장 위치를 정한 것이다. |
| C5 | text 경로 포함(계획:114) | text lane은 `SendMessage`만 호출해 Iris requestId를 받지 않는다(`W/egress/iris_sender_prepared.go:39-44`). 계약·SDK 문서 어디에도 requestId = clientRequestId라는 문장이 없다(ICG `README.md`, `Iris/docs/contracts/current.md` 검색 결과 0건). | text lane을 `SendMessageAccepted`로 바꿔 202의 `requestId`를 저장한다(§1.2). 봇 plane은 이미 이 방식이다(`B/internal/bot/orchestration/transport/reply_outbox.go:144-156`, `iris_request_id` 열). |

---

## 1. 워커: 현재 구조, 제거·축소 대상 전수, 축소 후 라우팅

### 1.1 현재 handoff 폴링 (전수)

| 위치 | 내용 |
|---|---|
| `W/egress/iris_sender.go:14` | `replyStatusPollInterval = 250ms` |
| `iris_sender.go:18-22` | `IrisClient` 인터페이스. `GetReplyStatus`는 21행 |
| `iris_sender.go:31-36, 56-69` | `IrisMessageSender.replyStatusPollInterval` 필드와 생성자 기본값(59행) |
| `iris_sender.go:71-91` | `sendMarkdown`. 202 검증 뒤 `waitForReplyHandoff`(86-88) |
| `iris_sender.go:97-108` | `acceptedReplyRequestID`. `success && delivery=="queued"`이 아니거나 requestId가 비면 `ErrHandoffOutcomeUnknown` |
| `iris_sender.go:110-143` | `waitForReplyHandoff`. `time.Tick` 250ms 폴링을 ctx(=attempt timeout)까지 반복 |
| `iris_sender.go:145-160` | `replyPollingEndedError` |
| `iris_sender.go:162-187` | `assessReplyHandoffPoll`. trim·lower 뒤 `iris.ClassifyReplyStatus`. `failed`는 `ErrHandoffFailed`, `outcome_unknown`·모르는 값·nil·requestId 불일치는 `ErrHandoffOutcomeUnknown` |
| `W/egress/sendoutcome/outcome.go:23-28` | sentinel `ErrHandoffOutcomeUnknown`, `ErrHandoffFailed` |
| `outcome.go:37, 55-57` | Classify의 handoff 분기 |
| `W/egress/youtubedispatch/send_engine_lifecycle.go:189-191` | `ErrHandoffFailed` → `FailurePermanent` |
| `W/egress/youtubedispatch/provider_attempt.go:39` | `ErrHandoffFailed` → `AttemptFailed` 지표 |
| `W/service/alarm/dispatchoutbox/consumer.go:103-111` | "Markdown handoff polling 포함" 이유의 `3 * lease` 기본값과 lease clamp |

attempt timeout 적용 범위는 다음과 같다.
- alarm-dispatch: `Runner.sendPreparedRequest`(`W/egress/alarmdispatch/alarm_dispatch_runner.go:348-355`)가 `SendPreparedMessage` 호출 전체(386행), 즉 admission과 폴링을 함께 감싼다. 값은 `workers.alarm_dispatch.executor.attempt_timeout`(`build_egress.go:321`)이다.
- notification: `attemptSend`(`dispatcher.go:286-314`), 값은 `build_egress.go:223`.
- youtube: `DeliverySendTimeout`(`build_egress.go:408,425`).
- 프로필 검증 `alarm_dispatch attempt_timeout < lease_ms`(`W/config/worker_profile.go:118-120`).

폴링을 지우면 세 경로 모두 **코드 변경 없이** attempt timeout이 admission 호출(SDK `WithReplyRetry` 포함, `SH/service/delivery/runtime_iris_client.go:55-60`)에만 걸린다.

### 1.2 `sendoutcome.Classify` 4종과 현재 경로별 라우팅

| 증거 | Kind | alarm-dispatch | youtube | notification |
|---|---|---|---|---|
| 성공(markdown은 `handoff_completed`, text는 202) | Success | `MarkSent`(`alarm_dispatch_runner.go:344`) | SENT | `markItemSent`(`dispatcher.go:321-325`) |
| markdown `failed`(`ErrHandoffFailed`) | Failed | **quarantine**(`alarm_dispatch_runner_failure.go:129`) | **FailurePermanent**(`send_engine_lifecycle.go:189-191`) | 같은 ID 재시도(`dispatcher.go:338-339`) |
| `outcome_unknown`·폴링 deadline·형식 깨진 202(`ErrHandoffOutcomeUnknown`) | OutcomeUnknown | quarantine(`…_failure.go:111-113`) | FailureOutcomeUnknown(`send_engine_lifecycle.go:175-178`) | QUARANTINED(`dispatcher.go:336-337`) |
| 409 `CLIENT_REQUEST_ID_OUTCOME_UNKNOWN` | OutcomeUnknown | quarantine | FailureOutcomeUnknown | QUARANTINED |
| 409 `CLIENT_REQUEST_ID_FAILED` | Failed | 세대 재발급 `reissueFailedRequest`(`…_failure.go:115-117, 352-366`) | `errRequestReissued` → retryable(`send_engine_lifecycle.go:181-183`) | `reissueRequest`(`dispatcher.go:327-330`) |
| ctx 오류·응답 유실(TransportAmbiguous) | TransportAmbiguous | 저장 요청이 있으면 같은 ID 재시도(`…_failure.go:119-126`), 없으면 quarantine | retryable(`send_engine_lifecycle.go:193-195,204-205`) | QUARANTINED(`dispatcher.go:336-337`) |
| 429/502/503 | Failed | 재시도, 최대 6회(`…_failure.go:173-179, 255-268`) | rate limited/transport retryable | 재시도 |
| 그 밖의 확정 실패(4xx·dial) | Failed | quarantine(129행) | Permanent | 재시도 |

### 1.3 축소안 (B안 적용 후)

**제거**
- `iris_sender.go:14`, 31-36의 `replyStatusPollInterval` 필드, 59행, 86-88행, 110-187행 전부. 폴링·`replyPollingEndedError`·`assessReplyHandoffPoll`을 지운다.
- `IrisClient`(18-22)에서 `GetReplyStatus`를 빼고 `SendMessage`를 `SendMessageAccepted(ctx, room, message, opts...) (*iris.ReplyAcceptedResponse, error)`로 바꾼다. 구체 타입 `*delivery.RuntimeIrisClient`는 `*iris.RebindingClient`를 내장하고(`SH/service/delivery/runtime_iris_client.go:26-31`), 이 타입에 `SendMessageAccepted`가 있다(ICG `internal/client/rebind/rebind.go:367`).
- `sendoutcome.ErrHandoffFailed`(`outcome.go:26-27`)와 `outcome.go:55-57` 분기. 생산자가 사라진다.
- `send_engine_lifecycle.go:189-191`, `provider_attempt.go:39`의 `errors.Is(err, sendoutcome.ErrHandoffFailed)` 항.
- `consumer.go:103-111`의 기본값·clamp(§1.4).

**이름 변경(의미 축소)**
- `ErrHandoffOutcomeUnknown` → `ErrAdmissionOutcomeUnknown`. 문서 주석은 "202 응답이 durable admission을 증명하지 못함(빈 응답·`queued` 아님·requestId 없음)"으로 바꾼다. 남는 생산자는 `acceptedReplyRequestID`와 nil 응답 두 곳이다.
- Kind는 4종을 유지한다(C2, Q1). `OutcomeUnknown` = "Iris 쪽 결과 불명 증거(409 OUTCOME_UNKNOWN 또는 admission 미증명 202)".

**발송 계약 변경**
- `SH/service/delivery/sender.go:6-9`의 `SendPreparedMessage(ctx, room, body, route, clientRequestID) error`를 `(irisRequestID string, err error)`로 바꾼다. 호출자 3곳: `alarm_dispatch_runner.go:386`, `W/egress/youtubedispatch/send_engine_support.go`, `W/egress/notificationdelivery/request.go`.
- `iris_sender_prepared.go:25-47`: text·markdown 모두 202를 받고 같은 `acceptedReplyRequestID`로 검증한 뒤 requestId를 반환한다. text 202의 `delivery`도 `queued`라는 근거는 SDK fixture뿐이다(ICG `internal/client/transport/client_test.go:682`). [INFERENCE] Iris 계약 문서에는 명시가 없다. 시험 T-W4로 고정하고, 운영 전 Iris 소유자 확인을 Q6에 넣었다.

**축소 후 라우팅(발송 단계)** — Success의 의미는 "durable admission 완료"다.

| 증거 | Kind | alarm-dispatch | youtube | notification(3단계 흡수 후 alarm-dispatch와 같음) |
|---|---|---|---|---|
| 202 + 유효 requestId | Success | `sent` + unit handoff 열 기록(§2.3) | SENT + send_request handoff 열(§4) | (흡수) |
| 202 형식 깨짐, 409 OUTCOME_UNKNOWN | OutcomeUnknown | quarantine(현행 유지) | FailureOutcomeUnknown(유지) | quarantine |
| 409 FAILED | Failed | 재발급(유지) | 재발급(유지) | 재발급 |
| TransportAmbiguous | TransportAmbiguous | 같은 ID 재시도(유지) | retryable(유지) | (흡수 후 alarm 규칙) |
| 429/502/503, 기타 Failed | Failed | 유지 | 유지 | 유지 |

handoff 결과(`handoff_completed`/`failed`/`outcome_unknown`/진행 중)는 발송 경로에서 사라지고 reconciler 라우팅(§2.4)으로만 간다.

### 1.4 quarantine 임계 3배 규칙

- `consumer.go:103-111`을 삭제한다. 생성자는 `quarantineThreshold < lease`이면 clamp하지 않고 오류를 낸다. `NewConsumer`는 이미 오류를 반환한다(71행).
- `W/config/worker_profile.go` `relationshipProblems`(100-133)에 `"alarm_dispatch quarantine_threshold_ms must be at least lease_ms"`를 추가한다. 폴링이 없어지면 `sending` 체류는 attempt timeout(< lease, 118행) + 상태 기록 예산 5s(`alarm_dispatch_runner.go:174`)로 묶인다. 따라서 lease 이상이면 진행 중 발송을 격리하지 않는다.
- 프로필 키 `quarantine_threshold_ms`는 유지한다. strict decode 때문에 키를 없애면 stack-secrets와 함께 바꿔야 한다.
- 운영 값 축소 여부는 Q4. 현재 운영 값이 lease보다 작으면 새 검증 때문에 기동이 실패하므로, 배포 전에 stack-platform-ops로 `quarantine_threshold_ms ≥ lease_ms`를 확인해야 한다(R5).

---

## 2. 워커 reconciler 설계 (alarm-dispatch; 3단계 흡수 뒤 notification 포함)

### 2.1 attempt timeout과 지평 (작업 항목 3)

- 로드 위치: `W/config/worker_profile.go:13-33` `LoadWorkerProfile` → shared-go `workercontract.LoadProfileFromEnv("hololive","alarm-worker")`. `STACK_WORKER_PROFILE_FILE`이 가리키는 파일을 읽는다(shared-go `pkg/workercontract/profile_env.go:11`).
- 필드: `Loaded.Profile.Workers["alarm_dispatch"].Executor.AttemptTimeout`. 타입은 `DurationPolicy`이고 `Mode`와 `Milliseconds`를 가진다(shared-go `profile.go:73-76`).
- 검증: 모드는 `fixed` 필수(`worker_profile.go:42`), shared-go `validateDuration`(`profile.go:429`), `< lease_ms`(118-120).
- 소비 위치: `alarmDispatchRunnerConfig`(`build_egress.go:313-323`).
- **설계: 지평 `handoffHorizon = AttemptTimeout`. 새 설정 키는 만들지 않는다.** 계획 8절의 "같은 값으로 시작"과 맞는다. 키를 추가하면 strict decode 때문에 stack-secrets 프로필과 함께 배포해야 한다. 지평을 attempt timeout과 다르게 늘리려면 그때 `alarm_dispatch.handoff_horizon_ms` 키와 stack-secrets 변경이 필요하다(Q3).
- 확인 간격(계획 그대로, Go 상수): 첫 확인 SENT+5s, 이후 15s → 60s → 5min(이후 5min 반복). 모든 예정 시각은 `admitted_at + horizon`을 넘지 않게 자른다. 그래서 마지막 확인은 지평 시각에 정확히 한 번 일어난다. 지평 H가 attempt timeout(< lease_ms)이면 실제 확인 시각은 `{5s, 20s, 80s, 380s, …} ∩ (0,H) ∪ {H}`다. H ≤ 20s이면 5s와 H 두 번이다(R6).
- Iris 재시도 창(계획:110, 약 1.75~2.2s)보다 5s가 길다. 따라서 첫 확인에서 `failed`가 아직 `queued`로 보이는 경우는 bridge 지연이 그 창을 넘을 때뿐이다.

### 2.2 스키마 (migration 4a-1)

번호: 현재 manifest 최대는 324다(`MIG/manifest.txt` 끝 `185 324_…`, main 체크아웃도 324). 0~3단계가 먼저 migration을 추가하므로 **구현 시점의 max+1**을 쓴다(`MIG/CONVENTIONS.md` "번호"). 파일명: `NNN_alarm_send_unit_handoff_check.sql`. manifest에 한 줄 추가.

```sql
ALTER TABLE public.alarm_dispatch_send_units
    ADD COLUMN IF NOT EXISTS handoff_request_id text,
    ADD COLUMN IF NOT EXISTS handoff_next_check_at timestamptz,
    ADD COLUMN IF NOT EXISTS handoff_checked_at timestamptz;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'public.alarm_dispatch_send_units'::regclass
          AND conname = 'chk_alarm_send_unit_handoff_shape'
    ) THEN
        ALTER TABLE public.alarm_dispatch_send_units
            ADD CONSTRAINT chk_alarm_send_unit_handoff_shape CHECK (
                (handoff_request_id IS NULL OR length(handoff_request_id) BETWEEN 1 AND 256)
                AND (handoff_request_id IS NULL OR request_body_hash IS NOT NULL)
                AND (handoff_next_check_at IS NULL
                     OR (handoff_request_id IS NOT NULL AND handoff_checked_at IS NULL))
                AND (handoff_checked_at IS NULL OR handoff_request_id IS NOT NULL)
            ) NOT VALID;
    END IF;
END
$$;

ALTER TABLE public.alarm_dispatch_send_units
    VALIDATE CONSTRAINT chk_alarm_send_unit_handoff_shape;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alarm_dispatch_send_units_handoff_due
    ON public.alarm_dispatch_send_units (handoff_next_check_at, id)
    WHERE handoff_next_check_at IS NOT NULL;
```

- 의미는 다음과 같다. 상태 어휘는 늘리지 않는다.
  - "admission 완료, handoff 미확인" = `handoff_request_id IS NOT NULL AND handoff_checked_at IS NULL`. 이때 `handoff_next_check_at`이 다음 확인 시각이다.
  - 확인 완료 = `handoff_checked_at` 기록, `handoff_next_check_at` NULL.
  - 격리·DLQ로 끝난 unit은 `handoff_checked_at`을 기록해 다시 고르지 않는다.
- 확인 횟수 열은 두지 않는다. 다음 간격은 DB가 계산한 경과 시간(`clock_timestamp() - min(sent_at)`)으로 정하므로 횟수가 필요 없다(§2.4).
- `chk_alarm_dispatch_deliveries_state_shape`(golden 98행)는 바꾸지 않는다. reconciler 전이는 `sent→retry`(제약 없음), `sent→dlq`(`dlq_at` 기록), `sent→quarantined`(`quarantined_at` 기록)로 기존 shape을 만족한다. `chk_alarm_send_request_shape`(golden 179행)도 그대로 두고, 위의 새 CHECK가 `request_body_hash`와 정합만 건다.
- 모든 새 열은 nullable이고 기존 행은 NULL이라 backfill이 없다. 과거 sent unit은 확인 대상이 아니다. 기존 markdown은 이미 폴링했고, 과거 text의 미확인은 소급하지 않는다.
- 이전 바이너리는 새 열을 쓰지 않으므로 migration을 먼저 적용해도 안전하다.
- dbtest: `hololive/hololive-dbtest/alarm_send_unit_handoff_migration_test.go`(CHECK 거부 행 3종과 재적용 멱등)와 `testdata/schema_snapshot.golden.sql` 갱신(`SCHEMA_SNAPSHOT_UPDATE=1`).

### 2.3 발송 성공 시 기록 (MarkSent 확장)

- `Consumer.MarkDispatched(ctx, envelopes)`(`consumer.go:237-250`)를 `MarkDispatched(ctx, envelopes, irisRequestID string)`로 바꾼다. 호출자는 `alarm_dispatch_runner.go:339-344`이고, `sendPreparedRequest`가 requestId를 반환하게 한다.
- `PgxRepository.MarkSent`(`repository_transitions.go:45-64`)와 `queries/repository_transitions_0040_02.sql`을 한 문장으로 바꾼다. 같은 문장 안에서 delivery `sending→sent`와 unit handoff 열을 기록한다.

```sql
WITH sent AS (
    UPDATE alarm_dispatch_deliveries
    SET status = 'sent', sent_at = clock_timestamp(),
        locked_by = NULL, locked_at = NULL, lock_expires_at = NULL,
        updated_at = clock_timestamp()
    WHERE id = ANY($1) AND status = 'sending' AND locked_by = $2
    RETURNING id, send_unit_id
), unit AS (
    UPDATE alarm_dispatch_send_units u
    SET handoff_request_id = $3,
        handoff_next_check_at = clock_timestamp() + ($4::bigint * INTERVAL '1 millisecond'),
        handoff_checked_at = NULL
    WHERE u.id = (SELECT min(send_unit_id) FROM sent)
      AND (SELECT count(id) FROM sent) = cardinality($1)
    RETURNING u.id
)
SELECT (SELECT count(id) FROM sent), (SELECT count(id) FROM unit)
```

- `$4` = Go 상수 `handoffFirstCheckDelay = 5s`이고, 정책은 Go가 정한다. 시계는 `clock_timestamp()`로 통일하고 `scripts/architecture/check-sql-ownership.py` `CLOCK_QUERY_POLICIES`(54-65)에 `repository_transitions_0040_02.sql`과 `handoff_*.sql`을 clock_timestamp로 추가한다.
- 행 수 검증은 기존 `validatePostSendRowsAffected`를 유지한다. unit 갱신 0행(부분 적용)은 typed 오류로 올린다(계획 4.1 "미스는 typed error").
- 3단계(S3A)가 같은 파일의 fence를 바꾸므로 **3단계 정리 뒤에 이 SQL을 얹는다**(§6).

### 2.4 reconciler 동작

**구성 요소**
- `W/service/alarm/dispatchoutbox/handoff.go`(새 파일): 저장소 메서드와 `Consumer` 래퍼. SQL 파일은 `dispatchoutbox/queries/handoff_*.sql`.
- `W/egress/alarmdispatch/alarm_dispatch_handoff.go`(새 파일): `HandoffReconciler`(Scheduler: `Start`/`RunOnce`)와 순수 함수 `decideHandoff`.
- 등록: `appendAlarmDispatchRunner`(`build_egress.go:127-` 이후) 안에서 alarm-dispatch가 활성일 때만 `workerruntime.NamedScheduler{Name: "alarm-dispatch-handoff-reconciler"}`를 추가한다. 기존 maintenance·backlog observer 등록(`build_egress.go:93-102`)과 같은 방식이다. Iris client는 `buildNotificationEgress`의 `irisClient`(`build_egress.go:45-53`)를 좁은 인터페이스 `replyStatusClient{ GetReplyStatus(ctx, id) (*iris.ReplyStatusSnapshot, error) }`로 넘긴다.
- 주기: idle 대기는 기존 `alarm_dispatch.poll_interval_ms`를 쓴다(새 키 없음). 배치 50, 상태 조회 timeout 5s, soft-claim 15s는 Go 상수다.

**단계(트랜잭션 밖에서 외부 호출)**
1. claim — `handoff_claim_due.sql`, autocommit 한 문장:
   ```sql
   WITH due AS MATERIALIZED (
       SELECT id FROM alarm_dispatch_send_units
       WHERE handoff_next_check_at <= clock_timestamp()
       ORDER BY handoff_next_check_at, id
       LIMIT $1
       FOR UPDATE SKIP LOCKED
   )
   UPDATE alarm_dispatch_send_units u
   SET handoff_next_check_at = clock_timestamp() + ($2::bigint * INTERVAL '1 millisecond')
   FROM due WHERE u.id = due.id
   RETURNING u.id, u.handoff_request_id, u.client_request_id, u.request_generation,
             u.request_delivery_ids, u.handoff_next_check_at AS claim_token,
             (SELECT floor(extract(epoch FROM clock_timestamp() - min(d.sent_at)) * 1000)::bigint
                FROM alarm_dispatch_deliveries d
               WHERE d.send_unit_id = u.id AND d.status = 'sent') AS elapsed_ms,
             (SELECT max(d.attempt_count)
                FROM alarm_dispatch_deliveries d
               WHERE d.send_unit_id = u.id AND d.status = 'sent') AS attempt_count
   ```
   `handoff_next_check_at`을 앞으로 미는 것이 soft claim이다. 반환값 `claim_token`은 뒤 단계의 CAS 토큰이다. 발송 lease는 잡지 않는다.
2. 관측 — 행마다 `GetReplyStatus(ctx5s, handoff_request_id)`.
3. 적용 — unit별 트랜잭션 하나. `lockSendRequest`(`send_request.go:117-126`)와 `send_request_members.sql` 잠금 순서(unit → members)를 재사용한다. fence:
   - unit: `handoff_next_check_at = claim_token AND handoff_checked_at IS NULL AND client_request_id = 관측 시 값`
   - members: 집합이 `request_delivery_ids`와 같고 전원 `status='sent'`
   
   fence 미스는 `ErrHandoffFence` typed 오류로 올리고 전이하지 않는다. 경고 로그를 남기고, 다음 claim 때 다시 판정한다.

**판정 `decideHandoff(obs, err, elapsed, horizon, generation, attemptCount)`** — 정책은 Go에 둔다. 상태 정규화는 현행 trim+lower를 유지한다(`iris_sender.go:171-173`).

| 관측 | 조건 | 행동 | SQL |
|---|---|---|---|
| `handoff_completed`, requestId 일치 | — | 확인 완료 | `handoff_complete.sql`: `handoff_checked_at=clock_timestamp(), handoff_next_check_at=NULL` |
| `failed` | `generation < iris.ReplyReissueMaxGenerations(=2, ICG iris/reply_reissue.go:15)` 이고 `attemptCount+1 < alarmDispatchMaxAttempts(=3, …_failure.go:255-258)` | retry + 세대+1 | `handoff_requeue_deliveries.sql`(members `sent→retry`, `attempt_count+1`, `next_attempt_at=clock_timestamp()+$ms`, `sent_at=NULL`, `last_error_code='handoff_failed'`) + 기존 `send_request_reissue.sql` 확장(§2.5) |
| `failed` | 그 밖 | dlq | `handoff_dlq_deliveries.sql`(`sent→dlq`, `dlq_at`, `attempt_count+1`, RETURNING `claim_keys`) + `handoff_complete.sql`. commit 뒤 claim key 해제(`completeFailureFinalization`과 같은 규칙, `…_failure.go:82-96`) |
| `outcome_unknown`, 모르는 상태, nil snapshot, requestId 불일치 | — | quarantined | `handoff_quarantine_deliveries.sql`(`sent→quarantined`, `quarantined_at`, `last_error_code='handoff_outcome_unknown'`) + `handoff_complete.sql` |
| HTTP 404(`errors.AsType[*iris.HTTPError]`, `StatusCode==404`; ICG `internal/client/transport/errors.go:45-83`) | — | quarantined | 위와 같고 `last_error_code='handoff_status_missing'` |
| `queued`/`preparing`/`prepared`/`sending` | `elapsed < horizon` | 다음 확인 | `handoff_reschedule.sql`: `handoff_next_check_at = LEAST(clock_timestamp() + $step, (SELECT min(sent_at) …) + $horizon)` |
| 위와 같음 | `elapsed ≥ horizon` | quarantined("handoff 미확인") | `last_error_code='handoff_unconfirmed'` |
| 조회 오류(404 외: transport·5xx·401·timeout) | `elapsed < horizon` | 전이 없이 다음 확인 | `handoff_reschedule.sql` |
| 위와 같음 | `elapsed ≥ horizon` | quarantined | `handoff_unconfirmed`. 현행 폴링이 조회 실패로 deadline을 넘기면 `ErrHandoffOutcomeUnknown`으로 격리하는 것(`iris_sender.go:138-141,147-160`)과 같다 |

- `$step`은 Go가 `elapsed`로 고른다: `<20s → 15s`, `<80s → 60s`, 그 밖 `5min`. 시각 계산은 SQL `clock_timestamp()` 한 곳에서 하고 Go 시각을 DB와 비교하지 않는다(계획 4.1).
- 재시도 backoff는 `attempt*5s`로 현행 `nextAlarmDispatchRetry`(`…_failure.go:321-350`)와 같다. 값은 Go가 ms로 넘긴다.
- 지표는 기존 `observePGRetryScheduled`/`observePGDLQ`/`observePGQuarantined`(`consumer.go:279-294,385`)를 재사용한다. 새 지표는 만들지 않는다.
- `last_error_code` 어휘 3종(`handoff_failed`, `handoff_outcome_unknown`, `handoff_unconfirmed`)과 `handoff_status_missing`이 늘어난다. DB CHECK는 없고 계약 문서 목록만 고친다(§7).
- `sent→retry`·`dlq`가 생기면 `sent` 행을 "완료"로 읽는 소비자에게 최대 지평만큼 창이 생긴다(R3): 생일 중복 판정(`W/service/celebration/queries/birthday_stream_runner_0104_03.sql:7`), 방송 알림 판정(`W/service/alarm/checker/checking/queries/youtube_live_session_source_0231_04.sql`).

### 2.5 재발급 경로 재사용 (`:r1/:r2`)

- 계획대로 reconciler가 직접 세대를 올린다. 기존 `ReissueSendRequest`(`send_request.go:177-227`)는 fence가 `status='sending' AND locked_by`(`fenceSendRequest(..., "sending")` 196행, `send_request_reissue_delivery.sql`)라서 `sent` 행에 쓸 수 없다. 그래서 같은 파일에 `ReissueAfterHandoffFailure(ctx, unitID, ids, claimToken, expectedID, retryAfter, errText)`를 추가한다. 이 메서드는 `lockSendRequest`와 세대 상한 검사(204행), `iris.ReissuedClientRequestID(base, gen+1)`(208행)을 그대로 쓰고, delivery 전이만 `handoff_requeue_deliveries.sql`(fence `status='sent'`)로 한다.
- `send_request_reissue.sql`을 다음으로 바꿔 두 호출자가 공유한다. 기존 호출자 시점에는 handoff 열이 이미 NULL이라 동작이 같다.
  ```sql
  UPDATE alarm_dispatch_send_units
  SET client_request_id = $2, request_generation = $3,
      handoff_request_id = NULL, handoff_next_check_at = NULL, handoff_checked_at = NULL
  WHERE id = $1
  ```
- 다음 claim은 기존 경로를 그대로 탄다. `LoadSendRequest`가 고정 본문을 쓰고, `client_request_id`가 새 세대(`:r1`/`:r2`)다. 새 행은 만들지 않는다(계획:108).
- 근거 강도: `GetReplyStatus`의 `failed`는 409 `CLIENT_REQUEST_ID_FAILED`와 같은 Iris admission 상태에서 나온다고 본다. [INFERENCE] Iris 계약 문서에는 "409 FAILED ⇔ admission state failed"라는 문장이 없다. SDK README는 "Failed 분류만으로 재전송이 안전하다고 판단해서는 안 됩니다"라고 쓴다(ICG `README.md:131-133`). 계획 4.2(103-107)는 사용자가 Iris 코드로 `failed` = pre-dispatch 소진 또는 `Rejected`임을 확인했다. 대안(retry만 하고 다음 발송에서 409 FAILED를 받아 기존 `reissueFailedRequest`로 세대를 올림)은 Iris POST가 1회 늘지만 409라는 직접 증거를 쓴다. 계획 결정을 따르는 것을 권고하되 Q5로 남긴다.

### 2.6 youtube-outbox·notification 적용 방식

- **notification**: 3단계에서 alarm-dispatch 소비자로 흡수된다(계획:164). 흡수 뒤에는 send unit을 쓰므로 §2.4가 그대로 적용된다. 흡수 전에 폴링을 지우면 notification markdown의 `failed`는 아무도 확인하지 않는다. 그래서 **4a-3(폴링 제거)는 흡수 완료 뒤에만 한다**.
- **youtube**: 3단계 수렴 뒤에도 자체 원장(`youtube_notification_send_request`: `base_id`·`generation 0..2`·`member_ids`, golden 1547-1560)과 SENT dedupe ledger(`youtube_notification_delivery_ledger`, shape SENT/QUARANTINED만, golden 1472-1488)를 유지한다(계획:166). 그래서 경로별 어댑터를 둔다.
  - migration(4a-2): `youtube_notification_send_request`에 같은 3열과 같은 shape CHECK·부분 인덱스를 추가한다.
  - 판정 함수 `decideHandoff`는 공유한다. 위치는 `W/egress/handoffcheck`(새 패키지, 순수 함수와 간격 상수만). 사용자가 alarm-dispatch와 youtube 둘이라 공유 근거가 있다. 범용 상태머신 엔진은 아니다.
  - 전이(youtube 어휘): completed는 확인 기록. failed는 `generation<2`이고 `attempt_count < youtube max_retries`이면 delivery `SENT→PENDING`, `send_request.generation+1`, **같은 트랜잭션에서 `source_delivery_id`가 일치하는 ledger SENT 행 삭제**, 아니면 `FAILED` + ledger 삭제. unknown/404/지평은 `QUARANTINED` + `delivery_ledger_record_quarantined.sql` 규칙으로 ledger를 QUARANTINED로 둔다.
  - ledger 삭제 허용과 claim 쪽 ledger adjudication(`transition_ledger_rows.sql`)의 정합은 3단계 youtube 수렴 결과에 달려 있다. 그래서 이 어댑터는 **S3 youtube 수렴 PR 이후** 작성한다(R4).
  - 지평은 `workers.youtube_delivery.executor.attempt_timeout`(`build_egress.go:408`)이다.

---

## 3. iris-client-go·Iris 계약 정합 (작업 항목 4)

| 항목 | 근거 | 설계 반영 |
|---|---|---|
| API | `GetReplyStatus(ctx, requestID) (*ReplyStatusSnapshot, error)`, GET `/reply-status/{requestId}`(ICG `internal/client/transport/client.go:331-338`). `iris.BotClient`/`RebindingClient` 노출(`rebind.go:403`) | 워커는 좁은 인터페이스로 받는다 |
| snapshot | `RequestID`, `State`, `UpdatedAtEpochMs`, `Detail *string`(ICG `internal/client/common/reply_types.go:12-25`) | requestId 불일치 → unknown |
| 상태값 | wire `queued, preparing, prepared, sending, handoff_completed, outcome_unknown, failed`. `ClassifyReplyStatus` → Pending/HandoffCompleted/Failed/Unknown, 빈 값·미지는 Unknown(ICG `iris/reply_status.go:5-30`) | §2.4 표 |
| 404 | 별도 sentinel 없음. `*iris.HTTPError{StatusCode:404}`이고 `Is(ErrPermanent)`가 참이다(ICG `internal/client/transport/errors.go:45-83`) | `errors.AsType[*iris.HTTPError]` 후 404 → 격리 |
| 202 | `ReplyAcceptedResponse{Success, Delivery, RequestID, Room, Type, Duplicate}`(`reply_types.go:3-10`) | text·markdown 모두 requestId 저장 |
| 재발급 | `ReplyReissueMaxGenerations=2`(ICG `iris/reply_reissue.go:15`), `IsPreHandoffClientRequestIDConflict`(63-70) | 세대 상한 |
| Iris 계약 | `/reply` 202는 queue admission만 뜻하고 전달은 `/reply-status`로 본다(`Iris/docs/contracts/current.md:222`). `pre_dispatch && retrySafe`만 requeue, dispatch 시작 뒤 실패는 `outcome_unknown`이며 재전송하지 않는다. `completed`는 수신 확인이 아니다(523-533). durable fallback은 미인식 admission 상태를 `outcome_unknown`으로 보고하고 `failed`로 보고하지 않는다(558) | reconciler의 unknown·404 격리, completed=확인 기록. 계약은 404 의미를 정의하지 않는다. 계획:111(eviction 뒤 durable fallback)을 따라 지평 안의 404는 이상으로 보고 격리한다 |

---

## 4. 봇 plane (4b) 설계 (작업 항목 5)

### 4.1 현재 구조

- 상태 어휘 10종: `pending, submitting, accepted, handoff_completed, retryable_pre_dispatch, outcome_unknown, dead, permanent_conflict, manual_review, discarded`. baseline 9종(`MIG/001_schema_epoch2_baseline.sql:588`)에 180이 `discarded`를 더했다(`MIG/180_reply_outbox_manual_review_discard.sql`). 현재 CHECK는 golden 294-295행.
- 현재 shape: `submitting|accepted`는 claim_token·lease_until·first_attempt_at 필수, `accepted`는 `iris_request_id` 필수, 종단 4종 외에는 payload 필수.
- 흐름:
  1. claim(`B/internal/durability/queries/reply_outbox_claim.sql`): `pending|retryable_pre_dispatch|outcome_unknown` → `submitting`, `attempts+1`.
  2. `DispatchStoredReply`(`B/internal/bot/orchestration/transport/reply_outbox.go:60-94`)가 `postReplyWithReissue`(in-process ladder base→:r1→:r2, `bot_transport.go:208-244`; ladder 정의 `reply_request_id.go`의 `replyReissueLadder`)를 실행한다.
  3. `acceptStoredReply` → `acceptanceHook` → `MarkAccepted`(`B/runtime/durable_reply_dispatch.go:58-77`, SQL `reply_outbox_mark_accepted.sql`: `submitting→accepted`, lease 유지).
  4. `waitForReplyHandoff` 폴링(`reply_handoff.go:49-67`, 250ms `bot_transport.go:41`)을 `dispatchBudget`(= `bot_reply_outbox` executor attempt_timeout, `B/runtime/durable_admission.go:206`)까지 돈다.
  5. 정산(`durable_outbox_policy.go:22-66`): 성공 → `handoff_completed`, `failed` → **`dead`**(27-29), accepted이면서 그 밖 → **`manual_review`**(50-55). admission 결과 불명 → `outcome_unknown`(같은 ID 재시도, 최대 5회, `reply_outbox_repository.go:80`) → 소진 시 `manual_review`.
- "accepted는 재발송하지 않음": `reply_outbox_repository.go:95-111`(정산 소스 제한), `reply_outbox_reclaim.go:23-25,85-99`(만료 `accepted` → `manual_review`, 지표 `bot_reply_outbox_accepted_reclaimed_total` `B/runtime/durable_metrics.go:22`, `durable_maintenance.go:62-77`).
- **방 순서**: 같은 방의 선행 행이 `pending|submitting|accepted|retryable_pre_dispatch|outcome_unknown`이면 후행 행을 claim하지 않는다(`reply_ready_relation.sql:4-12`, 인덱스 `idx_bot_reply_outbox_room_active` golden 304). 즉 오늘은 "앞 응답이 handoff될 때까지 다음 응답 admission을 보류"한다. runbook 계약 문장도 있다(`docs/current/runbooks/hololive-api.md:319` "Outbox는 같은 room의 active 선행 행을 직렬화").

### 4.2 어휘 축소안 판단

- `accepted`: B안 뒤에도 "admission 완료, handoff 미확인" 상태가 필요하다. reconciler 대상, payload 보존(failed 재발송용), 방 순서 보류를 위해서다. 이 상태를 없애려면 `handoff_completed`와 합치고 확인 열을 따로 둬야 한다(대안 R1, 아래). 권고는 **유지하되 의미를 바꾸는 것**이다: lease 없음, reconciler 대상. 상태를 줄이지 않고 **전이를 줄인다**. 사라지는 것은 accepted lease, `accepted→manual_review` reclaim, 발송 중 `accepted→manual_review`, 폴링이다.
- `outcome_unknown`(봇): 이 값은 Iris `outcome_unknown`이 아니라 **admission 응답 유실**(같은 ID 재시도가 안전, `bot_transport.go:254-316`, `durable_outbox_policy.go:57-65`)이다. 계획도 "admission 호출의 transport ambiguity 재시도는 admission 단계에 남는다"(계획:97)고 했다. `retryable_pre_dispatch`와 합치면 소진 시 `dead`/`manual_review` 구분(61-65행 대 43-47행)에 쓸 정보가 사라지므로 열을 하나 더 둬야 한다. **유지를 권고**한다.
- 대안 R1(어휘 10→9): `handoff_completed`를 없애고 `accepted + handoff_checked_at`로 표현한다. 워커와 형태가 같아지지만, 기존 종단 행 이행, `durable_prune_terminal.sql:35`, 지표·runbook·운영 artifact 이름이 바뀐다. 이득보다 변경 폭이 커서 권고하지 않는다.
- 결론(Q7): 봇 plane 상태 어휘는 10종을 유지한다. 계획 "accepted·outcome_unknown 계열을 줄이고"는 전이 축소로 이행한다.

### 4.3 4b 변경 목록

**삭제**
- `B/internal/bot/orchestration/transport/reply_handoff.go` 전체(32-172).
- `bot_transport.go:41`의 `replyStatusPollInterval`, 45-47·51·61의 getter, `replyLane.getter`.
- 185-187·366·420의 `waitForAcceptedReplyHandoff` 호출. 직접 발송 경로는 admission만 남는다. 운영은 항상 outbox writer를 쓴다(`B/runtime/bootstrap_bot_runtime_orchestration.go:74-79,144-151`).
- `reply_outbox.go:58,84-91`의 hook과 폴링. `reply_outcome.go:72,105-122`(`ErrReplyStatusFailed`, `replyStatusFailedError`, `IsReplyStatusFailed`).
- `durable_reply_dispatch.go:58-77`의 `acceptanceHook`.
- `durable_outbox_policy.go:27-29,50-55`의 accepted 분기.
- `reply_outbox_repository.go:284-305`의 `MarkAccepted`와 `reply_outbox_mark_accepted.sql`.
- 정산 정책 소스에서 `replyOutboxStatusAccepted`(105-110).

**변경**
- `DispatchStoredReply`는 `(irisRequestID string, err error)`를 반환한다. requestId 검증은 `acceptStoredReply`(110-127)를 재사용한다.
- 정산: admission 성공 → 새 정책 `ReplyOutboxAccepted`(소스 `submitting`, SQL `reply_outbox_settle_accepted.sql`):
  ```sql
  UPDATE bot_reply_outbox
  SET status = 'accepted', iris_request_id = $3, claim_token = NULL, lease_until = NULL,
      accepted_at = clock_timestamp(),
      handoff_next_check_at = clock_timestamp() + ($4::bigint * INTERVAL '1 millisecond'),
      last_error = '', updated_at = clock_timestamp()
  WHERE id = $1 AND claim_token = $2 AND status = 'submitting'
  ```
  정산 실패(DB 오류)면 행은 `submitting` + lease로 남는다. reclaim → `pending` → 같은 ID 재발송 → Iris가 같은 requestId로 202 duplicate를 준다(168h 안). 오늘은 이 경우가 `manual_review`였다(`acceptanceHook` 실패).
- reconciler(`B/runtime/durable_reply_handoff.go` 새 파일, SQL `B/internal/durability/queries/reply_outbox_handoff_{claim,complete,reschedule,requeue,dead,manual_review}.sql`, 저장소 `B/internal/durability/reply_outbox_handoff.go`): claim·관측·CAS 적용 3단계는 §2.4와 같다. CAS 토큰은 `handoff_next_check_at`, fence는 `status='accepted' AND iris_request_id`다.

| 관측 | 조건 | 전이 |
|---|---|---|
| handoff_completed | — | `handoff_completed`, payload NULL(`reply_outbox_settle_terminal.sql`과 같은 열 처리) |
| failed | `attempts < maxAttempts + operator_replay_grants`, `first_attempt_at`이 `AutomaticReplayHorizon` 안 | `retryable_pre_dispatch`, `available_at = clock_timestamp() + replyOutboxRetryAfterWithBase(...)`. 다음 claim의 in-process ladder가 base부터 다시 시도하고 409 FAILED를 받아 `:r1/:r2`로 간다. 세대 열은 필요 없다 |
| failed | 그 밖 | `dead`(현행 failed 종단과 같음) |
| outcome_unknown·미지·nil·불일치·404 | — | `manual_review`(현행 accepted+unknown과 같음), payload 보존 |
| pending | `elapsed < horizon` | 다음 확인 |
| pending·조회 오류 | `elapsed ≥ horizon` | `manual_review`(`last_error='reply handoff unconfirmed'`) |
| 조회 오류 | `elapsed < horizon` | 전이 없이 다음 확인 |

- 지평 = `bot_reply_outbox` executor attempt_timeout(`durable_admission.go:206`). 새 키는 없다(API 프로필도 strict). 이 프로필과 `claim_lease_ms`의 관계 검증은 그대로다(`hololive/hololive-api/internal/config/api_worker_profile.go:107`).
- 확인 간격(Q8): 방 순서를 지키는 권고안(A)에서는 봇 전용 ladder `250ms→500ms→1s→2s→5s→15s→60s`(지평에서 자름)를 쓴다. 오늘의 250ms 폴링 지연과 맞춰 후행 응답이 기다리는 시간을 늘리지 않기 위해서다. 실행은 정산 직후 `time.AfterFunc` wake와 `outboxPollEvery` 주기 poll을 함께 쓴다(`durable_worker_loop.go:13-42` 패턴). 워커 ladder(5s 시작)를 쓰면 같은 방 후속 응답이 최소 5초 막힌다.
- 방 순서(Q8): 권고 (A)는 `reply_ready_relation.sql`의 선행 집합에 `accepted`를 유지하는 것이다. 현행 계약을 보존한다. (B) `accepted`를 빼면 순서는 Iris 큐 순서에 맡겨진다. Iris 계약 문서에는 방별 reply 순서 보장 문장이 없다(`current.md` 검색 결과). (B)는 Iris 소유자 확인 뒤에만 택한다.
- reclaim: `reply_outbox_reclaim_candidates.sql:4`를 `status = 'submitting' OR (status = 'accepted' AND claim_token IS NOT NULL)`로 바꾼다. 뒤 항은 이전 바이너리가 남긴 leased accepted 처리용이며 현행 동작(→`manual_review`)을 유지한다. 4b-2에서 삭제한다.
- 지표: `bot_reply_outbox_accepted_reclaimed_total`은 4b-2에서 의미가 사라진다(삭제). 대시보드 영향은 R8.

### 4.4 migration (4b-1 / 4b-2)

배포 단위는 API 하나다. 그러나 `compose-redeploy-service.sh hololive-api`는 이전 앱이 떠 있는 동안 `run --rm hololive-db-migrate`를 실행한다(`docs/current/runbooks/hololive-api.md:37`). 엄격한 CHECK를 한 번에 넣으면 이전 바이너리의 `MarkAccepted`(lease 유지 accepted)가 23514로 실패한다. 그러면 `acceptanceHook`에서 `accepted=true`가 이미 세워져 있어 해당 행이 `manual_review`로 간다(`durable_reply_dispatch.go:59-60`, `durable_outbox_policy.go:53-55`). 그래서 두 단계로 나눈다.

- **4b-1 `NNN_bot_reply_outbox_unleased_accepted.sql`**: 열 `accepted_at timestamptz`, `handoff_next_check_at timestamptz`를 추가한다(nullable, backfill 없음). 180과 같은 `_next` 패턴(NOT VALID → VALIDATE → DROP/RENAME)으로 `chk_bot_reply_outbox_state_shape`를 **완화형**으로 바꾼다.
  ```sql
  (status <> 'submitting' OR (claim_token IS NOT NULL AND lease_until IS NOT NULL AND first_attempt_at IS NOT NULL))
  AND (status <> 'accepted' OR (length(iris_request_id) > 0 AND (
        (claim_token IS NOT NULL AND lease_until IS NOT NULL AND first_attempt_at IS NOT NULL)
     OR (claim_token IS NULL AND lease_until IS NULL AND accepted_at IS NOT NULL AND handoff_next_check_at IS NOT NULL))))
  AND (status = 'accepted' OR handoff_next_check_at IS NULL)
  AND (status IN ('handoff_completed','dead','permanent_conflict','discarded') OR payload IS NOT NULL)
  ```
  인덱스 `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bot_reply_outbox_handoff_due ON bot_reply_outbox (handoff_next_check_at, id) WHERE status = 'accepted' AND claim_token IS NULL`를 BEGIN 블록 밖에 둔다. 어휘 CHECK(`chk_bot_reply_outbox_status_vocab`)는 바꾸지 않는다.
- **4b-2 `NNN_bot_reply_outbox_accepted_strict.sql`**(다음 릴리스): 사전 조건은 `status='accepted' AND claim_token IS NOT NULL` 0행이다. VALIDATE가 실패하면 적용이 멈춘다. 엄격형으로 바꾼다: accepted 분기에서 leased 대안 제거, `submitting`만 lease 필수. `idx_bot_reply_outbox_lease_expiry`(golden 301, `WHERE status IN ('submitting','accepted')`)는 `CREATE INDEX CONCURRENTLY … WHERE status = 'submitting'`으로 교체하고 이전 것은 `DROP INDEX CONCURRENTLY`로 지운다. 코드에서는 reclaim의 legacy 항과 지표를 삭제한다.
- `grant_bot_reply_outbox_manual_replay`(최신 정의 `MIG/265_bot_manual_replay_lock_clock.sql`)는 고치지 않는다. `check-stack-retry-contract.sh`가 이 함수의 마지막 정의에서 `target_created_at + interval '144 hours'`를 읽기 때문이다(§7).
- dbtest: 두 migration 시험(완화형에서 이전·새 accepted 모양 모두 허용, 엄격형에서 leased accepted 거부)과 golden 갱신.
- 롤백(R7): 4b-1 바이너리를 이전 바이너리로 되돌리면 lease 없는 `accepted` 행은 이전 reclaim(`lease_until <= now`)에 걸리지 않는다. 그 방의 후속 응답은 `reply_ready_relation`에서 영구히 막힌다. 롤백 runbook에 사전 단계로 "lease 없는 accepted를 `manual_review`로 옮기는 operator SQL"을 추가하거나, 롤백 전 reconciler 배수(backlog 0) 확인을 요구해야 한다.

---

## 5. 시험 목록

**4a-1 (워커 reconciler, 폴링 유지)**
- T-R1 `decideHandoff` 표 시험(`W/egress/alarmdispatch/alarm_dispatch_handoff_test.go`, 또는 youtube 공유 시 `W/egress/handoffcheck`): 전이 6종(completed→확인, failed→재발급, failed 소진→dlq, outcome_unknown·404→격리, pending→재예약, 지평 초과→격리), 조회 오류→무전이 재예약, 조회 오류+지평 초과→격리, requestId 불일치·nil→격리.
- T-R2 재확인 간격: elapsed 0/5s/19s/20s/79s/80s/400s에서 step이 15s/60s/5min이고 지평에서 잘림. 지평 H=12s이면 확인이 5s·12s 두 번.
- T-R3 dbtest(`W/service/alarm/dispatchoutbox/handoff_dbtest_test.go`, `dbtest.NewPool`):
  - (a) MarkSent가 unit handoff 열을 함께 기록하고, 부분 적용이면 0행.
  - (b) claim SKIP LOCKED와 동시 claim 배타. 두 번째 claim은 soft-claim으로 같은 unit을 고르지 않음.
  - (c) CAS 토큰 불일치면 전이 거부(typed 오류).
  - (d) failed 재발급이 `client_request_id=:r1`, `generation=1`, members `retry`, `sent_at NULL`, handoff 열 NULL을 원자적으로 기록하고 다음 `ClaimDue`→`LoadSendRequest`가 `:r1` 요청을 복원.
  - (e) **세대 상한**: generation=2에서 failed → dlq, `request_generation`이 2를 넘지 않음(`chk_alarm_send_request_shape` 위반 없음).
  - (f) dlq 전이가 claim_keys를 반환하고 commit 뒤 해제.
  - (g) 멤버 하나가 `sent`가 아니면 fence 오류이고 전이 없음.
  - (h) 지평 초과 격리가 `quarantined_at`·`last_error_code='handoff_unconfirmed'`를 기록.
- T-W4 `iris_sender_prepared_test.go`: text lane이 `SendMessageAccepted`의 requestId를 반환. 202 `delivery!="queued"`·빈 requestId는 두 lane 모두 `ErrAdmissionOutcomeUnknown`.
- T-M1 migration dbtest: shape CHECK가 거부하는 행 3종, 재실행 멱등.

**4a-2 (youtube 어댑터, S3 youtube 수렴 뒤)**
- failed→`PENDING`+generation+1+ledger SENT 삭제가 원자적이고 다음 claim이 ledger adjudication에 막히지 않음. 소진→`FAILED`+ledger 삭제. unknown→`QUARANTINED`+ledger QUARANTINED.

**4a-3 (폴링 제거)**
- 삭제: `iris_sender_test.go` 188-307의 5개(`…WaitsThroughEveryInFlightState`, `…RetriesStatusObservationWithoutReposting`, `…ClassifiesTerminalStatus`, `…PollingDeadlineIsOutcomeUnknown`, `…PollingDeadlineWithoutQueryErrorHasNoQueryDetail`), `iris_sender_deadline_test.go`(`TestMarkdownHandoffStaysBoundedWhenStatusCannotFinish`), `iris_sender_markdown_status_test.go:14`(`TestMarkdownAdmissionWaitsForExactHandoffOutcome`), `consumer_test.go:42-90`(`TestConsumerQuarantineThresholdSeparatedFromLease`). 3배 기본값·clamp를 고정하는 시험이다.
- 갱신: `iris_sender_markdown_status_test.go:58`(malformed admission → `ErrAdmissionOutcomeUnknown`, 상태 조회 0회 유지), `sendoutcome/outcome_test.go:21` 표(handoff failed 행 삭제, unknown 행 sentinel 이름), `ErrHandoffOutcomeUnknown`을 쓰는 시험들(이름 변경: `alarm_dispatch_runner_test.go:201`, `…_classification_test.go:120`, `…_cancellation_test.go:181`, `…_immutable_request_test.go:144`, `…_provider_attempt_test.go:43`, `notificationdelivery/dispatcher_reliability_test.go:20`, `request_test.go:283-285`, `youtubedispatch/provider_attempt_test.go:48,110`, `send_engine_flow_test.go:100`, `send_engine_unknown_test.go:17,33`, `send_engine_lifecycle_test.go:28`), 가짜 Iris client의 `GetReplyStatus` 제거와 `SendMessageAccepted` 추가(`build_egress_test.go:63`, `alarm_dispatch_see_more_fold_test.go:131`, `alarm_dispatch_runner_cancellation_test.go:80`, `send_engine_see_more_fold_test.go:39`, `iris_sender_test.go:58`).
- 추가: markdown 발송이 202 직후 Success이고 `GetReplyStatus`를 호출하지 않음. `worker_profile` 관계 검증(`quarantine_threshold_ms < lease_ms`이면 기동 오류). `NewConsumer`가 threshold<lease를 오류로 거부.

**4b-1 (봇)**
- 판정 표 시험(봇 전이 7행), 간격 ladder.
- dbtest(`B/internal/durability/reply_outbox_handoff_db_test.go`):
  - admission 정산이 lease를 지우고 `accepted_at`·`handoff_next_check_at`을 기록.
  - CAS 불일치면 거부.
  - failed 재발송 → `retryable_pre_dispatch`, 다음 claim의 ladder가 409 FAILED 뒤 `:r1`을 보냄(가짜 Iris).
  - attempts 소진 → `dead`.
  - unknown·404·지평 → `manual_review` + payload 보존.
  - 같은 방 후행 행이 accepted 선행 동안 claim되지 않음(권고안 A).
  - accepted 정산 실패 뒤 reclaim·같은 ID 재발송이 duplicate 202로 accepted가 됨.
- 갱신·삭제:
  - `bot_transport_characterization_test.go:411-469`의 `TestReplyStatusFailedError`·`TestIsReplyStatusFailed`·`TestReplyStatusDetail`·`TestReplyHandoffStatusResult`는 삭제하거나 reconciler 판정 시험으로 옮긴다.
  - `durable_reply_settlement_test.go:20,80`(accepted 정산 의미 변경).
  - `durable_admission_test.go:127`(`TestAcceptedLeaseReclaimEmitsActionableCounterAndLog`는 4b-2에서 삭제).
  - `reply_outbox_reclaim_policy_db_test.go:13`(legacy accepted만).
  - `reply_outbox_repository_db_test.go:288`(전이 표).
  - `bot_dependency_modules_test.go:117`.
- migration dbtest.

**4b-2**: 엄격 CHECK가 leased accepted를 거부. reclaim이 submitting만 고름.

---

## 6. 3단계 의존과 PR 분할 (작업 항목 7)

| PR | 내용 | 의존 | 배포 단위·순서 |
|---|---|---|---|
| 4a-1 | migration(§2.2), `SendPreparedMessage` requestId 반환(SH+3 호출자), text lane `SendMessageAccepted`, MarkSent 확장(§2.3), reconciler(§2.4-2.5), `CLOCK_QUERY_POLICIES` 추가. **폴링 유지** | 3단계 alarm-dispatch fence 정리(S3A). `repository_transitions_0040_02.sql`, `send_request*.sql`, `fenceSendRequest`를 같이 만진다 | migration → worker. 폴링이 있는 동안 markdown unit은 첫 확인에서 바로 completed이고 text는 이때부터 `failed` 유실이 닫힌다 |
| 4a-2 | youtube 어댑터·migration | 3단계 youtube 수렴 | migration → worker |
| 4a-3 | 폴링 제거, sentinel 정리, 3배 기본값·clamp 제거와 검증, 계약 문서·runbook | 4a-1, 4a-2, **3단계 notification 흡수**(흡수 전이면 notification markdown `failed`를 아무도 확인하지 않음) | worker만. 사전에 ops가 `quarantine_threshold_ms ≥ lease_ms` 확인(R5) |
| 4b-1 | 봇 폴링 제거, accepted 무lease, reconciler, 완화 CHECK, hololive-api runbook | 3단계와 **독립**. Q2·Q8 결정이 선행 | migration → API |
| 4b-2 | 엄격 CHECK, legacy reclaim·지표 삭제 | 4b-1 운영 반영과 leased accepted 0행 | migration → API |

- 독립적으로 먼저 할 수 있는 것: 4b 전체(다른 모듈·배포 단위, `reply_handoff.go`는 워커와 코드 공유 없음). 4a-1 중 `SendPreparedMessage` requestId 반환과 text lane 전환은 3단계와 충돌이 적다. 그러나 MarkSent·재발급 SQL은 S3A가 바꾸므로 같은 PR에 묶어 S3A 뒤에 둔다.
- 각 PR은 CHANGELOG `## 미출시` 한 항목(지정된 경우).

---

## 7. 계약 문서와 stack 검사 (작업 항목 6)

**`docs/current/contracts/alarm.md`**
- 63-68 "Iris Markdown admission" 교체안:
  > Text and Markdown admission both retain the exact Iris `requestId` from the `202` response; a `202` that does not prove queued admission is quarantined. Admission marks the send unit `sent` without waiting for handoff. A separate reconciler reads `/reply-status/{requestId}` from 5 s after admission at 15 s, 60 s and 5 min intervals until the alarm-dispatch attempt timeout: `handoff_completed` records the check, `failed` returns the unit to `retry` with the next request generation (r1/r2) while generation and attempt budgets remain and otherwise moves it to `dlq`, and `outcome_unknown`, an unknown state, a missing status (`404`) or an unconfirmed handoff at the horizon quarantine the unit. Status lookup errors defer the check without a transition.
- 76-79 "structured `OUTCOME_UNKNOWN` and unknown handoff remain quarantined. Only confirmed pre-handoff `CLIENT_REQUEST_ID_FAILED` permits…" → "structured `OUTCOME_UNKNOWN` remains quarantined. A confirmed pre-handoff `CLIENT_REQUEST_ID_FAILED` at admission, or a reconciled `failed` status, permits atomic generation/retry advance to SDK r1/r2 within the existing retry budget."
- "Timeout and retry policy"(346-353): `last_error_code` 목록에 `handoff_failed`, `handoff_outcome_unknown`, `handoff_status_missing`, `handoff_unconfirmed`를 추가하고, "Handoff reconciliation" 항목 한 줄을 추가한다.

**runbook**
- `docs/current/runbooks/alarm-worker.md:57`: handoff 확인이 sender가 아니라 reconciler에 있음.
- 같은 문서 §4(185-200): 증상 "202 후 delivery가 sent로 전이되지 않음"을 "sent 뒤 unit의 `handoff_next_check_at`가 남아 있음 / `handoff_*` 코드로 quarantine"으로 바꾸고, "DB row만 수동 sent 처리 금지" 문장은 유지한다.

**봇 plane 계약 위치**: 별도 contract 문서가 없고 `docs/current/runbooks/hololive-api.md`가 계약을 담는다.
- 283: 상태 설명에 `accepted` = admission 완료·handoff 확인 대기 추가.
- 285: "accepted lease가 만료된 행은 … manual_review" 삭제(4b-2). `bot_reply_outbox_accepted_reclaimed_total` 언급 삭제. reconciler 결과(failed 재발송·unknown/지평 manual_review) 추가.
- 317: discard 전 `/reply-status` 조회 절차 유지.
- 319: 방 직렬화 문장 유지(권고안 A) 또는 수정(B).

**stack 검사(`/home/kapu/work/iris-stack/tools/checks`)**

| 스크립트 | 검사 내용 | 이 변경의 영향 |
|---|---|---|
| `check-stack-retry-contract.sh` | Iris webhook retry 상수와 iris-client-go sender horizon·dedup TTL 일치(132-191), shared-go `irisdurable` 보존 168h·margin·자동 replay 지평(65-99,193-201), hololive manual replay 지평 = shared 자동 지평. 마지막은 manifest 순서상 `grant_bot_reply_outbox_manual_replay` 마지막 정의의 `target_created_at + interval 'N hours'`(101-128,155-157) | 값 변경 없음. 4b migration은 이 함수를 재정의하지 않는다(재정의하면 144h 문자열 유지 필요) |
| `check-stack-reissue-contract.sh` | Iris `PreviousFailed` wire code와 SDK `HTTPErrorCodeClientRequestIDFailed` 일치(8-20) | 변경 없음 |
| `check-stack-worker-contract.sh` | `docs/contracts/stack-worker-profile-v1.schema.json`·diagnostics schema와 fixture 검증, 정수 토큰, mirror fixture byte 동일(101-152). settings는 `{"type":"object"}`로 열려 있음(schema 110) | 새 키를 만들지 않으므로 변경 없음. hololive strict decode는 이 검사 밖이다(지평 키를 추가하면 stack-secrets 프로필 동시 변경, Q3) |

---

## 8. 결정 요약

| 갈림길 | 선택지 | 권고 | 근거 |
|---|---|---|---|
| handoff 열 위치 | delivery / send unit | send unit | 요청 1개 = unit 1개(golden 162-182), 조회 중복·멤버 불일치 회피 |
| 확인 횟수 표현 | count 열 / 경과 시간 | 경과 시간(DB 계산) | 열·CAS 감소, 시간원 단일 |
| text requestId | clientRequestId로 조회 / 202 requestId 저장 | 저장 | 동치 근거 없음(C5), 봇과 동일 |
| sendoutcome | 3종 / 4종 유지 + sentinel 정리 | 4종 | C2 |
| failed 처리 | reconciler 직접 세대+1 / retry 후 409로 세대+1 | 직접(계획) | 계획 결정. Q5로 근거 확인 |
| 3배 규칙 | 기본값 삭제 + 검증 / 운영 값 축소 | 코드 삭제와 검증, 값은 ops | C3 |
| 봇 어휘 | 10 유지·전이 축소 / 9로 축소(R1) | 유지 | §4.2 |
| 봇 방 순서 | A 유지 / B Iris 순서 위임 | A | 현행 계약(runbook:319), Iris 순서 보장 문서 없음 |
| 봇 migration | 1단계 / 2단계 | 2단계 | migrate가 이전 앱 가동 중 실행(runbook:37) |
| 지평 설정 | attempt_timeout 파생 / 새 키 | 파생 | strict decode, 계획 8절 |
