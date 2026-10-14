# Hololive SQL 최적화 및 압축 플랜 (2026-10-14)

**상태: 운영 성능 목표 보완 진행 중.** 1차 구현·공간 회수·운영 배포는 완료했지만 S1/S2의 운영 성능 목표는 미완료다. 현재 운영 릴리스는 `10.0.0`, source revision은 `97206d67787f9f8643c7937784eef6bb900bab85`다. 1차 수용 결과와 후속 작업을 구분해 기록한다.

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
  - [x] `active_live_videos`와 활성 product/head/availability 입력을 `MATERIALIZED`로 고정하고, `EXISTS`로 채널 membership을 검사한다. 과거 종료 세션에서 시작하는 채널 조인을 제거했다.
  - [x] `live_reviews`를 S2의 집합형 판정으로 바꿔 영상별 상관 함수 호출과 영수증 표 반복 조회를 제거했다.
- **기대 효과:** 호출당 블록 읽기 **10,895 → ~450 블록(-95.9%)**, 실행 시간 **27~77 ms → ~2~3 ms**.

### S2. `live_check_videos.sql` 및 `youtube_live_review_current_receipt` — Projection Guard 내부 5초 주기 비용 축소 (P1)

- **실측 근거:** 5초마다 `youtube_collection_projection_guard` 잠금 안에서 실행된다. 누적 96,262회, 평균 **8.72 ms**, 호출당 **1,746 블록**. `EXPLAIN (ANALYZE, BUFFERS)` 실측(`8.46 ms`, `1,387` 블록) 중 **`1,078` 블록(77.7%)과 `5.87 ms`(69.4%)**가 `reviewed_videos` CTE(`SubPlan 5`, `loops=53`)에서 발생한다.
  - `youtube_live_review_current_receipt(p_video_id)`가 `to_jsonb(session)` 등으로 전체 스냅샷 JSONB를 만든 뒤(19개 `timestamptz`를 문자열로 직렬화), `youtube_live_review_semantic_facts(snapshot)`에서 다시 19개 문자열을 `::timestamptz`로 파싱해 `extract(epoch FROM ...)`를 구하는 이중 변환을 매 호출 53번 수행한다.
  - `youtube_live_review_receipt_facts`가 53행(7페이지)이라 인덱스를 쓰지 않고 매번 7페이지 Seq Scan × 53회 = `371` 블록을 읽는다.
- **변경:**
  - [x] 현재 의미 사실은 SQL `timestamptz` 열에서 직접 epoch를 구성하고, 영수증 집합을 한 번 비교한다. NULL·시간대·소수초·최신 일치 영수증·재검토 조건의 동등성 시험을 통과했다.
- **기대 효과:** 호출당 블록 읽기 **1,387 → ~280 블록(-80%)**, 실행 시간 **8.5 ms → ~1.5 ms**, Projection Guard 잠금 보유 시간 단축.

### S3. `extend_targets.sql` 및 `replaceReasonsIfChanged` — Heartbeat 시 587회 B-Tree 탐색 및 Reasons 재조회 제거 (P1)

- **실측 근거:**
  - `extend_targets.sql` (`3047231863221814013`): 164,562회 호출, 평균 변경 행은 **1.05행**인데 호출당 **1,998.2 블록(~16 MB)**을 읽는다. 플래너가 `unnest($2, $3, $4)`를 10행으로 추정해 `youtube_collection_targets_pkey`를 587번 Nested Loop Index Scan(`587 × 3.4 = 1,998` 블록)하기 때문이다.
  - `load_reasons.sql` (`-6846211758071134799`): 5초마다 target 해시가 동일한 `extendUnchangedGeneration` 안에서도 현재 세대의 `youtube_collection_target_reasons` 587행을 매번 읽어 Go로 전송·비교한다.
