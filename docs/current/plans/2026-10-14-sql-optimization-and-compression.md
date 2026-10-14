# Hololive SQL 최적화 및 압축 플랜 (2026-10-14)

## 목표와 범위

`hololive-bot` 운영 PostgreSQL(`holo-postgres`, PG 18.6, v9.3.2 / migration 303 기준)의 읽기 전용 실측을 바탕으로, **SQL 실행 계획·잠금 최적화(S1~S5)**와 **데이터 압축·공간 회수(C1~C3)**의 실행 순서와 의존성을 정리한다.

- **기준 상태 (2026-10-14 01:10 UTC, 읽기 전용 가드 `on`):**
  - 전체 DB 크기(`pg_database_size('hololive')`): **4,097 MB (`4,296,054,463 bytes`)**
  - 상위 10개 테이블이 **4,014 MB(전체의 98.0%)**를 차지한다.
  - 이전 계획([2026-10-05 hotpath](2026-10-05-db-hotpath-optimization.md), [2026-10-06 runtime](2026-10-06-db-runtime-optimization.md), [2026-10-08 followup](2026-10-08-db-review-followup.md), [2026-10-12 SQL·fadeout](2026-10-12-sql-optimization-and-compat-fadeout.md))에서 이미 반영된 항목(B1~B3 인덱스, `youtube_live_sessions` fillfactor·생성 열 등)은 제외하고 현재 남아 있는 병목과 공간 요인만 다룬다.
- **원칙:**
  - 수집 주기·대상·알림 정확도·보존 보호 조건(`CURRENT`/`STAGING` projection, 활성 lease, 미처리 queue/replay, 활성 `LIVE`/`UPCOMING` 세션)은 약화시키지 않는다.
  - 새 재시도·호환 fallback·제거 코드 재등장 grep 가드는 추가하지 않는다.

## 운영 현황 실측 요약 (2026-10-14 01:10~01:35 UTC)

`hololive-seoul`(`100.100.1.8`) `holo-postgres`에서 `default_transaction_read_only=on`, `statement_timeout=10s`, `lock_timeout=2s`를 확인한 세션으로 카탈로그·통계·계획만 조회했다.

| 순위 | 테이블 | 전체 크기 | Heap | Index | TOAST | 행 수(추정/집계) | 핵심 진단 |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | `source_observation_applications` | 1,789 MB | 1,129 MB | 660 MB | 352 kB | 4,870,407 | 전체의 **43.7%**. `observation_id IS NULL` 고아 행 28.8%(~140만 행), `ON DELETE SET NULL`로 인한 비-HOT UPDATE 2,044만 건(HOT 0.0%) |
| 2 | `source_observations` | 1,030 MB | 589 MB | 441 MB | 200 kB | 1,087,857 | 전체의 **25.1%**. `scope_sha256`·`evidence_sha256` 64자 hex 텍스트(행당 130 B) |
| 3 | `youtube_live_absence_slots` | 317 MB | 282 MB | 35 MB | 112 kB | 88,218 | **Heap 빈 공간 248.57 MB(88.2%)**, 실제 튜플 32.95 MB. `pkey`·`scheduled_for` 인덱스도 삭제 페이지 87.6%(1,818/2,074 페이지) |
| 4 | `youtube_collection_target_reasons` | 257 MB | 130 MB | 127 MB | 72 kB | 986,962 | `CURRENT` 1세대(586행) + `RETIRED` 1,647세대(7일 보존, 일 ~235세대) |
| 5 | `youtube_collection_targets` | 209 MB | 133 MB | 76 MB | 72 kB | 986,962 | `reasons`와 합계 **466 MB(11.4%)** |
| 6 | `youtube_live_viewer_samples` 외 퇴역 4종 | 151 MB | 89 MB | 62 MB | 128 kB | 1,093,561 | **퇴역 기능 잔재**. 2026-09-25 이후 신규 적재 0건, 런타임 조회 경로 0건 |
| 7 | `youtube_live_reconciliation_heads` | 114 MB | 1,032 kB | 288 kB | **113 MB** | 4,463 | `ignored_absence_scheduled_for` 배열(`pglz`). **종료(`ENDED`) 2,655행이 1,460만 원소(76 MB) 점유** |
| 8 | `source_observation_payloads` | 87 MB | 8.9 MB | 4.0 MB | 74 MB | 16,143 | `payload` 열 `lz4` 적용됨(압축 합계 65 MB: `schedule` 29 MB, `community` 25 MB) |
| 9 | `youtube_content_absence_slots` | 40 MB | 30 MB | 10 MB | 40 kB | 76,048 | `coverage` JSONB(`pglz`), 해시 2열 hex 텍스트 |
| 10 | `source_observation_queue` | 35 MB | 20 MB | 16 MB | 40 kB | 216,888 | v9.2.1(302·303)에서 보존 삭제 부분 인덱스 반영 완료 |

