# S2 설계: `youtube_live_runtime_state` 명시 갱신과 전역 guard 대체 (계획 2.3·4.3·6절 2단계·7절)

- 기준 코드: `/home/kapu/work/iris-stack/hololive-bot-apsm-1` = origin/main `cb105cc97`. 아래 `file:line`은 별도 표기가 없으면 `cb105cc97` 기준이다. 경로 접두 `api/` = `hololive/hololive-api/`, `so/` = `api/internal/youtube/sourceobservation/`, `mig/` = `api/scripts/migrations/`, `dbt/` = `hololive/hololive-dbtest/`, `rt/` = `api/internal/planes/youtube/runtime/`.
- 조사 시점(2026-10-19)에 1단계 작업자(U1)가 같은 worktree에서 `so/live_persist.go`·`schedule_persist.go`·`hololive-shared/pkg/dbx/tx.go` 등을 편집 중이었다(미커밋). 8절에서 충돌 지점으로 다룬다.
- 저장소 파일은 수정하지 않았다. DB·시험은 실행하지 않았다. 실행으로 확인하지 않은 동작 주장은 `[INFERENCE]`로 표기한다.

---

## 0. 결론 요약

| 항목 | 권고 |
|---|---|
| 갱신 방식 | Go가 트랜잭션 끝에서 변경 key 집합으로 **명시 호출**한다. 계산 SQL은 기존 `refresh_youtube_live_runtime_state(text[])`를 단일 소유자로 남기고(guard 호출 제거, per-video 잠금·RC 검사 추가) Go·운영 함수·maintenance·data migration이 같은 함수를 부른다(F1). 트리거 5종×6표 + `source_observations` guard 트리거, 트리거 함수 2개, guard 표는 DROP. |
| 전역 guard 대체 | 전역 advisory key 하나를 **shared/exclusive**로 나눈다. 관측 writer(finalizeTx·finalizer)는 shared, 증거 retention·수동 영수증·head 복구·data migration은 exclusive. writer끼리는 per-video advisory(갱신 함수 안, video_id 정렬)로만 직렬화된다. |
| 40001 정책 | 새 트랜잭션 내 재시도를 추가하지 않는다. writer는 READ COMMITTED로만 갱신 함수를 호출할 수 있다(RR/SER는 함수가 명시 거부). 잠금 경합에서 생기는 40001/40P01/55P03은 기존 관측 재시도(`rt/supervisor_retry.go:178-189`, `rt/supervisor.go:351`)와 finalizer 다음 tick(`rt/supervisor.go:96`)이 처리한다. |
| 전환 측정 | 2a는 "Go 실제 쓰기"가 아니라 **동일 트랜잭션 key shadow 비교**다. 트리거가 refresh한 key를 트랜잭션 GUC에 기록(migration 325)하고, Go가 모은 key 집합에서 빠진 key 수를 지표로 센다. 계산식은 F1이라 동일하므로 측정 대상은 key 완전성 하나다. |
| 브랜치 | 2a: key 수집 plumbing + shadow 비교 + 지표 + migration 325, 트리거·guard 유지. 2b: migration 326(트리거·guard 제거, 함수 교체, 권한) + Go 명시 호출·잠금 교체 + 시험 재작성 + 문서. 2b는 2a 지표 0과 `check-runtime-projections.sh` 0 확인 전 merge 금지. |
| 사용자 결정 | (Q1) 수동 영수증 함수의 SERIALIZABLE 요구를 READ COMMITTED로 바꾸는 운영 계약 변경. (Q2) F1(함수 유지) 대신 계획 문구대로 Go SQL 직접 upsert(F2)를 원하면 `hololive_runtime`에 표 쓰기 권한 확대가 필요. |

---

## 1. runtime_state 관련 DB 객체 (원문 인용)

### 1.1 312 `mig/312_youtube_live_runtime_state.sql`

**guard 표·잠금 함수** (`:5-25`)
```sql
CREATE TABLE IF NOT EXISTS public.youtube_live_runtime_state_guard (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    writer xid8 NOT NULL
);
COMMENT ON TABLE public.youtube_live_runtime_state_guard IS
    '전체 수명 쓰기의 MVCC 직렬화 행. 오래된 RR/SERIALIZABLE snapshot은 40001로 실패하며 재시도하지 않는다.';
...
CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state()
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $lock$
BEGIN
    -- 같은 트랜잭션은 잠금을 유지하되 행 버전을 다시 쓰지 않는다. TRUNCATE 뒤 빈 표도 복구한다.
    INSERT INTO public.youtube_live_runtime_state_guard AS guard (singleton,writer)
    VALUES (true,pg_catalog.pg_current_xact_id())
    ON CONFLICT (singleton) DO UPDATE SET writer=EXCLUDED.writer
    WHERE guard.writer IS DISTINCT FROM EXCLUDED.writer;
END
$lock$;
```
- 직렬화 대상: singleton 행 하나에 대한 MVCC 갱신이다. 트랜잭션마다 첫 호출이 행을 새 xid로 갱신하므로 **모든 guard 호출 트랜잭션이 전역으로 직렬화**된다. RR/SER 트랜잭션은 대기 후 갱신 시 "concurrent update" 40001로 실패한다(오래된 snapshot 거절).
- 같은 트랜잭션의 재진입은 `WHERE guard.writer IS DISTINCT FROM` 때문에 행을 다시 쓰지 않는다(`dbt/live_runtime_state_migration_test.go:187-199`가 ctid로 고정).

**모델 표** (`:34-57`): `video_id` PK, `channel_id`, `session_status`, `head_status`, `scheduled_start_time`, `lifecycle_origin`, head positive 4시각, `availability_at`, `availability_seen_at`, `unresolvable_since`, `reviewed_at`. CHECK: `(channel_id IS NULL) = (session_status IS NULL)`, session 또는 head가 LIVE/UPCOMING. COMMENT: "원본·claim 잠금/조회 전에 lock_youtube_live_runtime_state()를 호출한다. 직접 DML은 statement trigger가 보호한다."

**계산 함수** (`:59-117`) — key 한정 집합 SQL 두 문장:
```sql
CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh$
BEGIN
    PERFORM public.lock_youtube_live_runtime_state();
    -- VOLATILE의 문장별 snapshot으로 guard 대기 전에 시작한 DML에서도 앞선 커밋을 읽는다.
    DELETE FROM public.youtube_live_runtime_state state
    WHERE state.video_id=ANY(p_video_ids)
      AND NOT EXISTS (... session.status IN ('LIVE','UPCOMING'))
      AND NOT EXISTS (... head.status IN ('LIVE','UPCOMING'));

    INSERT INTO public.youtube_live_runtime_state AS state (...)
    SELECT ids.video_id,session.channel_id,...,review.reviewed_at
    FROM (SELECT DISTINCT unnest(p_video_ids) AS video_id) ids
    LEFT JOIN public.youtube_live_sessions session USING (video_id)
    LEFT JOIN public.youtube_live_reconciliation_heads head USING (video_id)
    LEFT JOIN public.youtube_live_pending_ends pending USING (video_id)
    LEFT JOIN public.youtube_video_availability availability USING (video_id)
    LEFT JOIN LATERAL (SELECT max(receipt.recorded_at) AS reviewed_at
        FROM public.youtube_live_review_receipt_facts receipt
        WHERE receipt.video_id=ids.video_id AND session.status='UPCOMING' ... 
          AND receipt.semantic_value=public.youtube_live_review_source_value(...)) review ON true
    WHERE session.status IN ('LIVE','UPCOMING') OR head.status IN ('LIVE','UPCOMING')
    ORDER BY ids.video_id COLLATE "C"
    ON CONFLICT (video_id) DO UPDATE SET ...
    WHERE state IS DISTINCT FROM EXCLUDED;
END
$refresh$;
```
- 입력 표: sessions, heads, pending_ends, availability, receipt_facts(receipts는 facts를 통해 간접). 값이 같으면 행을 다시 쓰지 않는다.