- **변경:**
  - [x] `extend_targets.sql`은 현재 세대 범위를 먼저 materialize하고 입력과 비교한 뒤 실제 `not_before` 변경 행만 `ctid`로 갱신한다.
  - [x] `Refresher`는 커밋된 generation·header MVCC 버전·정규화 reasons 해시를 기억한다. 같은 세대의 외부 writer, rollback·commit 실패·panic은 캐시를 무효화한다.
- **기대 효과:** `extend_targets.sql` 호출당 블록 읽기 **1,998 → ~20 블록(-99%)**, 무변경 heartbeat의 `load_reasons.sql` 조회 0회.

### S4. Live 관측 소비 경로 — `PARTIAL` 관측의 불필요한 TOAST 배열 적재 및 Absence GIN 스캔 제거 (P1)

- **실측 근거:**
  - 운영 `live_snapshot` 관측의 **94.2%(`653,021 / 692,985`)는 `PARTIAL`**이다.
  - 신규 `UPCOMING`이나 첫 `LIVE` positive가 없는 일반 `PARTIAL` 관측에서는 reducer가 부재(`applyAbsenceToKnownSession`)를 전혀 평가하지 않는데도, `repository_live_heads_0046_46.sql`(`8119211535387059239`, 평균 **3.29 ms**, 15초 구간 **5.46 ms**, 런타임 총 실행 시간 1위)이 채널의 모든 `UPCOMING` 세션(196행, 원소 218만 개 = 압축 11.7 MB)의 `ignored_absence_scheduled_for` 배열을 매번 TOAST에서 읽는다.
  - `lifecycle_origin IN ('metadata_only', 'legacy_unknown')`이고 positive clock이 없는 `UPCOMING` 55행(원소 57.7만 개, 3.1 MB)도 `applyAbsenceToKnownSession` 초입에서 바로 반환되어 배열을 쓰지 않지만 매번 적재된다.
  - `loadLiveAbsenceSlots`는 `len(channelIDs) == 0`일 때도 단일 `OR` 쿼리(`repository_live_absence_slots.sql`)에 빈 배열 `$1 = '{}'`을 넘겨 generic plan에서 `BitmapOr`(GIN 인덱스 포함)를 수행한다.
- **변경:**
  - [x] 먼저 scalar head를 잠근 뒤 scoped negative·첫 positive replay 필요 여부를 판단한다. 일반 `PARTIAL`은 ignored 배열과 absence slot을 모두 읽지 않는다.
  - [x] positive clock이 없는 `metadata_only`·`legacy_unknown`은 배열을 생략한다. 단, 이번 positive로 observed가 되어 replay에 참여하는 세션의 기존 이력은 정확도를 위해 읽는다.
  - [x] replay 채널이 없으면 `scheduled_for`와 현재 coverage만 검사하는 전용 SQL을 사용하며 custom/generic plan 시험을 통과했다.
- **기대 효과:** `repository_live_heads_0046_46.sql` 평균 시간 **3.29~5.46 ms → ~0.2 ms(-94%)**, 일반 `PARTIAL` 관측의 TOAST 배열 읽기 및 absence slot 조회 0회.

### S5. Collector Lease 획득(`acquireTx`) 및 후보 조회 — 무의미한 `INSERT` 제거와 WAN 왕복 축소 (P2)

- **실측 근거:**
  - `sqlLeaseInsert` (`-4926252849196727417`): 누적 **2,783.6만 회** 호출 중 실제 삽입은 **3,012건(0.011%)**이고 **99.989%가 no-op**이다(15초 구간 98회 호출, `254.51 ms`, 삽입 0건).
  - `acquireTx`가 `youtube_collection_projection_guard`를 `FOR SHARE`로 잡은 채 4번의 순차 SQL 왕복(`lockProjectionGuard` → `verifyAcquireTargets` → `insertAcquireJobRow` → `acquireLeaseProof`)을 수행해, 원격 AP(`osaka`, `osaka2`)가 WAN RTT 동안 공유 잠금을 보유한다. 그 영향으로 중앙 API의 `SELECT guard_key ... FOR UPDATE`(`-7931210353652924045`)가 3블록만 읽으면서도 평균 **19.08 ms**(15초 구간 **65.68 ms**) 대기한다.
  - `repository_target_bundle_0144_04.sql`(`6395284297819869485`)은 `cadence`와 `membership` 두 CTE가 같은 `youtube_collection_targets`를 두 번 읽고 `(NOT $3::boolean OR subject_key = $4)`로 인해 generic plan에서 `subject_key` PK 조건을 활용하지 못한다.