---

## 1. SQL 실행 계획 및 Hot-Path 최적화 (S1 ~ S5)

### S1. `collection_target_observability.sql` — `live_states` 조인 순서 역전 및 `live_reviews` 루프 제거 (P1)

- **실측 근거:** 누적 25,468회, 평균 **77.40 ms**, 호출당 평균 **11,820 블록(92.3 MB)** 읽기. `EXPLAIN (ANALYZE, BUFFERS)` 실측(`27.48 ms`, `10,895` 블록)에서:
  1. `live_states` CTE가 `targets`(통계 없음 → `kind = 'youtubejs_channel_live'`를 1행으로 오판, 실제 119행)에서 시작해 `youtube_live_sessions`의 `idx_yls_channel` Bitmap Heap Scan을 119회 반복하며 **과거 `ENDED` 세션을 포함한 전체 10,002행(`8,553` 블록, 전체 읽기의 78.5%)**을 읽은 뒤 `active_live_videos`(215행)와 Hash Join하며 9,800행을 버린다.
  2. `live_reviews` CTE가 53개 영수증 영상마다 `youtube_live_review_current_receipt(video.video_id)`를 상관 SubPlan(`loops=53`, `951` 블록)으로 실행해 이미 읽은 테이블을 다시 53번씩 조회하고 `youtube_live_review_receipt_facts`(7페이지)를 53회 Seq Scan(`371` 블록)한다.
- **변경:**
  - [ ] `active_live_videos`를 `MATERIALIZED`로 고정하고, 채널 대상 여부는 `EXISTS (SELECT 1 FROM targets t WHERE t.kind = 'youtubejs_channel_live' AND t.subject_key = product.channel_id)`로 검사해 활성 영상(~215행)만 PK로 조회하게 한다.
  - [ ] `live_reviews`는 S2의 집합형 판정(또는 `live_states`와 `youtube_live_review_receipt_facts`의 직접 조인)으로 바꿔 53회 상관 SubPlan을 제거한다.
- **기대 효과:** 호출당 블록 읽기 **10,895 → ~450 블록(-95.9%)**, 실행 시간 **27~77 ms → ~2~3 ms**.

### S2. `live_check_videos.sql` 및 `youtube_live_review_current_receipt` — Projection Guard 내부 5초 주기 비용 축소 (P1)

- **실측 근거:** 5초마다 `youtube_collection_projection_guard` 잠금 안에서 실행된다. 누적 96,262회, 평균 **8.72 ms**, 호출당 **1,746 블록**. `EXPLAIN (ANALYZE, BUFFERS)` 실측(`8.46 ms`, `1,387` 블록) 중 **`1,078` 블록(77.7%)과 `5.87 ms`(69.4%)**가 `reviewed_videos` CTE(`SubPlan 5`, `loops=53`)에서 발생한다.
  - `youtube_live_review_current_receipt(p_video_id)`가 `to_jsonb(session)` 등으로 전체 스냅샷 JSONB를 만든 뒤(19개 `timestamptz`를 문자열로 직렬화), `youtube_live_review_semantic_facts(snapshot)`에서 다시 19개 문자열을 `::timestamptz`로 파싱해 `extract(epoch FROM ...)`를 구하는 이중 변환을 매 호출 53번 수행한다.
  - `youtube_live_review_receipt_facts`가 53행(7페이지)이라 인덱스를 쓰지 않고 매번 7페이지 Seq Scan × 53회 = `371` 블록을 읽는다.
- **변경:**
  - [ ] 현재 원본의 의미 사실 JSONB를 `to_jsonb` 문자열 재파싱 없이 SQL `timestamptz` 열에서 직접 `extract(epoch FROM ...)`로 구성하거나(판정 결과 100% 동등성 검증), `review_videos` 집합 전체에 대해 1회 Hash Join으로 판정하도록 개선한다.
- **기대 효과:** 호출당 블록 읽기 **1,387 → ~280 블록(-80%)**, 실행 시간 **8.5 ms → ~1.5 ms**, Projection Guard 잠금 보유 시간 단축.