**guard 트리거 함수** (`:120-129`) `guard_youtube_live_runtime_state()` = `PERFORM lock_youtube_live_runtime_state(); RETURN NULL;`

**문장 트리거 함수** (`:131-161`, 316이 교체) `refresh_youtube_live_runtime_state_statement()`: transition table에서 key를 모아 `refresh(...)` 호출. TRUNCATE는 transition table이 없으므로 "runtime_state ∪ sessions ∪ heads 전체 key".

**트리거 설치** (`:163-180`)
```sql
FOREACH source_table IN ARRAY ARRAY['youtube_live_sessions','youtube_live_reconciliation_heads',
    'youtube_live_pending_ends','youtube_video_availability','youtube_live_review_receipts','youtube_live_review_receipt_facts']
LOOP
    ... youtube_live_runtime_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ... guard_youtube_live_runtime_state()
    ... youtube_live_runtime_insert AFTER INSERT REFERENCING NEW TABLE AS runtime_new ... refresh_youtube_live_runtime_state_statement()
    ... youtube_live_runtime_update AFTER UPDATE REFERENCING OLD TABLE AS runtime_old NEW TABLE AS runtime_new ...
    ... youtube_live_runtime_delete AFTER DELETE REFERENCING OLD TABLE AS runtime_old ...
    ... youtube_live_runtime_truncate AFTER TRUNCATE ...
END LOOP;
CREATE OR REPLACE TRIGGER youtube_live_runtime_guard
    BEFORE DELETE OR TRUNCATE ON public.source_observations
    FOR EACH STATEMENT EXECUTE FUNCTION public.guard_youtube_live_runtime_state();
```
golden 확인: `dbt/testdata/schema_snapshot.golden.sql:797,1266-1270,1305-1309,1321-1325,1346-1349,1607-…`.

**수동 영수증 함수 재정의** (`:183-218`) `record_youtube_live_review(...)`: `current_setting('transaction_isolation') <> 'serializable'`이면 거부 → 입력 검증 → `PERFORM lock_youtube_live_runtime_state()` → session/head/pending/availability `FOR UPDATE` → `youtube_live_review_snapshot` CAS → receipts INSERT(row trigger `store_youtube_live_review_receipt_facts`가 facts INSERT, `mig/279...:50-54`).

**권한** (`:221-247`): `hololive_runtime`은 모델 SELECT와 `lock_...()` EXECUTE만, refresh/guard/statement 함수 EXECUTE 회수. `hololive_scraper` 전부 회수. `record_youtube_live_review` EXECUTE 보유 역할에 `lock_...()` EXECUTE 부여.

**backfill** (`:249-252`) runtime_state ∪ sessions ∪ heads 전체 key로 refresh.

### 1.2 313 `mig/313_youtube_collection_runtime_bundles.sql`
live guard와 별개 객체(`youtube_collection_projection_guard.runtime_writer`, `:5-22`). runtime_state와 무관하며 이번 범위 밖이다. 다만 313의 CHANGELOG(`CHANGELOG.md:81`) "bounded retention도 guard부터 획득"은 315의 live guard 호출을 가리킨다.

### 1.3 315 `mig/315_source_retention_bounded_scan.sql:51-52`
```sql
    -- 직접 SQL 호출도 live guard → 커서 → 원본 순서를 지킨다. 다음 문장이 대기 후 새 snapshot을 읽는다.
    PERFORM public.lock_youtube_live_runtime_state();
```
retention 함수 `delete_source_observation_retention_batch`가 live guard → `source_observation_retention_state` FOR UPDATE → 원본 FOR UPDATE SKIP LOCKED → DELETE(FK CASCADE/SET NULL) 순서. Go 호출처: `so/repository_retention.go:318-340`(`deleteEvidenceBatch`) → `so/queries/repository_retention_delete_evidence_0079_79.sql`.

### 1.4 316 `mig/316_youtube_live_runtime_update_filter.sql`
`:5` guard 선획득, `:7-66` 문장 트리거 함수 교체. UPDATE일 때 표별로 "소비·검토 입력" 열이 달라진 key만 refresh:
- sessions: `channel_id,status,lifecycle_origin,title,topic_id,is_premiere,scheduled_start_time,started_at,ended_at,live_first_seen_at`
- heads: `status`, positive 4시각, `last_end_evidence_at,last_complete_absence_at,last_absence_scheduled_for,first_/second_absence_scheduled_for,consecutive_absence_slots,last_absence_observation_id,end_candidate_kind,end_candidate_observation_id,ended_at,end_reason,unresolvable_since`
- pending: `channel_id,kind,observation_id,effective_at,scheduled_for,ended_at,negative_eligible,scope_covers`
- availability: `availability,effective_at,observed_at`
- receipts/facts: 모든 key. identity 변경(video_id UPDATE)은 old/new 양쪽 key.
BEFORE guard는 그대로라 refresh를 생략하는 기술적 UPDATE도 전역 직렬화된다.

### 1.5 이후 migration
317~324 중 live guard·트리거를 바꾸는 것은 없다. 324(`mig/324_...:10-`)는 sessions UPDATE로 트리거에 의존해 모델을 갱신한다(2b 뒤 같은 형태의 data migration은 명시 refresh 필요 → 6.4).

### 1.6 어떤 DML이 무엇을 발화하는가

| 표 | BEFORE guard | AFTER refresh | 비고 |
|---|---|---|---|
| youtube_live_sessions | INSERT/UPDATE/DELETE/TRUNCATE | INSERT·DELETE 전 key, UPDATE는 316 열 변경 key, TRUNCATE 전체 | DELETE는 `youtube_live_time_finalizations` CASCADE(비원본) |
| youtube_live_reconciliation_heads | 동일 | 동일(316 head 열) | FK `(video_id,end_candidate_observation_id)`→pending DEFERRABLE |
| youtube_live_pending_ends | 동일 | 동일(316 pending 열) | |
| youtube_video_availability | 동일 | 동일(316 availability 열) | `observation_id` FK SET NULL ← source_observations |
| youtube_live_review_receipts | 동일 | 전 key | row trigger가 facts INSERT, 불변 트리거 |
| youtube_live_review_receipt_facts | 동일 | 전 key | 불변 트리거 |
| source_observations | DELETE/TRUNCATE만 | 없음 | 삭제의 FK SET NULL이 availability UPDATE를 일으키며 그 UPDATE가 availability 트리거를 발화. `observation_id`는 316 입력이 아니므로 refresh 0, guard만 |

statement 트리거라 0행 문장(no-op upsert 포함)도 BEFORE guard는 발화한다. 따라서 위 6표 중 하나라도 건드리는 모든 문장, 그리고 `finalizeTx`의 명시 호출로 **모든 관측 확정**(라이브와 무관한 community/photo/profile 포함)이 전역 직렬화된다(`so/repository_finalize.go:90`).

### 1.7 guard 직접 호출처 (계획 4.3의 "두 곳"은 불완전)

| 호출처 | 위치 | 비고 |
|---|---|---|
| 모든 관측 확정 | `so/repository_finalize.go:90` → `so/repository_helpers.go:30-37` → `so/queries/repository_live_runtime_lock.sql:1` | claim보다 먼저 |
| 종료 finalizer | `so/live_finalizer.go:31` | due 조회보다 먼저 |
| 증거 retention | `mig/315...:52` (Go: `so/repository_retention.go:326`) | 커서·원본보다 먼저 |
| 수동 영수증 | `mig/312...:201` (`record_youtube_live_review`) | 운영자 psql, SERIALIZABLE |
| head 복구 | `scripts/maintenance/reconcile-terminal-live-heads.sql:19` | 운영 승인 스크립트 |
| (적용 완료) | `mig/312:27`, `mig/316:5` | 이력, 수정 금지 |

트리거를 없애도 SQL 쪽 세 호출처(315·312 record·maintenance)가 남는다. 2b는 이들을 같은 의미로 옮겨야 한다.

---

## 2. writer 경로와 변경 key, 소비자