- **변경:**
  - [x] 기존 job 획득은 guard + 통합 admission/acquire SQL 두 문장이다. INSERT는 실제 누락 행에만 시도하며, 최초 생성 성공에만 추가 UPDATE로 충돌 대기 이후의 slot·TTL을 확정한다.
  - [x] cadence·membership을 `FILTER` 집계 한 번으로 합치고 ExactSubject SQL을 분리했다. 4개 collector 경쟁·wire traffic·기존 행 INSERT 금지·최초 생성 rollback 경합 시험을 통과했다.
- **기대 효과:** 무의미한 `INSERT` 제거, 원격 collector의 `projection_guard` 잠금 보유 시간 50% 이상 단축.

---

## 2. 데이터 압축 및 용량 회수 플랜 (C1 ~ C3)

### C1. 무손실 공간 회수 및 퇴역 잔재 정리 (단기: **약 540 ~ 860 MB 절감**, 4.10 GB → ~3.25 GB)

1. **`C1-1` `youtube_live_reconciliation_heads` 종료(`ENDED`) 세션 배열 비우기 및 `lz4` 전환 (~104 MB 절감)**
   - **근거:** `youtube_live_reconciliation_heads`(114 MB 중 TOAST **113 MB**)에서 이미 종료된 `ENDED` 2,655행이 **1,459.5만 개 원소(압축 76 MB)**를 가지고 있다. `ENDED` 세션의 배열은 어떤 런타임 경로도 읽지 않고 검토 영수증 대상(`UPCOMING` 전용)도 아니다.
   - **조치:**
     - [x] 저장되는 `ENDED` head의 배열을 비우며, 활성 세션의 미적재 이력은 유지한다.
     - [x] 305가 `lz4`를 지정하고 종료 배열을 keyset 배치 정리한다. canonical `LIVE`/`UPCOMING`과 ENDED head가 불일치하면 canonical 상태를 우선하여 이력을 보존한다.
     - [x] 점검 창에서 `VACUUM (FULL, ANALYZE)`를 실행했다. 전체 relation 크기는 119,660,544 → 13,623,296 bytes다.

2. **`C1-2` `youtube_live_absence_slots`·`youtube_collection_job_leases`·`youtube_live_pending_ends` 블로트 회수 (~285 MB 절감 + 후보 조회 가속)**
   - **근거:**
     - `youtube_live_absence_slots`: 317 MB(Heap 282 MB + Index 35 MB) 중 실제 데이터는 **32.95 MB**이고 **248.57 MB(88.2%)가 빈 공간**이다. `pkey`·`scheduled_for` 인덱스도 87.6%가 삭제된 페이지다.
     - `youtube_collection_job_leases`: 505행(186.6 kB)인데 Heap이 856 kB(107페이지, 빈 공간 69.8%)이고 `idx_youtube_collection_job_projection_generation`이 3,064 kB(삭제 페이지 87.1%)다. 이 Heap 블로트 때문에 초당 25회 실행되는 `repository_candidates_0144_02.sql`이 매번 107페이지를 Seq Scan한다.
     - `youtube_live_pending_ends`: 5,028행(3.0 MB) 중 **3,859행(76.8%)**은 2026-10-07 v7.2.5 배포 이전에 쌓인 동결 `ENDED` 세션의 행으로, 현재 코드는 읽지도 쓰지도 않는다.
   - **조치:**
     - [x] 305가 종료 후보가 없는 canonical `ENDED`의 동결 pending 잔재를 정리했다. 적용 후 해당 잔재는 0건이다.
     - [x] job lease와 pending-end에 `lock_timeout=3s`로 `VACUUM (FULL, ANALYZE)`를 실행했다. 실제 회수량은 마지막 표에 기록한다.
     - [x] absence coverage에 `lz4`를 지정하고 점검 창의 `VACUUM (FULL, ANALYZE)`를 완료했다. 전체 relation 크기는 332,726,272 → 30,228,480 bytes다.