### S3. `extend_targets.sql` 및 `replaceReasonsIfChanged` — Heartbeat 시 587회 B-Tree 탐색 및 Reasons 재조회 제거 (P1)

- **실측 근거:**
  - `extend_targets.sql` (`3047231863221814013`): 164,562회 호출, 평균 변경 행은 **1.05행**인데 호출당 **1,998.2 블록(~16 MB)**을 읽는다. 플래너가 `unnest($2, $3, $4)`를 10행으로 추정해 `youtube_collection_targets_pkey`를 587번 Nested Loop Index Scan(`587 × 3.4 = 1,998` 블록)하기 때문이다.
  - `load_reasons.sql` (`-6846211758071134799`): 5초마다 target 해시가 동일한 `extendUnchangedGeneration` 안에서도 현재 세대의 `youtube_collection_target_reasons` 587행을 매번 읽어 Go로 전송·비교한다.
- **변경:**
  - [ ] `extend_targets.sql`에서 현재 세대(`WHERE projection_generation = $1`, PK 연속 범위 ~20 블록)를 먼저 읽고 `unnest` 입력과 조인해 `not_before IS DISTINCT FROM`인 행만 `ctid`(또는 PK)로 `UPDATE`하거나, Go `Refresher`에서 실제로 `not_before`가 바뀐 대상만 전달한다.
  - [ ] `Refresher`가 현재 활성 `generation`과 마지막으로 확인한 `reasons` 해시를 메모리에 기억해, 같은 세대에서 `reasons`가 그대로면 `load_reasons.sql` 조회를 생략한다.
- **기대 효과:** `extend_targets.sql` 호출당 블록 읽기 **1,998 → ~20 블록(-99%)**, 무변경 heartbeat의 `load_reasons.sql` 조회 0회.

### S4. Live 관측 소비 경로 — `PARTIAL` 관측의 불필요한 TOAST 배열 적재 및 Absence GIN 스캔 제거 (P1)

- **실측 근거:**
  - 운영 `live_snapshot` 관측의 **94.2%(`653,021 / 692,985`)는 `PARTIAL`**이다.
  - 신규 `UPCOMING`이나 첫 `LIVE` positive가 없는 일반 `PARTIAL` 관측에서는 reducer가 부재(`applyAbsenceToKnownSession`)를 전혀 평가하지 않는데도, `repository_live_heads_0046_46.sql`(`8119211535387059239`, 평균 **3.29 ms**, 15초 구간 **5.46 ms**, 런타임 총 실행 시간 1위)이 채널의 모든 `UPCOMING` 세션(196행, 원소 218만 개 = 압축 11.7 MB)의 `ignored_absence_scheduled_for` 배열을 매번 TOAST에서 읽는다.
  - `lifecycle_origin IN ('metadata_only', 'legacy_unknown')`이고 positive clock이 없는 `UPCOMING` 55행(원소 57.7만 개, 3.1 MB)도 `applyAbsenceToKnownSession` 초입에서 바로 반환되어 배열을 쓰지 않지만 매번 적재된다.
  - `loadLiveAbsenceSlots`는 `len(channelIDs) == 0`일 때도 단일 `OR` 쿼리(`repository_live_absence_slots.sql`)에 빈 배열 `$1 = '{}'`을 넘겨 generic plan에서 `BitmapOr`(GIN 인덱스 포함)를 수행한다.
- **변경:**
  - [ ] `!scopedNegative(&evidence) && !needsPositiveAbsenceReplay(&state, &evidence)`인 관측에서는 모든 영상을 `omitIgnoredFor`(`$2`)에 넣고 `loadLiveAbsenceSlots` 호출을 건너뛴다.
  - [ ] positive clock이 없는 `metadata_only`·`legacy_unknown` 세션도 항상 `omitIgnoredFor`에 포함한다.
  - [ ] `loadLiveAbsenceSlots`에서 `len(channelIDs) == 0`일 때는 `WHERE scheduled_for = $1 AND (coverage -> 'requested_channel_ids') ?| $2::text[]` 전용 문장으로 분리해 `idx_youtube_live_absence_slots_scheduled_for`만 사용한다.
- **기대 효과:** `repository_live_heads_0046_46.sql` 평균 시간 **3.29~5.46 ms → ~0.2 ms(-94%)**, 일반 `PARTIAL` 관측의 TOAST 배열 읽기 및 absence slot 조회 0회.