### 2.1 Go writer (모두 `so/`, 모두 `dbx.InPgxTx*` = 서버 기본 격리 수준, `hololive-shared/pkg/dbx/tx.go:47-64`)

| 경로 | 진입 | 원본 쓰기 | 변경 key(K) |
|---|---|---|---|
| 관측 소비: live_snapshot | `community_consumer.go:143-146` → `live_consumer.go:13-46` | `persistLiveDecision`(`live_persist.go:24-38`): session upsert(`0047`), head upsert(`0048`), pending upsert·delete(`live_evidence.go:210-228`) | `decision.Sessions[*].VideoID ∪ decision.PendingEnds[*].VideoID` (pending delete는 sessionIDs로 한정, `queries/repository_live_pending_ends_delete.sql`) |
| 관측 소비: schedule | `community_consumer.go:148-150` → `schedule_consumer.go:13` → `persistScheduleDecision`(`schedule_persist.go:124-`) | session upsert(`liveSessionStatement`) | `decision.Sessions[*].VideoID` |
| 최초공개(content video_list) | `content_consumer.go:41` → `content_premiere.go:13-43` | `upsertConfirmedPremiereSession`(`live_persist.go:76-91`, `repository_live_premiere_classification_upsert.sql`) | `decision.Sessions[*].VideoID` |
| live check: video | `live_check_consumer.go:82-125` | availability upsert(`:107`, applied일 때만), 활성 세션이면 `persistLiveDecision`(`:160`) | applied면 `payload.VideoID` ∪ decision K |
| 시각 확정(ENDED) | `live_check_consumer.go:121` → `live_time_finalization.go:110-160` → `persistTimeFinalization`(`:364-`, `repository_live_time_finalization_write.sql`) | sessions·heads UPDATE(확정 시) | `sessionUpdated`(SQL 2번째 열)면 `write.VideoID` |
| live check: channel | `live_check_consumer.go:52-79` | `youtube_channel_live_checks`만 | 없음 |
| 종료 finalizer | `live_finalizer.go:15-76` | `persistLiveDecision`(`:71`) | decision K (사실상 `videoID`) |
| community/photo/profile/content(비 premiere) | 각 consumer | 라이브 원본 없음 | 없음 |
| 증거 retention | `repository_retention.go:318-340` | source_observations DELETE → availability `observation_id` SET NULL | 없음. `observation_id`는 계산 입력이 아님(`dbt/live_runtime_state_migration_test.go:303-312` "증거 FK 제거는 가용성 의미 사실을 바꾸지 않습니다"가 고정) |
| replay/queue/payload/absence retention | `repository_replay*.go`, `repository_retention.go` 기타 | 라이브 원본 없음 | 없음 |

### 2.2 SQL writer (Go 밖)

| 경로 | 원본 쓰기 | K |
|---|---|---|
| `record_youtube_live_review` (`mig/312:183-218`) | receipts INSERT → facts INSERT | `{p_video_id}` |
| `scripts/maintenance/reconcile-terminal-live-heads.sql:109-111` | heads UPDATE(LIVE→ENDED) | `terminal_head_repair.video_id` |
| data migration(예: 324) | 임의 | 변경 key(작성자 책임) |
| 수집기 `hololive-youtube-collector/.../repository_publish_set_0032_32.sql:196` | source_observations INSERT | 트리거 없음(INSERT는 guard 대상 아님) → 무관 |

라이브 원본을 쓰는 다른 모듈은 없다: 비시험 Go/SQL 전수(`git grep` DML 패턴)에서 위 목록 외 일치 없음. 워커·봇·수집기의 참조는 읽기와 시험 fixture뿐이다.

### 2.3 소비자
- `rt/queries/live_check_videos.sql:13` ← `rt/roster.go:55`
- `rt/queries/collection_target_observability.sql:40` ← `rt/collection_metrics.go:85`
- 오라클: `scripts/maintenance/queries/live_runtime_state_drift.sql:31`, `check-runtime-projections.sh`
- 그 외 모듈의 runtime_state 참조 없음(전 저장소 grep). 계획 2.3의 "소비자는 둘뿐" 확인.

---

## 3. 명시 갱신 설계

### 3.1 계산 SQL의 소유자: F1(권고) vs F2

| | F1: 기존 함수 유지, Go가 명시 호출 | F2: Go 쿼리 파일로 이동, 함수 DROP |
|---|---|---|
| 권한 | `hololive_runtime`에 `refresh(text[])` EXECUTE만 추가. SECURITY DEFINER가 "원본에서 계산한 값만 쓸 수 있음"을 강제. 312의 "runtime은 모델 SELECT만" 계약(`mig/312:226-229`, 시험 `dbt/live_runtime_state_migration_test.go:249-276`의 permission denied) 유지 | `hololive_runtime`에 runtime_state INSERT/UPDATE/DELETE 부여 필요 → 임의 값 쓰기 가능. 보안 계약 변경 |
| 중복 | 하나. 수동 영수증·head 복구·data migration도 같은 함수 호출 | Go 파일 + record 함수 본문 + maintenance 스크립트에 같은 SQL 3벌(SQL 쪽 경로는 Go를 부를 수 없음) |
| 2a 측정 | 계산식 동일 → key 완전성만 측정하면 됨 | 계산식 동등성도 측정 필요 |
| 계획 문구 | "persistLiveDecision·finalize 트랜잭션에서 변경 key를 모아 runtime_state를 직접 upsert"의 **언제·어떤 key**는 Go가 결정, 계산은 DB 함수 | 문구 그대로 |

권고: **F1.** runtime_state는 업무 상태 전이가 아니라 원본의 파생 집계이며(계획 1절 "집합 조회·집계 SQL은 유지"), 트리거(암묵 발화)·guard 행이 제거 대상이지 계산식이 아니다. F2를 원하면 Q2로 승인받는다.

F2를 택할 때의 SQL(참고): `so/queries/repository_live_runtime_refresh.sql` 한 문장
```sql
WITH ids AS MATERIALIZED (SELECT DISTINCT video_id FROM unnest($1::text[]) AS input(video_id)),
removed AS (
    DELETE FROM youtube_live_runtime_state state USING ids
    WHERE state.video_id = ids.video_id
      AND NOT EXISTS (...LIVE/UPCOMING session...) AND NOT EXISTS (...LIVE/UPCOMING head...)
)
INSERT INTO youtube_live_runtime_state AS state (...)
SELECT ... (312:77-106의 SELECT 그대로, ids 대체) ...
ON CONFLICT (video_id) DO UPDATE SET ... WHERE state IS DISTINCT FROM EXCLUDED
```
삭제 대상(활성 원본 없음)과 삽입 대상(활성 원본 있음)은 서로소라 같은 문장의 CTE로 합칠 수 있다 [INFERENCE: 같은 문장 내 DELETE가 지운 행을 ON CONFLICT가 보지 못하는 PG 의미론에 의존하나, 집합이 서로소라 무관].

### 3.2 2b의 함수 본문 (migration 326)
```sql
CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[])
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $refresh$
BEGIN
    -- 잠금 대기 뒤 문장이 새 snapshot으로 앞선 커밋을 읽어야 하므로 고정 snapshot 호출은 거부한다.
    IF pg_catalog.current_setting('transaction_isolation') <> 'read committed' THEN
        RAISE EXCEPTION 'live runtime refresh requires read committed transaction';
    END IF;
    IF p_video_ids IS NULL OR pg_catalog.cardinality(p_video_ids) = 0 THEN
        RETURN;
    END IF;
    -- 같은 영상의 갱신을 커밋까지 직렬화한다. 정렬 순서로 잡아 writer 간 교착을 만들지 않는다.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(
                pg_catalog.concat_ws(E'\x1f', 'youtube_live_runtime_video', ordered.video_id), 0))
    FROM (SELECT DISTINCT video_id FROM pg_catalog.unnest(p_video_ids) AS input(video_id)
          ORDER BY video_id COLLATE "C") AS ordered;
    -- 이하 312:66-115의 DELETE·INSERT … ON CONFLICT 두 문장을 그대로 둔다(guard PERFORM만 제거).
    ...
END
$refresh$;
```
- 다중 key 정렬 잠금은 기존 패턴 `api/internal/planes/bot/internal/durability/queries/inbox_ordering_keys_lock.sql` 재사용.
- key 문자열은 `so/queries/repository_live_subject_lock_0044_44.sql`(`concat_ws(E'\\x1f','live_subject_head',$1)`)과 같은 방식으로 namespace를 붙인다.
- RC 검사는 기존 record 함수의 격리 수준 검사 패턴(`mig/312:192-194`)과 같은 형태(P0001). 잘못된 격리 수준은 재시도로 해결되지 않는 오류이므로 40001로 올리지 않는다(비재시도 → 관측 dead-letter/루프 오류로 드러남).