3. **`C1-3` 퇴역 테이블 5종(`viewer_sample` 3종 + 구 통계 2종) 완전 제거 (~151 MB 절감, 데이터 삭제 승인 필요)**
   - **근거:**
     - `youtube_live_viewer_samples`(150 MB, 1,089,579행), `youtube_live_viewer_sample_heads`(552 kB, 2,913행), `youtube_live_viewer_sample_evidence`(24 kB, 0행), `youtube_stream_stats`(160 kB, 944행), `youtube_channel_latest_stats`(40 kB, 125행).
     - 2026-09-25 이후 신규 수집이 없고(`source_observations`·`youtube_collection_targets` 내 `viewer_sample` 0건), 런타임 조회 코드가 전무하다.
   - **조치:**
     - [x] viewer 소비자·persist·reducer·SQL·미사용 도메인 모델 및 현재 계약 어휘를 제거했다.
     - [x] 306이 5개 테이블과 퇴역 체크포인트·offset을 제거했다. 활성 관측·재처리·projection·lease 보호 검사를 유지하며 적용 후 5개 테이블은 모두 없다.

4. **`C1-4` 퇴역 Projection 보존 기간 조정 선택지 (7일 → 1~2일 시 ~330~400 MB 추가 절감, 정책 선택)**
   - **근거:** `youtube_collection_targets`(209 MB) + `youtube_collection_target_reasons`(257 MB) = **466 MB**는 7일치 `RETIRED` 세대(1,647세대)다.
   - **완료:** 승인된 **2일**을 코드·Compose·정적 master/mirror·실행 중 API에 반영했다. 기존 제한된 보존 함수로 1,201세대와 target/reason 각각 719,714행, 퇴역 lease 55행을 배치 삭제했다. 후속 읽기 전용 확인에서 2일 초과 RETIRED는 0건이었다.

### C2. TOAST 및 가변 길이 열 `lz4` 압축 표준화

- **근거:** 운영 DB의 `default_toast_compression`이 `pglz`이고, `source_observation_payloads.payload` 외의 모든 JSONB·텍스트·배열 열이 `pglz`를 쓰고 있다. PG 18에서 `lz4`는 `pglz` 대비 압축·해제 CPU 비용이 훨씬 낮고 압축률도 대등하거나 우수하다.
- **조치:**
  - [x] 305가 현재 DB 이름을 동적으로 조회하여 `default_toast_compression='lz4'`를 설정했다. 새 운영 세션에서 `lz4`를 확인했다.
  - [x] 계획의 주요 TOAST 9열에 명시한 `lz4`를 운영 카탈로그 `attcompression='l'`로 확인했다. 설정 변경만으로 기존 TOAST 값 전체가 재압축되는 것은 아니다.

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
   - **완료:** 307로 안 A를 적용했고 application에는 `id, observation_id, entity_kind, entity_key, decision, applied_at`만 남는다. `observation_id`는 NOT NULL·CASCADE이며 고아 보존 함수·인덱스·유예 설정을 제거했다. 308은 source 관측·live/content absence의 해시 6열을 `bytea`로 바꾸되 공개 hex 표현과 dedup·replay 계약은 유지한다.

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
- targetprojection·live reducer·collector joblease·publisher 패키지 시험 통과. source consumer 통합 중 오래된 보존 fixture와 잘못된 `effective_at` 열 조회를 수정했고 해당 두 시험은 통과했다. 이후 전체 CI·통합 전용 게이트·이미지 빌드·운영 수용까지 완료했다.
- 배포 진입점의 topology·Compose 서비스·AP version·native deploy·systemd 검사 통과. 삭제/추가된 lease SQL에 맞춰 AP 전송 manifest도 갱신했다.
- 정적 설정은 중앙 master와 mirror가 일치하며 파일 mode/owner는 **0640 0:1002**다. 재기동한 API 환경에서도 Projection **2일**, 폐기한 audit-grace/viewer 설정의 부재를 확인했다.