### S5. Collector Lease 획득(`acquireTx`) 및 후보 조회 — 무의미한 `INSERT` 제거와 WAN 왕복 축소 (P2)

- **실측 근거:**
  - `sqlLeaseInsert` (`-4926252849196727417`): 누적 **2,783.6만 회** 호출 중 실제 삽입은 **3,012건(0.011%)**이고 **99.989%가 no-op**이다(15초 구간 98회 호출, `254.51 ms`, 삽입 0건).
  - `acquireTx`가 `youtube_collection_projection_guard`를 `FOR SHARE`로 잡은 채 4번의 순차 SQL 왕복(`lockProjectionGuard` → `verifyAcquireTargets` → `insertAcquireJobRow` → `acquireLeaseProof`)을 수행해, 원격 AP(`osaka`, `osaka2`)가 WAN RTT 동안 공유 잠금을 보유한다. 그 영향으로 중앙 API의 `SELECT guard_key ... FOR UPDATE`(`-7931210353652924045`)가 3블록만 읽으면서도 평균 **19.08 ms**(15초 구간 **65.68 ms**) 대기한다.
  - `repository_target_bundle_0144_04.sql`(`6395284297819869485`)은 `cadence`와 `membership` 두 CTE가 같은 `youtube_collection_targets`를 두 번 읽고 `(NOT $3::boolean OR subject_key = $4)`로 인해 generic plan에서 `subject_key` PK 조건을 활용하지 못한다.
- **변경:**
  - [ ] `acquireTx`에서 `verifyAcquireTargets` + 조건부 행 생성 + `UPDATE` 선점을 단일 SQL(또는 기존 행 `UPDATE` 우선 시도 후 행이 없을 때만 `INSERT`)로 합쳐 **99.99%의 무의미한 `INSERT`와 트랜잭션 내 WAN 왕복 2회를 제거**한다.
  - [ ] `repository_target_bundle_0144_04.sql`의 `cadence`·`membership` 집계를 단일 스캔(`FILTER` 절)으로 합치고 `ExactSubject` 여부에 따라 문장을 분리한다.
- **기대 효과:** 무의미한 `INSERT` 제거, 원격 collector의 `projection_guard` 잠금 보유 시간 50% 이상 단축.

---

## 2. 데이터 압축 및 용량 회수 플랜 (C1 ~ C3)

### C1. 무손실 공간 회수 및 퇴역 잔재 정리 (단기: **약 540 ~ 860 MB 절감**, 4.10 GB → ~3.25 GB)

1. **`C1-1` `youtube_live_reconciliation_heads` 종료(`ENDED`) 세션 배열 비우기 및 `lz4` 전환 (~104 MB 절감)**
   - **근거:** `youtube_live_reconciliation_heads`(114 MB 중 TOAST **113 MB**)에서 이미 종료된 `ENDED` 2,655행이 **1,459.5만 개 원소(압축 76 MB)**를 가지고 있다. `ENDED` 세션의 배열은 어떤 런타임 경로도 읽지 않고 검토 영수증 대상(`UPCOMING` 전용)도 아니다.
   - **조치:**
     - [ ] 코드에서 `ENDED` 상태로 저장할 때 `ignored_absence_scheduled_for`를 빈 배열(`'{}'`)로 비운다.
     - [ ] migration에서 `ALTER TABLE youtube_live_reconciliation_heads ALTER COLUMN ignored_absence_scheduled_for SET COMPRESSION lz4;` 적용 및 기존 `ENDED` 행의 비어 있지 않은 배열을 keyset 배치로 `'{}'`로 비운다.
     - [ ] `VACUUM (FULL, ANALYZE) youtube_live_reconciliation_heads`(4,463행, ~1 MB라 짧은 잠금으로 즉시 완료)로 TOAST 약 100 MB를 OS에 반환한다.