### 3.3 Go 쪽 변경 (2b)

1. `so/queries/repository_live_runtime_refresh.sql` 신설: `SELECT public.refresh_youtube_live_runtime_state($1::text[])`.
2. `so/repository_helpers.go`: `refreshLiveRuntimeState(ctx, tx, videoIDs []string) error` — 빈 집합이면 왕복 없이 반환. 오류는 `fmt.Errorf("refresh youtube live runtime state: %w", err)`.
3. `so/types.go:170-175` `ReconcileResult`에 `LiveRuntimeVideoIDs []string` 추가. 각 writer가 2.1 표의 K를 채운다.
   - `persistLiveDecision`이 K를 반환(signature `(... ) ([]string, error)`)하도록 하고 호출자(`live_consumer.go:41`, `live_check_consumer.go:160`, `live_finalizer.go:71`)가 결과에 싣는다. K = `decision.Sessions` ∪ `decision.PendingEnds` id(상위집합; 1단계가 "읽은 행과 같으면 쓰지 않음"으로 바꿔도 head·pending 쓰기가 있을 수 있어 Sessions 전체를 유지).
   - `persistVideoAvailability` applied → `payload.VideoID`; `persistTimeFinalization` sessionUpdated → `write.VideoID`; `persistPremiereDecision`·`persistScheduleDecision` → decision.Sessions id.
   - `content_consumer.go`의 premiere 결과 병합(`mergeContentApplications`, `:55-69`)과 같은 위치에서 key도 합친다.
4. `so/repository_finalize.go` `finalizeTx`(`:84-127`): `completeFinalizeObservation`(`:119`) **뒤**, 반환 전에 `refreshLiveRuntimeState(ctx, tx, result.LiveRuntimeVideoIDs)`. 갱신을 트랜잭션의 마지막 공유 쓰기로 둔다(이후에는 자기 claim의 queue·offset·applications만 있고 그것도 갱신 앞에 끝남 → per-video 잠금 보유 중 다른 트랜잭션 잠금을 기다리지 않음).
5. `so/live_finalizer.go:71` 뒤에 같은 호출.
6. 외부 호출(HTTP 등)은 이 트랜잭션들에 없다(`finalizeTx` 콜백은 DB 작업뿐). 추가하지 않는다.

비용: 트리거는 원본 문장마다 refresh(+guard)를 호출했다(2026-10-15 측정 `docs/current/plans/2026-10-15-sql-db-review-handoff.md:572-576`: refresh 5.6회/초·평균 2.585ms, pending upsert 2.698ms에 refresh 포함). 2b는 트랜잭션당 1회. 단 K는 316 필터 없는 상위집합이다. live_snapshot에서 dirty 세션은 대부분 head positive 시각도 바뀌어(316 입력) 트리거도 refresh하던 key라 차이가 작다고 본다 [INFERENCE]. 2a의 extra-key 지표(5.2)로 확인하고, 크면 PG18 `RETURNING old.*/new.*`로 316 열 비교를 쓰기 SQL 옆에 두는 정밀화를 검토한다(2b 범위 밖, 측정 근거가 생길 때만).

---

## 4. guard 대체: 잠금 설계

### 4.1 키
- 전역: `hashtextextended('youtube_live_runtime_global', 0)` (int8 단일키).
- 영상: `hashtextextended(concat_ws(E'\x1f','youtube_live_runtime_video', video_id), 0)`.
- 충돌은 불필요한 대기만 만든다. 전역 키와 영상 키가 같아지는 경우(2^-64)만 교착 가능 → 40P01 → 기존 재시도.

### 4.2 함수 (migration 326)
- `lock_youtube_live_runtime_state()` **이름 유지, 본문 교체**: `PERFORM pg_catalog.pg_advisory_xact_lock(<전역>)` (exclusive). 315 retention 함수·maintenance 스크립트가 이미 이 이름을 부르므로 315 본문을 다시 만들지 않고 의미("모든 라이브 writer 배제")를 유지한다. 행 쓰기가 없으므로 RR/SER 거절은 사라진다(아래 4.5).
- `lock_youtube_live_runtime_state_shared()` 신설: `pg_advisory_xact_lock_shared(<전역>)`. `hololive_runtime` EXECUTE.
- 대안: 이름을 `..._exclusive`로 바꾸면 315 함수 전체를 새 migration에 다시 써야 하고 maintenance·record 함수도 바뀐다. 이득 없이 변경면만 커서 비권고.

### 4.3 잠금 순서 (2b)

| 경로 | 1 전역 | 2 | 3 | 4 행 잠금 | 5 영상 advisory | 6 |
|---|---|---|---|---|---|---|
| 관측 확정(전 kind) `finalizeTx` | shared (`repository_finalize.go:90` 자리) | claim queue FOR UPDATE (`repository_claim_lock_0013_13.sql`) | subject advisory(`live_subject_head:`…, live/premiere/schedule/channel) | session→head→pending FOR UPDATE(video_id 순), availability upsert, 쓰기 | refresh 안, 정렬 | applications 등은 refresh 전, commit |
| 종료 finalizer | shared (`live_finalizer.go:31` 자리) | due 조회(무잠금) | – | session→head→pending | refresh 안 | commit |
| 증거 retention | exclusive (315:52, 변경 없음) | retention_state FOR UPDATE | – | 원본 FOR UPDATE SKIP LOCKED → DELETE(FK) | 없음(K 없음) | commit |
| 수동 영수증 | exclusive | – | – | session→head→pending→availability FOR UPDATE | refresh([v]) | commit |
| head 복구 | exclusive (스크립트 :19) | – | – | sessions→heads FOR UPDATE | refresh(ids) | commit |
| data migration | exclusive(권장 규약) | – | – | 임의 | refresh(K) | commit |

### 4.4 교착 분석
- **shared↔exclusive**: shared를 claim·원본 잠금보다 먼저 잡고, exclusive 보유자는 그 뒤 원본 잠금을 잡는다. 기존 guard와 같은 "전역 먼저" 순서라 retention의 FK SET NULL(availability 행 잠금 대기)과 writer의 availability 행 잠금이 순환하지 않는다(`so/live_runtime_guard_test.go:136-141` 주석이 지키던 불변식). PG 잠금 큐에서 대기 중 exclusive 뒤의 새 shared는 기다리므로 retention 기아는 없다.
- **writer↔writer(영상 advisory)**: 한 문장에서 video_id 정렬로 모두 잡는다. 보유자는 refresh(runtime_state 자기 key 행만, 이 행들은 같은 영상 잠금 없이는 아무도 쓰지 않음)와 commit만 남아 다른 잠금을 기다리지 않는다 → 영상 잠금이 낀 순환 없음.
- **writer↔writer(원본 행)**: guard가 없어지면 관측 확정끼리 다시 병렬이 된다. session/head/pending은 각 문장 안에서 video_id 순서이고 표 순서도 session→head→pending으로 같다(`queries/repository_live_sessions_0045_45.sql` ORDER BY+FOR UPDATE, `0046`, `repository_live_pending_ends.sql`). 남는 위험: 서로 다른 subject가 **새 영상**을 동시에 삽입할 때(예: schedule과 live_snapshot) 한쪽이 상대 삽입을 기다리는 동안 상대가 이쪽이 잡은 다른 행을 기다리는 경우 → 40P01. 312 이전과 같은 성질이며 기존 재시도로 처리된다 [INFERENCE: 운영 빈도 미측정, 2b 뒤 `hololive_youtube_plane_consume_total{result="retry"}`로 관찰].
- **같은 영상의 두 writer**: 세션이 있으면 모든 writer가 session 행을 FOR UPDATE로 먼저 잡으므로(2.1의 모든 경로가 `loadLiveState`/`repository_schedule_sessions.sql`/`loadLiveSessions` 경유) 원본 쓰기 자체가 이미 영상 단위로 직렬화된다. 영상 advisory는 이 규율에 의존하지 않고 갱신 함수 하나로 투영 정합을 보장하기 위한 것이다(계획 4.3 결정). 경합이 거의 없으므로 비용은 잠금 테이블 항목 수준이다.