## 7. 최종 검증 및 운영 수용 결과

### 검증

- 구현 6명 → 독립 검토 2명 → 통합 lint 수정 3명의 세 wave를 실행했다. 빌드·시험·운영 적용은 통합 소유자가 실행했다.
- `scripts/architecture/check-migration-manifest.sh`, schema golden 갱신 및 전체 `hololive-dbtest`, `scripts/ci/local-ci.sh`의 architecture·vet·staticcheck·golangci-lint·NilAway·race·non-race 검사를 통과했다. lint 억제나 검사 상한 변경은 없으며, 발견한 SQL 문자열 wiring-only 시험은 제거하고 보존 동작 시험을 유지했다.
- 마지막 wiring-only 시험 제거 후 `go test -count=1 ./hololive/hololive-api/internal/planes/youtube/targetprojection`도 통과했다(**6.373s**).
- workspace `check-stack-db-access-policy.sh`는 통과했다. `check-stack-retry-contract.sh`는 `Iris/native/**`를 스캔하므로 stack 지침의 해당 경로 스캔 금지에 따라 실행하지 않았다. 이 교차 저장소 게이트 통과를 주장하지 않으며, 변경된 YouTube 경로의 보존·dedup·replay·경쟁 동작은 위 패키지/통합 시험으로 검증했다.
- `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh --integration-tests-only` 통과. 실제 임시 PostgreSQL·Valkey의 dispatchoutbox, summarizer, YouTube dispatch 및 joblease 시험을 실행했다.
- `./build-all.sh --build-only --no-bump hololive-api hololive-alarm-worker youtube-collector` 통과. API·worker·collector·issuer ARM64 이미지의 full revision과 version을 확인했다. native a/d는 kapu에서 amd64/v3로 빌드·검증했다. 원격 빌드는 없다.
- 배포 전후 `pg-hotpath-explain-snapshot.sh --stats-window-seconds 60`을 통과했다. 이 게이트의 두 claim `EXPLAIN ANALYZE`는 실제 UPDATE 뒤 ROLLBACK하므로 별도 사용자 승인을 받았다. 일반 카탈로그·행·계획 조회는 계속 `transaction_read_only=on`이다.

### 적용과 복구 경계

- 점검 시작 **2026-10-14 03:07:03 UTC**. API·worker·collector a/b/c/d와 issuer를 중지하고, 승인된 `db-migrate`가 **6 적용 / 164 생략 / 총 170**으로 완료됐다. 새 6개 migration checksum은 검토된 로컬 원본과 모두 일치한다.
- 기존 keyset 보존 함수로 2일 정책을 적용하고 다음 7개 relation에 `VACUUM (FULL, ANALYZE)`를 완료했다: job leases, pending ends, reconciliation heads, live absence slots, source applications, target reasons, targets.
- API·worker는 **03:16 UTC**, native a/d는 **03:17 UTC**, Compose c/b는 **03:19~03:20 UTC**에 새 revision으로 기동했다. 마지막 b의 `StartedAt`은 **03:20:11 UTC**다.
- 중앙 API의 bot/admin health·인증된 LLM readiness, worker health, a/b/c/d의 READY·first_success·PROCESSED handoff를 확인했다. Osaka·Osaka2·Seoul의 `ap-completion-check.sh`가 모두 통과했고 중앙/Seoul 컨테이너는 healthy·RestartCount 0이었다. 중앙 새 로그에 ERROR/FATAL/PANIC 또는 연결 실패가 없고 PostgreSQL·Valkey 연결을 확인했다.
- 새 DB 백업은 생성하지 않았다. 이전 image tag `rollback-sql-compression-20261014T025442Z`, 중앙 `/opt/hololive-bot/compose/sql-compression-20261014T025442Z/previous/`, Seoul `~/hololive-bot/backups/sql-compression-20261014T025442Z/previous/`, native 이전 immutable release와 `previous` 링크를 보존했다. **이 자료만으로 삭제 데이터나 구 schema를 복원할 수 없으며 image-only rollback은 지원하지 않는다.**
- Git 원격 push/tag/Release는 수행하지 않았다. 배포 revision은 위 source SHA이며 마지막 문서·시험 정리 commit과 구분한다.