2. **`C1-2` `youtube_live_absence_slots`·`youtube_collection_job_leases`·`youtube_live_pending_ends` 블로트 회수 (~285 MB 절감 + 후보 조회 가속)**
   - **근거:**
     - `youtube_live_absence_slots`: 317 MB(Heap 282 MB + Index 35 MB) 중 실제 데이터는 **32.95 MB**이고 **248.57 MB(88.2%)가 빈 공간**이다. `pkey`·`scheduled_for` 인덱스도 87.6%가 삭제된 페이지다.
     - `youtube_collection_job_leases`: 505행(186.6 kB)인데 Heap이 856 kB(107페이지, 빈 공간 69.8%)이고 `idx_youtube_collection_job_projection_generation`이 3,064 kB(삭제 페이지 87.1%)다. 이 Heap 블로트 때문에 초당 25회 실행되는 `repository_candidates_0144_02.sql`이 매번 107페이지를 Seq Scan한다.
     - `youtube_live_pending_ends`: 5,028행(3.0 MB) 중 **3,859행(76.8%)**은 2026-10-07 v7.2.5 배포 이전에 쌓인 동결 `ENDED` 세션의 행으로, 현재 코드는 읽지도 쓰지도 않는다.
   - **조치:**
     - [ ] migration으로 `youtube_live_pending_ends`에서 종료 후보(`end_candidate_observation_id`, `next_end_check_at`)가 없는 `ENDED` 세션의 잔재 3,859행을 정리한다.
     - [ ] 작은 두 테이블(`youtube_collection_job_leases` 505행, `youtube_live_pending_ends` 1,169행)에 짧은 `lock_timeout`으로 `VACUUM (FULL, ANALYZE)`를 수행해 `repository_candidates_0144_02.sql`의 호출당 블록 읽기를 **112.6 → ~28 블록(-75%)**으로 줄인다.
     - [ ] `youtube_live_absence_slots`는 `coverage` 열에 `SET COMPRESSION lz4` 적용 후 점검 창에서 `VACUUM (FULL, ANALYZE)`를 수행해 **~275 MB**를 회수한다(무중단 우선 시 `REINDEX INDEX CONCURRENTLY`로 인덱스 ~28 MB 선회수 가능).

3. **`C1-3` 퇴역 테이블 5종(`viewer_sample` 3종 + 구 통계 2종) 완전 제거 (~151 MB 절감, 데이터 삭제 승인 필요)**
   - **근거:**
     - `youtube_live_viewer_samples`(150 MB, 1,089,579행), `youtube_live_viewer_sample_heads`(552 kB, 2,913행), `youtube_live_viewer_sample_evidence`(24 kB, 0행), `youtube_stream_stats`(160 kB, 944행), `youtube_channel_latest_stats`(40 kB, 125행).
     - 2026-09-25 이후 신규 수집이 없고(`source_observations`·`youtube_collection_targets` 내 `viewer_sample` 0건), 런타임 조회 코드가 전무하다.
   - **조치:**
     - [ ] `reconcileViewer` 소비자(`viewer_consumer.go`, `viewer_persist.go`, `internal/youtube/reconcile/viewer`, `repository_viewer_*.sql`)와 미사용 도메인 모델(`YouTubeLiveViewerSample`, `YouTubeStreamStats`)을 제거한다.
     - [ ] 새 migration에서 5개 테이블을 `DROP TABLE`하고 `viewer_sample` 계약 어휘를 정리한다.

4. **`C1-4` 퇴역 Projection 보존 기간 조정 선택지 (7일 → 1~2일 시 ~330~400 MB 추가 절감, 정책 선택)**
   - **근거:** `youtube_collection_targets`(209 MB) + `youtube_collection_target_reasons`(257 MB) = **466 MB**는 7일치 `RETIRED` 세대(1,647세대)다.
   - **조치:** `YOUTUBE_PLANE_RETENTION_PROJECTION_RETIRED_DAYS`를 7일에서 1~2일로 줄이면 정상 크기가 **~70~130 MB**로 수렴한다(7일 감사 보존이 필요하면 현행 유지).

### C2. TOAST 및 가변 길이 열 `lz4` 압축 표준화

- **근거:** 운영 DB의 `default_toast_compression`이 `pglz`이고, `source_observation_payloads.payload` 외의 모든 JSONB·텍스트·배열 열이 `pglz`를 쓰고 있다. PG 18에서 `lz4`는 `pglz` 대비 압축·해제 CPU 비용이 훨씬 낮고 압축률도 대등하거나 우수하다.
- **조치:**
  - [ ] migration에서 `ALTER DATABASE hololive SET default_toast_compression = 'lz4';`를 선언한다(271 선례와 동일하게 현재 DB 이름 동적 조회).
  - [ ] 주요 TOAST 대상 열(`youtube_live_reconciliation_heads.ignored_absence_scheduled_for`, `youtube_live_absence_slots.coverage`, `youtube_content_absence_slots.coverage`, `youtube_live_review_receipts.original_snapshot`, `youtube_live_review_receipt_facts.facts`, `alarm_dispatch_events.payload`, `alarm_upcoming_candidates.notification`, `bot_webhook_inbox.payload`, `bot_reply_outbox.payload`)에 `SET COMPRESSION lz4`를 명시한다.