### 4.5 RR/SER snapshot 거절 의도의 이전과 40001 정책
- 이전: guard 행 MVCC 갱신 → 고정 snapshot은 40001, "새 재시도를 추가하지 않음"(`CHANGELOG.md:78`).
- 이후:
  1. 갱신 함수가 READ COMMITTED가 아니면 거부(3.2). Go writer는 `dbx.InPgxTx*`의 `pgx.TxOptions{}`(서버 기본 RC)라 영향 없음. 계층을 추가하지 않는다.
  2. RC에서는 영상 advisory 대기 뒤 다음 문장이 새 snapshot을 쓰므로 앞선 커밋을 읽는다(312 주석 `:65`과 같은 근거; plpgsql VOLATILE 함수는 RC에서 문장마다 새 snapshot).
  3. 잠금 경합의 40001/40P01/55P03은 새 코드 없이 기존 분류 `rt/supervisor_retry.go:178-189`(`transientSQLState`) → 관측은 `rt/supervisor.go:351` `retryAndForget`(지연 `ClaimInterval`, 시도 상한은 기존 Retry), finalizer는 `rt/supervisor.go:96`에서 로그 후 다음 tick, retention은 `rt/supervisor_retention.go:43`. shared-go `pkg/retry`는 youtube plane 소비 경로에서 쓰지 않으며 도입하지 않는다.
  4. 명시 위치: `so/repository_helpers.go`의 잠금·갱신 helper 문서 주석에 "고정 snapshot 호출은 함수가 거부하고, 잠금 경합 오류는 트랜잭션 안에서 재시도하지 않고 관측 재시도에 맡긴다"를 한국어로 적는다. `so/live_finalizer.go:40-41` 주석("NOW()는 guard 대기 전…")은 "session 행 잠금 대기 전 트랜잭션 시작 시각"으로 고친다.
- **1단계 연동(중요)**: 1단계가 도입 중인 `LiveSessionConflictError`(U1 미커밋 `so/live_persist.go`, `repository_live_session_insert.sql`의 `ON CONFLICT DO NOTHING`+0행=충돌)는 지금은 guard 직렬화 때문에 사실상 도달하지 않지만, 2b 뒤에는 두 subject가 같은 새 영상을 동시에 삽입하면 정상적으로 발생한다. 2b는 이 오류를 `retryableObservationError`가 재시도로 분류하도록 해야 한다(`errors.AsType[*sourceobservation.LiveSessionConflictError]`). 그렇지 않으면 비재시도 → dead-letter로 관측을 잃는다(`rt/supervisor.go:351-375`).

### 4.6 수동 영수증의 고정 snapshot (Q1)
`record_youtube_live_review`는 SERIALIZABLE을 요구한다(`mig/312:192-194`). advisory 대기는 snapshot 확보 뒤에 일어나므로(첫 문장이 snapshot을 잡은 뒤 함수 안에서 대기) MVCC 행이 없으면 대기 중 커밋된 변경을 놓친다. 예: 대기 중 다른 writer가 커밋한 review 입력 변경이 runtime 행을 바꾸지 않은 경우(이전에도 reviewed_at NULL), 영수증 트랜잭션의 refresh가 오래된 view로 reviewed_at을 채워 drift를 만든다.

| 선택지 | 내용 | tradeoff |
|---|---|---|
| O-A (권고) | 함수가 READ COMMITTED를 요구. exclusive 잠금 뒤 FOR UPDATE·snapshot CAS·INSERT·refresh가 모두 최신 커밋을 읽음 | CAS가 더 강해짐(대기 뒤 최신 원본과 비교). 운영 절차의 `BEGIN ISOLATION LEVEL SERIALIZABLE`을 바꿔야 하는 운영 계약 변경 |
| O-D | SERIALIZABLE 유지. 전역 잠금을 advisory 대신 `LOCK TABLE youtube_live_runtime_state IN SHARE ROW EXCLUSIVE MODE`(snapshot 전 획득 가능)로, writer는 ROW EXCLUSIVE로 | 운영자가 함수 호출 전 LOCK 문을 먼저 실행해야 함(역시 절차 변경). writer가 라이브 무관 kind에서도 runtime 표 잠금 보유 |
| 유지 | guard 표를 남김 | 전역 직렬화 그대로 → 2단계 실익 없음(계획 0절) |

---

## 5. 전환 기간(2a) 측정 설계

### 5.1 선택지
| | 측정 가능한 것 | 측정 못 하는 것 | 위험 |
|---|---|---|---|
| D2: 트리거 유지 + Go가 실제로도 갱신 | 계산식 차이(F2일 때) | **key 누락**: 트리거가 이미 맞게 갱신해 가려짐 | 갱신 비용 2배 |
| D3: refresh 트리거만 끄고 guard 유지 + Go 갱신 | 운영 실데이터 drift(기존 오라클 그대로) | – | Go 오류가 곧 소비자 오류(live check 누락) |
| D4: shadow 표 | 완전성+계산식 | – | 표·백필·record 함수까지 2a에서 바꿔야 함 |
| **M1: key shadow 비교(권고)** | key 누락·초과 수, 동일 트랜잭션 | 계산식(F1이라 동일, 불필요) | 소비자 영향 0, 추가 왕복 1/관측 |

### 5.2 M1 상세
- migration **325** `325_youtube_live_runtime_trigger_key_audit.sql`: `BEGIN; SET LOCAL lock_timeout='3s'; SELECT public.lock_youtube_live_runtime_state();` 후 `refresh_youtube_live_runtime_state_statement()`를 316 본문 그대로 재정의하되 `IF video_ids IS NOT NULL THEN` 안에서 refresh 직전에
  ```sql
  PERFORM pg_catalog.set_config('hololive.live_runtime_trigger_keys',
      pg_catalog.concat_ws(',', NULLIF(pg_catalog.current_setting('hololive.live_runtime_trigger_keys', true), ''),
                           pg_catalog.array_to_string(video_ids, ',')), true);
  ```
  `COMMIT;` + manifest 추가 + golden 갱신. 함수의 `SET search_path` 절은 같은 변수의 SET LOCAL만 함수 종료 시 되돌리므로(PostgreSQL CREATE FUNCTION 문서) 다른 GUC의 `set_config(...,true)`는 트랜잭션 끝까지 남는다 [INFERENCE: 문서 근거, 2a dbtest로 확인 필수]. 트랜잭션 끝에 자동 소멸하므로 pool 연결 오염 없음.
- Go(2a): `so/queries/repository_live_runtime_key_audit.sql`
  ```sql
  SELECT count(DISTINCT trigger_key) FILTER (WHERE NOT trigger_key = ANY($1::text[])) AS missing,
         (SELECT count(DISTINCT go_key) FROM unnest($1::text[]) AS go(go_key)
          WHERE NOT go_key = ANY(string_to_array(NULLIF(current_setting('hololive.live_runtime_trigger_keys', true), ''), ','))) AS extra
  FROM unnest(string_to_array(NULLIF(current_setting('hololive.live_runtime_trigger_keys', true), ''), ',')) AS trigger(trigger_key)
  ```
  `finalizeTx`의 3.3-4 위치(모든 kind, K가 비어도 실행 — key를 잊은 경로를 잡기 위해)와 finalizer의 3.3-5 위치에서 실행. 결과는 동작을 바꾸지 않는다(트리거가 정본).