### 물리 공간 회수

단위는 bytes다. 점검 직전과 물리 회수 직후를 비교하며, 재기동 후 정상 쓰기로 DB가 다시 조금 증가한 값은 별도다.

| 대상 | 적용 전 | 회수 직후 |
| --- | ---: | ---: |
| 전체 DB | 4,299,347,647 | 1,881,298,623 |
| source applications | 1,875,853,312 | 714,170,368 |
| source observations | 1,080,565,760 | 784,269,312 |
| live absence slots | 332,726,272 | 30,228,480 |
| target reasons | 271,163,392 | 65,478,656 |
| targets | 220,143,616 | 51,355,648 |
| reconciliation heads | 119,660,544 | 13,623,296 |
| job leases | 4,276,224 | 327,680 |
| pending ends | 3,096,576 | 294,912 |

전체 회수는 **2,418,049,024 bytes = 2.418 GB, 56.24%**다. 재기동 후 후속 조회는 DB **1,885,116,095 bytes**, CURRENT 1세대, RETIRED 443세대, 2일 초과 RETIRED 0건이었다. 종료 배열·동결 pending 잔재와 퇴역 5개 테이블도 0건이다.

### 성능 실측과 남는 한계

| 운영 SQL | custom 블록 | generic 블록 | custom 실행 ms | generic 실행 ms |
| --- | ---: | ---: | ---: | ---: |
| collection observability | 1,075 | 1,075 | 15.437 | 14.650 |
| live-check videos | 776 | 776 | 7.009 | 7.452 |

- 계획의 명시적 fixture 상한 **1,000 / 400 / 100블록**은 모두 통과했다(S1 312, S2 260, S3 62). 운영 데이터의 단발 실측은 최초 관측 대비 S1 블록 **90.13%**, S2 블록 **44.05%** 감소했지만, 수행 전 추정인 **450/280블록 및 2~3/1.5ms에는 도달하지 않았다**. 운영과 fixture의 행 배치·분포·동시 부하가 달라 둘을 같은 성능 결과로 취급하지 않는다.
- 일반 PARTIAL의 배열/absence 조회 생략, reasons cache 무조회 및 collector 왕복 감소는 실제 DB 소비·경쟁·wire 시험으로 검증했다. S4 운영 평균 0.2ms, S5 WAN 잠금 50% 감소는 별도 통제 전후 실측이 아니므로 달성으로 주장하지 않는다.
- 기존 TOAST 값 전체 재압축, 장기 WAL/공간 증가율, 외부 provider의 모든 채널 최신성은 이번 수용 시험의 증명 범위가 아니다. 수집·보존 보호 조건과 새 재시도/호환 fallback 없음은 유지한다.

## 8. 운영 성능 목표 보완