### C3. `source_observation_applications`(1,789 MB) 및 `source_observations`(1,030 MB) 구조·표현 압축 (중기: **약 600 MB ~ 1.1 GB 절감**)

전체 DB의 **68.8%(2,819 MB)**를 차지하는 두 테이블의 쓰기 증폭과 행 폭을 줄인다.

1. **`C3-1` `source_observation_applications`의 `ON DELETE SET NULL` 이중 쓰기 제거 또는 부모 중복 열 분리 (정책 선택)**
   - **근거:**
     - `source_observations` 보존 삭제 때 FK `ON DELETE SET NULL`이 자식 행마다 225 B 튜플 전체를 다시 쓰는 비-HOT UPDATE(**누적 2,044만 건, HOT 0%, WAL 28 GB**)를 일으킨다.
     - 그 결과 전체 487만 행 중 **28.8%(~140만 행, Heap+인덱스 약 515 MB)**가 이미 원본이 삭제된 `observation_id IS NULL` 고아 행이며, 런타임 조회(`repository_community_window_ready_0086_86.sql`)는 `WHERE observation_id = $1`만 읽으므로 고아 행을 전혀 참조하지 않는다.
   - **선택지:**
     - **안 A (`ON DELETE CASCADE` 전환 — 권장):** 감사 행 수명을 원본(`source_observations`) 수명과 일치시킨다. `ON DELETE SET NULL` UPDATE 2,044만 건(WAL 28 GB)과 고아 전용 보존 삭제 함수·인덱스(`idx_source_observation_applications_orphaned_kind_applied_id`, **135 MB**) 및 고아 행(**~380 MB**)이 모두 사라져 **약 515 MB와 보존 삭제 I/O 절반 이상**을 즉시 절감한다. 또한 부모가 항상 존재하므로 부모 중복 5개 열(`provider`, `observation_kind`, `subject_key`, `evidence_sha256`, `effective_at` = 행당 121 B)도 제거할 수 있어 추가로 **~400 MB**를 더 줄일 수 있다(합계 **~900 MB 절감**).
     - **안 B (고아 3일 유예 유지 시 — 관측 헤더 분리):** 관측 1건당 다건(`schedule_snapshot` ~128행, `video/shorts` ~14행, `community` ~9행)으로 복제되는 부모 열 121 B를 관측 단위 헤더로 분리해 자식 행 폭을 225 B → ~95 B로 축소한다(**~450 MB 절감**).

2. **`C3-2` SHA-256 해시 열의 64자 hex `text`(65 B) → 32바이트 `bytea`(33 B) 전환 (~230 MB 절감)**
   - **근거:** `source_observation_payloads.payload_sha256`과 달리 `source_observations`(`scope_sha256`, `evidence_sha256`: 109만 행 × 130 B = 141 MB)와 `source_observation_applications`(`evidence_sha256`: 487만 행 × 65 B = 317 MB)는 64자 hex 텍스트를 저장한다. 32바이트 `bytea`로 바꾸면 행당 32~64 B가 줄어든다(`C3-1`에서 `applications.evidence_sha256` 열 자체를 제거하면 `source_observations` 등만 전환).

---

## 3. 단계별 실행 순서와 의존성

1. **1단계 — 코드·SQL 계획 최적화 및 즉시 회수 (스키마 파괴 없음):**
   - `S1`(`collection_target_observability.sql`), `S2`(`live_check_videos.sql`·검토 함수), `S3`(`extend_targets.sql`·reasons 캐시), `S4`(live head TOAST 적재 생략·absence slot 쿼리 분리), `S5`(collector lease acquire·bundle SQL 최적화).
   - `C1-1`(종료 세션 배열 비우기 코드 + migration), `C1-2`(`youtube_live_pending_ends` 동결 행 정리 migration), `C2`(`default_toast_compression = 'lz4'` 및 열 압축 지정 migration).
2. **2단계 — 물리 공간 회수 점검 (`stack-platform-ops` / `hololive-bot-ops` 승인 후 실행):**
   - `youtube_collection_job_leases`, `youtube_live_pending_ends`, `youtube_live_reconciliation_heads`, `youtube_live_absence_slots` 순서로 짧은 `lock_timeout`과 점검 창에서 `VACUUM (FULL, ANALYZE)` 실행(**약 390 MB 즉시 회수 + 후보 조회 75% 가속**).