- 지표(2a 한정, 2b에서 삭제): `so`에 `promauto` CounterVec `hololive_youtube_live_runtime_key_audit_missing_total{path}`, `..._extra_total{path}`. path = 관측 kind 문자열 또는 `live_end_finalizer`(유한 집합, 0 시리즈 사전 생성 — `rt/metrics.go`의 `precreateCounterVec` 관례). missing>0이면 `ErrorContext` 로그(kind·count, video id 미기재).
- 오라클 병행: `scripts/maintenance/check-runtime-projections.sh`는 그대로 트리거 정합(기존 오라클 `live_runtime_state_drift.sql`)을 계속 본다.
- 배포 순서: compose `hololive-db-migrate`가 API보다 먼저 325를 적용해야 한다. 325 없이 2a API가 돌면 GUC가 비어 missing=0으로 잘못 읽힌다(구분 불가). 325는 이전 API와 호환(GUC 추가만).

### 5.3 2a 시험(M2)
`so`의 실DB 시험에서 2.1의 각 writer 경로(live_snapshot 신규/기존/부재 slot, schedule, premiere, video live check 활성·ENDED 시각 확정·availability not-applied, finalizer)를 실행하고 missing 지표 증가가 0인지 `prometheus/testutil.ToFloat64`로 확인한다. 2b에서 이 시험들은 "트리거 없이 경로 실행 후 drift 오라클 0"으로 바뀐다(6.5).

### 5.4 2b merge 조건
1. 운영 지표 `..._missing_total` 전 path 0, 관찰 창이 모든 path를 포함(live_snapshot·schedule·video_list·video_live_check·live_end_finalizer 각 증가분 > 0 확인).
2. `check-runtime-projections.sh` live 0|0|0.
3. dbtest·so·rt 시험 통과.

---

## 6. 2b 제거 migration 초안과 브랜치 경계

### 6.1 번호
현재 max = 324(`mig/manifest.txt:185`, 형제 worktree 0-1/0-2/0-4/0-5/1에도 새 migration 없음 — 2026-10-19 확인). 2a = **325**, 2b = **326**. merge 시점에 `max+1` 재확인(`CONVENTIONS.md:36-39`). 312·313·316은 수정하지 않는다.

### 6.2 `326_youtube_live_runtime_explicit_refresh.sql` (순서가 의미를 가진다)
```sql
-- 라이브 수명 모델을 명시 갱신으로 바꾸고 전역 MVCC guard를 advisory 잠금으로 대체한다.
BEGIN;
SET LOCAL lock_timeout = '3s';
-- 1. 기존 guard로 진행 중 writer를 배제한다(이 시점엔 이전 본문).
SELECT public.lock_youtube_live_runtime_state();
-- 2. 원본 트리거 제거(6표 × 5, source_observations guard)
DO $triggers$
DECLARE source_table text;
BEGIN
    FOREACH source_table IN ARRAY ARRAY['youtube_live_sessions','youtube_live_reconciliation_heads',
        'youtube_live_pending_ends','youtube_video_availability','youtube_live_review_receipts','youtube_live_review_receipt_facts']
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_guard ON public.%I', source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_insert ON public.%I', source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_update ON public.%I', source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_delete ON public.%I', source_table);
        EXECUTE format('DROP TRIGGER IF EXISTS youtube_live_runtime_truncate ON public.%I', source_table);
    END LOOP;
END
$triggers$;
DROP TRIGGER IF EXISTS youtube_live_runtime_guard ON public.source_observations;
-- 3. 트리거 함수 제거(트리거가 먼저 없어야 DROP 가능)
DROP FUNCTION IF EXISTS public.refresh_youtube_live_runtime_state_statement();
DROP FUNCTION IF EXISTS public.guard_youtube_live_runtime_state();
-- 4. 전역 잠금 교체: exclusive(기존 이름) + shared(신규)
CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state() ... pg_advisory_xact_lock(hashtextextended('youtube_live_runtime_global',0)) ...;
CREATE OR REPLACE FUNCTION public.lock_youtube_live_runtime_state_shared() ... pg_advisory_xact_lock_shared(같은 키) ...;
REVOKE ALL ON FUNCTION public.lock_youtube_live_runtime_state_shared() FROM PUBLIC;
-- 5. 계산 함수 교체(3.2)
CREATE OR REPLACE FUNCTION public.refresh_youtube_live_runtime_state(p_video_ids text[]) ...;
-- 6. 수동 영수증(Q1=O-A일 때): RC 요구, exclusive 잠금, FOR UPDATE, CAS, INSERT, refresh(ARRAY[p_video_id])
CREATE OR REPLACE FUNCTION public.record_youtube_live_review(...) ...;
-- 7. guard 표 제거(잠금 함수가 더는 참조하지 않은 뒤)
DROP TABLE IF EXISTS public.youtube_live_runtime_state_guard;
-- 8. 권한
DO $roles$ ... 
    hololive_runtime: GRANT EXECUTE ON refresh(text[]), lock_..._shared() ; 표는 SELECT만 유지
    hololive_scraper: 새 함수도 REVOKE
    record_youtube_live_review EXECUTE 보유 역할(312:236-246과 같은 aclexplode 루프): refresh(text[]) EXECUTE 추가
      (invoker 함수가 SECURITY DEFINER refresh를 부르므로 필요. 시험 TestLiveReviewRecorderDecoderMigrationPreservesRestrictedRecording이 이 경로를 고정)
$roles$;
-- 9. 모델 COMMENT 갱신: "writer가 트랜잭션 끝에서 변경 key로 refresh를 호출한다. 관측 writer는 shared, 배제 경로는 exclusive 전역 잠금."
-- 10. 전체 재계산(312:249-252와 같은 key 합집합). 트리거 기간 정합을 그대로 넘기고 drift 0에서 시작한다.
SELECT public.refresh_youtube_live_runtime_state(ARRAY(
    SELECT video_id FROM public.youtube_live_runtime_state
    UNION SELECT video_id FROM public.youtube_live_sessions
    UNION SELECT video_id FROM public.youtube_live_reconciliation_heads));
COMMIT;
```
- 멱등: DROP IF EXISTS, CREATE OR REPLACE, 재적용 시 1의 호출은 새 본문(exclusive)이라 무해. 10은 RC(러너 BEGIN 블록) [INFERENCE: 러너가 isolation 지정 BEGIN을 거부하므로 기본 RC, `CONVENTIONS.md:14`].
- 315 함수는 다시 만들지 않는다(4.2).
- manifest 추가, `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 golden 갱신.

### 6.3 SQL 외부 자산(2b)
- `scripts/maintenance/reconcile-terminal-live-heads.sql`: `:38-48` 검토 trigger VALUES 목록은 `'public.guard_youtube_live_runtime_state()'::regprocedure` 등 DROP된 함수를 캐스트하므로 **실행 자체가 실패**한다 → 목록 제거, "대상 표에 내부 아닌 trigger/rule이 하나라도 있으면 거부"로 단순화. `:109-111` UPDATE 뒤 `PERFORM public.refresh_youtube_live_runtime_state(ARRAY(SELECT video_id FROM terminal_head_repair ORDER BY video_id))`. `:19`(exclusive)는 그대로.
- `scripts/runtime/test-terminal-live-head-repair.sh:67`: 검토 안 된 trigger 예시가 `guard_youtube_live_runtime_state()`를 쓴다 → 시험용 함수로 교체. `:97-98` 기대(대상 행 runtime 0, 비대상 불변)는 refresh 추가로 유지.
- `scripts/maintenance/check-runtime-projections.sh`·`live_runtime_state_drift.sql`: 변경 없음(2b 이후 실제 Go 정합 측정 도구가 됨).
- `mig/CONVENTIONS.md`: "라이브 원본(6표)을 바꾸는 migration은 같은 트랜잭션에서 `lock_youtube_live_runtime_state()` 뒤 변경 key로 `refresh_youtube_live_runtime_state()`를 호출한다" 한 절.

### 6.4 Go(2b)
3.3 전부 + `repository_live_runtime_lock.sql` → `SELECT public.lock_youtube_live_runtime_state_shared()`, 2a의 audit SQL·지표·M2 지표 시험 삭제, `retryableObservationError`에 1단계 충돌 오류 분류(4.5).

### 6.5 문서·CHANGELOG(2b)
- `docs/current/services/hololive-api.md:166-168`(316의 BEFORE guard·RR 거절 서술) 갱신.
- 1단계 PR이 만드는 잠금 순서 표(U1 산출물, `docs/current/contracts/` 또는 해당 현재 문서)의 guard 행을 4.3 표로 교체.
- `docs/current/runbooks/terminal-live-head-repair.md:12` 문구(검토 trigger 허용) 갱신.
- Q1=O-A면 운영 영수증 절차 문서(현재 `docs/design/2026-10-02-...:551`은 이력 문서라 수정하지 않음; 현재 runbook이 없으면 services 문서에 RC 요구 명시).
- CHANGELOG `## 미출시` 맨 위 한 항목(2a·2b 각 PR). 2b 항목에 "API 정지 → db-migrate(326) → 신규 API, 이미지만 되돌리는 롤백 불가" 명시(312 선례 `CHANGELOG.md:79`).