- 사용자가 미달 원인 설명 후 목표 달성을 위한 추가 작업을 요청했다. 기존 migration·배포 승인과 백업 중단·원격 Git 미발행 경계를 유지한다.
- 완료 기준은 fixture 통과가 아니라 **운영 custom/generic 모두 S1 450블록·3ms, S2 280블록·1.5ms**다. 같은 roster·snapshot의 결과 동등성과 반복 측정 평균을 확인하고 변동 범위도 공개한다. 여기서 블록은 shared hit+read 합이다.
- 원본 재조회뿐 아니라 좁은 fixture가 가린 운영 행 배치와 CTE 행 수 과소 추정도 검증한다. 관측 시 운영 원본은 약 10,019행, head 4,471행, availability 757행, pending 1,169행, 검토 사실 53행이었다. 임의의 운영 데이터 복제·백업 없이 cardinality/폭 집계로 합성 fixture를 보강한다.
- 1차 후속 후보는 공유 scalar 원본과 순수 의미 사실 생성 함수(310)를 사용한다. 운영 읽기 전용 SELECT probe에서 S1 custom/generic **347/339블록**, S2 **254/254블록**으로 블록 목표를 통과했지만 실행시간은 **20.009/18.732ms**, **5.364/6.011ms**여서 미완료다.
- 해당 계획에서 S1 availability CTE가 209회, 검토 결과 CTE가 209회 반복됐고 S2 검토 결과 anti join도 91회 반복됐다. 다음 수정은 이 CTE 반복과 target mapping의 교차 비교를 제거한다. planner GUC 강제·상한 완화·새 mutable cache로 우회하지 않는다.
- 원본 재사용·비상관 membership·의미 사실 1회 생성 후보도 운영 S1 약 8~10ms, S2 약 4ms로 시간 목표에 미달했다. 엄격한 typed JSON 해석을 매 조회 수행하는 실험은 약 19/14ms로 더 느려 채택하지 않았다.
- 310 후보는 불변 `facts`를 정확한 왕복 검사로 typed generated 값에 투영한다. 모든 숫자는 numeric이며, 원본 session/head를 저장하는 가변 캐시는 없다. 독립 검토에서 기존 동명 타입의 필드 순서 충돌을 찾았고 정확한 타입 그래프·생성 열 정의 검증으로 수정했다.
- 실제 로컬 `db-migrate`는 PostgreSQL 18.6에 **171 적용 / 0 생략**으로 성공했다. 실제 runtime 역할의 SQL 조회, 310 재적용·타입/열 충돌 거부·전체 필드/3개 시간대 및 malformed JSON·무한대·극단 timestamp/정수 동등성 시험을 통과했다.
- 강화된 대표 fixture에서 기존 SQL은 S1 **1,099블록 > 450**으로 실패했다. 새 후보는 S1 **286블록**, S2는 head 접근 개선으로 **280블록**을 관측했다. 이 수치만으로 운영 시간 목표를 달성했다고 판정하지 않는다.
- 전체 runtime 시험에서 기존 30,000 활성 세션 사례의 1초 제한과 영수증 0건의 pending 무조회가 실패했다. 별도 실행계획은 두 UNION ALL 원본의 통계 손실로 30,000행을 4,503,000행으로 추정하고, 총 2,320ms 중 JIT에 1,981ms를 쓰는 원인을 보였다. unique 원본을 먼저 조인하고 완성된 프레임을 UNION하도록 바꾸고 빈 영수증 guard를 추가했다. 이후 실제 SQL은 **234.906ms, JIT 없음**이었고 전체 runtime 시험을 **52.818s**에 통과했다. JIT 설정·시험 제한은 바꾸지 않았다.
- 독립 의미 검토·운영 migration 검토 모두 남은 구체적 결함이 없음을 확인했다. 두 검토자는 시험을 실행하지 않았고, 검증 실행은 통합 소유자가 담당했다. 전체 dbtest도 **52.818s**에 통과했다. CI가 지적한 wildcard SQL·새 시험의 유지보수성·shadow/whitespace는 억제 없이 수정했다.
- 배포 후보는 API와 310만 변경한다. worker·collector a/b/c/d는 현재 revision을 유지한다. 310은 원본·facts를 변경/삭제하지 않아 기존 API SQL도 작동하며, 이전 API 이미지와 배포 파일을 보존한다. 운영 성능 수용 전에는 목표 달성으로 표시하지 않는다.

Fallback delta: 새 재시도·대체 경로를 추가하지 않는다.