3. **3단계 — 퇴역 테이블 제거 및 감사 테이블 구조 압축 (사용자 승인 항목):**
   - `C1-3`: 퇴역 `viewer_sample` 3개 테이블 + `youtube_stream_stats` + `youtube_channel_latest_stats` 코드·스키마 삭제(**151 MB 회수**).
   - `C3-1` / `C3-2`: 선택한 감사 보존 정책(안 A `ON DELETE CASCADE` + 중복 열 제거 권장)에 따라 `source_observation_applications`·`source_observations` 축소(**600 MB ~ 1.1 GB 회수**).

## 4. 검증 방법

- **SQL 실행 계획 회귀 시험 (`hololive-dbtest` / 각 패키지 통합 시험):**
  - `S1`·`S2`: 영수증 53건·종료 세션 1만 행 fixture에서 `force_custom_plan`·`force_generic_plan` 모두 `EXPLAIN (ANALYZE, BUFFERS)`로 읽은 블록 상한(`collection_target_observability.sql` 1,000 블록 이하, `live_check_videos.sql` 400 블록 이하)과 판정 결과 동등성을 단정한다.
  - `S3`: 600개 target에서 `extend_targets.sql`이 변경 1행일 때 읽는 블록 상한(100 블록 이하)을 단정한다.
  - `S4`: `PARTIAL` 관측 소비 시 `ignored_absence_scheduled_for` 미적재 및 결과 동등성을 기존 `live_state_scope_test.go`·`live_head_storage_test.go`로 검증한다.
  - `S5`: 4개 collector 동시 경쟁 시험과 candidate/acquire wire traffic 시험 통과를 확인한다.
- **게이트:**
  - `scripts/architecture/check-migration-manifest.sh`, `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`
  - `scripts/ci/local-ci.sh`, `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh --integration-tests-only`
  - `./build-all.sh --build-only --no-bump hololive-api hololive-alarm-worker youtube-collector`

## 5. 결정 및 승인이 필요한 사항

| 항목 | 선택지 / 승인 대상 | 영향 |
| --- | --- | --- |
| `C1-3` 퇴역 테이블 5종 DROP | `youtube_live_viewer_samples`(150 MB, 109만 행), `heads`, `evidence`, `youtube_stream_stats`, `youtube_channel_latest_stats` 삭제 여부 | 2026-09-25 이전 과거 시청자 표본·구 통계 행 영구 삭제(현재 읽는 코드 없음) |
| `C3-1` `source_observation_applications` 정책 | **안 A(권장):** `ON DELETE CASCADE` 전환 + 부모 중복 5열 제거(~900 MB 절감, 비-HOT UPDATE 2,044만 건 제거) vs **안 B:** 고아 3일 유예 유지 + 헤더 테이블 분리(~450 MB 절감) | 안 A 선택 시 원본 만료 이후의 3일 추가 고아 감사 유예가 사라지고 원본 수명과 일치함 |
| `C1-4` 퇴역 Projection 보존 기간 | 현행 **7일(466 MB)** 유지 vs **1~2일(70~130 MB)** 단축 | 과거 RETIRED 세대 target/reason 진단 조회 가능 기간 |
| 운영 물리 회수(`VACUUM FULL`) 및 배포 | `youtube_collection_job_leases`, `youtube_live_pending_ends`, `youtube_live_reconciliation_heads`, `youtube_live_absence_slots` 재작성 및 서비스 배포 | 테이블별 짧은 `ACCESS EXCLUSIVE` 잠금(큰 테이블은 점검 창 실행) |

## 6. 실행 결정 및 진행 기록

### 승인된 범위 (2026-10-14)