### 6.6 브랜치 경계와 배포

| | 2a (트리거·guard 유지) | 2b (제거) |
|---|---|---|
| migration | 325(GUC 기록) | 326 |
| Go | K plumbing(3.3-3), audit 호출(5.2), 지표 | 명시 refresh, shared 잠금, audit·지표 삭제, 충돌 오류 재시도 분류 |
| 시험 | M2 경로별 missing=0, 325 GUC 지속 확인 1건 | 7절 재작성 + 경로별 drift 0 |
| 배포 | db-migrate(325) → API. 롤링 가능(325는 이전 API와 호환) | **API 정지 → db-migrate(326) → 신규 API**. 이전 API + 326 = refresh 없음(drift)·전 관측 exclusive 직렬화 → 이미지 단독 롤백 불가, 전진 복구 |
| merge 조건 | 일반 | 5.4 충족 전 금지 |

---

## 7. 시험 목록과 재작성 방침

계획의 "runtime_state 11 + guard 2"는 `dbt/live_runtime_state_migration_test.go` 9개 + `dbt/live_runtime_state_concurrency_test.go` 2개 = 11, `so/live_runtime_guard_test.go` 2개 = guard 2다. 계획에서 빠진 `dbt/live_runtime_update_filter_migration_test.go` 3개와 guard 의존 시험 1개도 함께 다룬다.