- 사용자 요청: 이 계획을 구현·검증·운영 적용까지 완결한다.
- `C1-3`: 퇴역 viewer 3종과 구 통계 2종의 코드·테이블 및 운영 데이터 삭제 승인.
- `C3-1`: **안 A**, 원본 만료 시 `ON DELETE CASCADE`로 감사 행을 즉시 삭제한다. 추가 고아 감사 유예는 두지 않으며 부모 중복 5열을 제거한다.
- `C1-4`: 퇴역 Projection 보존 기간을 **2일**로 단축한다. `CURRENT`/`STAGING` 및 활성 lease 보호 조건은 유지한다.
- 운영: 서비스 배포, migration, 계획의 `VACUUM FULL` 승인. Git 원격 발행은 포함하지 않는다.
- 복구: 기존 백업 중단 지침을 확인한 뒤 사용자가 **백업 없이 진행**을 명시적으로 선택했다. 새 DB 백업·복원 시험·정기 백업 재개는 하지 않는다. 기존 백업과 이전 이미지·배포 트리는 보존하지만, 삭제 데이터 복구나 구 바이너리만의 롤백은 보장할 수 없다.
- `C3-2`의 `ALTER COLUMN TYPE ... USING decode(...)`는 전체 heap/index를 다시 쓰며 `ACCESS EXCLUSIVE`를 유지한다. 변경된 스키마와 구 publisher/consumer의 동시 실행을 피하도록 검증 산출물 준비 후 점검 창에서 중지·migration·신규 프로세스 시작 순서를 지킨다.

### 실행 중 근거

- 초기 checkout에는 이 계획 파일만 미추적 상태였으며, 기존 추적 파일 변경은 없었다.
- 1차 구현: SQL 검토 판정, heartbeat, live 적재, collector lease, viewer 제거, 감사·해시 압축의 6개 소유자로 분담했다. migration manifest·schema golden·압축 정리 migration·통합 검증·운영 적용은 통합 소유자가 관리한다.
- 운영 읽기 전용 확인: `transaction_read_only=on`, PostgreSQL **18.6 / aarch64**, DB 크기 **4,296,636,095 bytes**. 중앙 앱·collector·worker는 healthy였고, 파일시스템 여유는 **33 GB**였다. 이 값은 변경 전 기준이며 최종 수용 결과가 아니다.
- 로컬 구현은 `S1~S5`, `C1-1~4`, `C2`, `C3-1~2`까지 반영했다. migration은 `304 → 305 → 306 → 309 → 307 → 308` 순서이며 manifest 총 170개다. 공개 해시는 hex를 유지하고 저장 열만 `bytea`로 전환한다.
- 2차 독립 검토 2명: 활성 canonical 세션과 ENDED head가 불일치할 때 이력을 지우는 문제, 최초 lease INSERT 충돌 대기 후 만료된 TTL을 반환하는 문제를 찾아 수정했다. PostgreSQL 재현은 각각 수정 전 `UPCOMING|0` → 수정 후 `UPCOMING|1`, 충돌 후 잔여 TTL `-0.155540s` → 초기화 후 `1.999962s`였다.
- 실제 `db-migrate` 명령의 빈 PostgreSQL 18.6 재생: **170 적용 / 0 생략**, 종료 코드 0. 전체 `hololive-dbtest` 및 schema golden 갱신 시험 통과(**46.456s**).
- 계획 fixture(ENDED 10,000·활성 215·영수증 53): custom/generic 모두 observability **312블록**, live-check **260블록**. 최초 SQL 수정은 11,533/1,131블록으로 상한을 넘었으며, 활성 원본·head·availability 입력 및 집합 함수 인수를 고정한 뒤 같은 상한 1,000/400을 통과했다.
- 별도 `psql` smoke(테스트 함수가 아닌 실제 SQL 파일 실행): custom/generic observability **315블록 / 6행**, live-check **263블록 / 163행**, 현재 일치 영수증 **52건**. 600 CURRENT + 48,000 RETIRED의 1행 heartbeat 변경은 양쪽 **62블록**. ExactSubject generic plan은 PK의 generation·subject·kind 조건으로 **1행 / 1 loop**였다.
- targetprojection·live reducer·collector joblease·publisher 패키지 시험 통과. source consumer 통합 중 오래된 보존 fixture와 잘못된 `effective_at` 열 조회를 수정했고 해당 두 시험은 통과했다. 전체 CI·통합 전용 게이트·이미지 빌드와 운영 수용은 아직 남아 있다.
- 배포 진입점의 topology·Compose 서비스·AP version·native deploy·systemd 검사 통과. 삭제/추가된 lease SQL에 맞춰 AP 전송 manifest도 갱신했다.
- 정적 설정은 중앙 master와 mirror를 일치시켰으며 파일 mode/owner **0640 0:1002**를 확인했다. Projection 2일 및 제거된 두 retention 키가 반영되었지만 프로세스 재기동은 아직 하지 않았다.


Fallback delta: 새 재시도·대체 경로를 추가하지 않는다.