| # | 시험 (파일:줄) | pool | 2b 방침 |
|---|---|---|---|
| 1 | `TestLiveRuntimeStateDriftCountsEachMismatch` (`dbt/live_runtime_state_migration_test.go:41`) | NewPool | 유지·수정: `lock_…()`+트리거 대신 sessions INSERT 후 `refresh(...)` 호출, 이후 손상 3종 → (1,1,1). 오라클이 운영 2b 측정 도구가 되므로 가치 유지 |
| 2 | `TestLiveRuntimeStateMigrationBackfillAndReapply` (`:60`) | 312 이전 | 변경 없음(312 이력 시험) |
| 3 | `TestLiveRuntimeStateImmediateChangesAndRollback` (`:91`) | NewPool | 재작성: 각 DML 뒤 영향 key(이동은 old/new 둘 다)로 `refresh` 호출 → tx 안 drift 0, rollback 뒤 drift 0. TRUNCATE 6건 삭제(운영 TRUNCATE 경로 없음, 갱신 책임 제거) |
| 4 | `TestLiveRuntimeStateReceiptInsertionChangedAndRevertedFacts` (`:137`) | NewPool | 재작성: 영수증 기록(Q1=O-A면 RC tx) 후 reviewed_at 확인, title 변경마다 `refresh` 호출로 reopen/legacy 의미 확인, facts append-only 거부 유지 |
| 5 | `TestLiveRuntimeStateTechnicalWritesAndGuardReseed` (`:173`) | NewPool | guard reseed 부분 삭제(표 없음). 기술적 쓰기 뒤 `refresh`가 같은 행을 다시 쓰지 않음(ctid 불변)만 남기고 `TestLiveRuntimeStateRefreshKeepsUnchangedRow`로 개명(316 비용 계약의 남은 부분) |
| 6 | `TestLiveRuntimeStateMigrationPrivilegesAndRecordingACL` (`:208`) | 312 이전 | 변경 없음(312 이력). 326 권한은 새 시험(아래 N1) |
| 7 | `TestLiveRuntimeStateObservationDeleteAndFactsReseed` (`:279`) | NewPool | 원본 DELETE → FK SET NULL 뒤 refresh 없이 drift 0·reviewed 유지 부분 유지(retention이 K를 내지 않아도 되는 근거). facts TRUNCATE 재적재 부분 삭제 |
| 8 | `TestLiveRuntimeStateMigrationFailureIsAtomic` (`:324`) | 312 이전 | 변경 없음 |
| 9 | `TestLiveRuntimeStateRecordingValidationAndCAS` (`:347`) | NewPool | Q1=O-A면 "isolation" 사례를 Serializable→`requires read committed`로, 나머지 사례 RC로 |
| 10 | `TestLiveRuntimeStateConcurrentDistinctSourceWriters` (`dbt/live_runtime_state_concurrency_test.go:17`) | NewPool | 재작성(핵심): RC 두 tx가 같은 영상의 sessions/heads를 각각 쓰고 각자 `refresh(['concurrent'])` 호출. 둘째가 첫째의 영상 advisory에 막힌 것을 `pg_blocking_pids`로 확인(`waitLiveRuntimeGuard` 재사용) → 첫째 commit → 둘째 결과가 합성 상태(`assertCombinedLiveRuntimeState`) |
| 11 | `TestLiveRuntimeStateStaleFixedSnapshotFails` (`:90`) | NewPool | 재작성: RR/SER tx에서 `refresh` 호출이 `requires read committed`로 거부되고 모델이 섞이지 않음(drift 0). 40001 단언 삭제 |
| 12 | `TestLiveRuntimeUpdateFilterMigrationPreservesDataAndPrivileges` (`dbt/live_runtime_update_filter_migration_test.go:16`) | 316 이전 | 변경 없음(316 이력). `liveRuntimeRefreshSecurity`(`:35-44`)가 DROP될 `..._statement()`를 조회하지만 316 직후 시점이라 유효 |
| 13 | `TestLiveRuntimeUpdateFilterInvalidatesCandidateReceiptFields` (`:46`) | NewPool | 재작성: 후보 입력 UPDATE 뒤 `refresh` 호출 → reviewed_at NULL, 복원 뒤 복구. 계산 함수의 검토 입력 의미 시험으로 유지 |
| 14 | `TestLiveRuntimeUpdateFilterStaleTechnicalWriterFails` (`:110`) | NewPool | 삭제(BEFORE guard 소멸, 고정 snapshot 거절은 #11이 담당) |
| G1 | `TestFinalizeRuntimeGuardPrecedesClaimAndRollsBackProjection` (`so/live_runtime_guard_test.go:19`) | NewPool | 재작성: holder가 `SELECT lock_youtube_live_runtime_state()`(exclusive) → Finalize가 shared에서 claim 전 대기(NOWAIT 확인 유지) → 해제 후 성공이면 runtime 1행, rollback이면 0행. 콜백 `runtimeProjectionWrite`(`:155-178`)의 콜백 내 runtime 조회(트리거 전제)를 제거하고 결과 `LiveRuntimeVideoIDs`로 key 반환, 판정은 commit 뒤 |
| G2 | `TestEvidenceRetentionGuardsBeforeCandidateAndPreservesAvailability` (`:81`) | NewPool | 유지: holder exclusive(이름 그대로) → retention이 후보 잠금 전 대기. holder를 shared로 바꾼 하위 사례 추가(관측 writer 보유 중 retention 대기) — 4.3의 shared/exclusive 배제를 고정 |
| G3 | `TestLiveFinalizerUsesConsumerLockOrderAndRechecksDue` (`so/live_evidence_persist_test.go:383`) | NewPool | 재작성: consumer 대역 tx가 guard 대신 session 행을 `FOR UPDATE`로 잡는다(실제 consumer 순서). finalizer가 session 잠금에서 대기, 대기 중 due가 된 후보는 처리하지 않음(`processed=false`) 유지 |
| N1 | 신규 `dbt/live_runtime_explicit_refresh_migration_test.go` | 326 이전 | 326 재적용 멱등·데이터 보존(drift 0), 트리거·guard 표·함수 부재, runtime 역할: 표 쓰기 불가·refresh/shared EXECUTE 가능, 기록 역할 refresh EXECUTE |
| N2 | 신규 `so` 경로별 시험(5.3의 2b판) | NewPool | 각 writer 경로 실행 후 drift 오라클 0. 오라클 SQL을 `so`에서 읽도록 `hololive-dbtest`에 비시험 helper `LiveRuntimeStateDrift(tb, db)`(저장소 루트 탐색은 `dbt/migrations.go:342-370` 재사용)를 두고 dbt의 `liveRuntimeStateDrift`(`:21-32`)도 이를 쓰게 한다 |

트리거 의존 fixture(2b에서 깨지는 다른 시험): 원본을 직접 INSERT/UPDATE한 뒤 소비 SQL을 읽는 `rt/` 시험 — `collection_guard_load_test.go`, `collection_lifecycle_performance_test.go`, `collection_metrics_test.go`(+`rt/testqueries/collection_target_observability.sql` seed), `lifecycle_diagnostics_test.go`, `live_check_projection_test.go`, `live_review_plan_test.go`, `live_review_source_sharing_test.go`, `time_finalization_projection_test.go`, `projection_retention_capacity_test.go` [INFERENCE: seed와 소비 SQL/refresher 사용이 같은 파일에 있음을 grep으로만 확인]. 방침: seed 뒤 전체 key refresh helper 한 번 호출. 2b 구현 뒤 rt 시험 실행 결과로 대상 확정. `record_youtube_live_review` 호출 시험(Q1=O-A 시 격리 수준 변경): `dbt/live_review_recorder_decoder_migration_test.go:140`, `dbt/live_review_receipt_facts_migration_test.go:223`, `dbt/live_runtime_update_filter_migration_test.go:88`, `rt/collection_lifecycle_performance_test.go:229`, `rt/lifecycle_diagnostics_test.go:108`, `rt/live_review_semantic_test.go:85`, `so/video_lifecycle_persist_test.go:402`. 워커·봇·livequery 시험의 원본 seed는 runtime_state를 읽지 않아 영향 없음.

---

## 8. 1단계와의 충돌 지점

U1(브랜치 `refactor/live-state-policy-go`, worktree apsm-1)이 2026-10-19 현재 편집 중인 파일(미커밋 `git status`): `so/live_persist.go`, `so/live_finalizer.go`, `so/live_consumer.go`, `so/live_check_consumer.go`, `so/schedule_persist.go`, `so/schedule_consumer.go`, `so/queries/repository_live_session_upsert_0047_47.sql`(삭제)→`repository_live_session_insert.sql`/`update.sql`(신규), `so/queries/repository_schedule_sessions.sql`, `api/internal/youtube/reconcile/live/apply*.go`, `reconcile/schedule/reduce.go`, `hololive-shared/pkg/dbx/tx.go`(`Statement.RequireRow`, `NoRowsError`).

| 2단계 변경 | 충돌하는 1단계 변경 | 처리 |
|---|---|---|
| `persistLiveDecision`이 K 반환 | 1단계가 signature를 `(ctx, tx, stored, decision)`으로 변경 | 2a는 1단계 merge 뒤 그 signature 위에서 반환값만 추가 |
| `persistScheduleDecision`·premiere K | `schedule_persist.go`에 `stored` 인자 추가 | 동일 |
| `live_finalizer.go:31,40-41,71` | `:71` 호출 인자 변경 | 같은 함수, 순차 rebase |
| `live_check_consumer.go:107-164` K 수집 | `:160` 호출 인자 변경 | 동일 |
| `repository_finalize.go:90,119` | 1단계 3번(claim DEAD_LETTER Go 이관)이 `lockClaim`·`rejectFinalizeObservation`·`deadLetter*`(`:129-200`)와 claim SQL을 바꿀 수 있음 | 2단계는 `:90` 한 줄과 `:119` 뒤 한 호출만. 1단계의 거절 분기(`:99-106`)는 갱신 호출 전에 반환하므로 K 없음 — 거절 경로가 라이브 원본을 쓰지 않는지 1단계 결과로 재확인 |
| `LiveSessionConflictError` 재시도 분류 | 1단계 신규 오류 | 2b에서 `retryableObservationError` 분류 필수(4.5). 1단계가 이미 분류하면 중복 추가 금지 |
| 잠금 순서 표 문서 | 1단계 요구 1 산출물 | 2b가 guard 행을 4.3 표로 교체 |
| CHANGELOG `## 미출시` 맨 위 | 1단계 항목 | 각 PR 한 항목, rebase 시 순서만 |
| `live.Reduce`의 dirty 집합 | 1단계가 CASE 규칙을 reducer로 옮기며 dirty 판정이 바뀔 수 있음 | K는 `decision.Sessions`에서 파생하므로 자동 추종. 2a 지표가 누락을 잡음 |

---

## 9. open_questions
1. **Q1**: `record_youtube_live_review`의 SERIALIZABLE 요구를 READ COMMITTED로 바꿔도 되는가(O-A). 운영 영수증 절차·오류 문구가 바뀐다. 거부 시 O-D(LOCK TABLE 기반 전역 잠금)로 설계를 바꿔야 한다.
2. **Q2**: F1(계산 함수 유지, runtime 역할에 EXECUTE만 추가)로 진행해도 되는가. 계획 문구대로 Go SQL이 표를 직접 upsert(F2)하려면 `hololive_runtime`에 runtime_state 쓰기 권한을 주는 보안 계약 변경이 필요하다.
3. **Q3**: retention은 계획 4.3과 달리 exclusive 전역 잠금을 유지하도록 권고했다(312 이후의 retention↔writer 배제·보호 판정 일관성 보존). retention까지 배제를 풀어 312 이전 동작(40P01 재시도 허용)으로 돌아갈지는 운영 측정 후 별도 결정.
4. 2a 관찰 창 길이(모든 path 증가분 확보 기준, 예: 최소 7일)와 2b 운영 적용 승인 시점.

## 10. risks
- 2b 뒤 관측 확정끼리 병렬이 되어 새 영상 동시 삽입 등에서 40P01·1단계 충돌 오류가 생긴다. 재시도 분류 누락 시 dead-letter로 관측 유실. 빈도 미측정.
- K 누락은 2b 뒤 소비자(live check 대상·observability) drift로 바로 드러난다. 2a 지표가 경로를 덮지 못하면(드문 path) 놓칠 수 있다.
- 325의 GUC 지속 동작은 문서 근거만 있고 실행 확인 전이다 [INFERENCE]. 확인 실패 시 2a 측정 수단을 shadow 표(D4)로 바꿔야 한다.
- 326 이후 이전 API 이미지로 롤백 불가(트리거 없음 + exclusive 직렬화). 전진 복구만.
- maintenance 스크립트는 326 적용 즉시 regprocedure 캐스트로 실패하므로 326과 같은 PR에서 고쳐야 한다.
- K 상위집합으로 316이 생략하던 refresh가 일부 되살아날 수 있다 [INFERENCE: 차이 작음]. 2a extra 지표와 pg_stat_statements로 비교.
- 트리거 의존 rt 시험 fixture가 다수라 2b 시험 수정 범위가 크다.
