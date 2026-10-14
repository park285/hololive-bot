# Hololive SQL 최적화 및 압축 플랜 (2026-10-14)

**상태: 구현·검증·운영 적용 완료.** S1~S5·C1~C3를 적용했고, 추가 승인된 동기 읽기 모델로 S1/S2의 운영 custom/generic 블록·평균 시간 목표도 모두 충족했다. 최종 API 배포 revision은 `98b579e5c7a4910c440a79c20295f441f2de74ff`(10.1.0, migration 313)다. 최종 수용 근거와 변동 범위는 §13, 공간 회수는 §7에 기록한다. 이전 미달 결과와 별도 작업의 배포 이력은 아래에 구분해 보존한다.

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
  - [x] migration 312의 동기 `youtube_live_runtime_state`를 직접 읽어 과거 종료 세션 재조회와 상관 영수증 반복 조회를 제거했다. 원본·영수증 변경과 같은 트랜잭션에서 현재 일치 검토 시각을 갱신한다.
  - [x] migration 313의 CURRENT/STAGING target bundle로 구조 집계를 쓰기 시 확정한다. 세대 만료·lease·현재 시각·신선도 조건은 조회 시 평가한다.
- **기대 효과:** 호출당 블록 읽기 **10,895 → ~450 블록(-95.9%)**, 실행 시간 **27~77 ms → ~2~3 ms**.

### S2. `live_check_videos.sql` 및 `youtube_live_review_current_receipt` — Projection Guard 내부 5초 주기 비용 축소 (P1)

- **실측 근거:** 5초마다 `youtube_collection_projection_guard` 잠금 안에서 실행된다. 누적 96,262회, 평균 **8.72 ms**, 호출당 **1,746 블록**. `EXPLAIN (ANALYZE, BUFFERS)` 실측(`8.46 ms`, `1,387` 블록) 중 **`1,078` 블록(77.7%)과 `5.87 ms`(69.4%)**가 `reviewed_videos` CTE(`SubPlan 5`, `loops=53`)에서 발생한다.
  - `youtube_live_review_current_receipt(p_video_id)`가 `to_jsonb(session)` 등으로 전체 스냅샷 JSONB를 만든 뒤(19개 `timestamptz`를 문자열로 직렬화), `youtube_live_review_semantic_facts(snapshot)`에서 다시 19개 문자열을 `::timestamptz`로 파싱해 `extract(epoch FROM ...)`를 구하는 이중 변환을 매 호출 53번 수행한다.
  - `youtube_live_review_receipt_facts`가 53행(7페이지)이라 인덱스를 쓰지 않고 매번 7페이지 Seq Scan × 53회 = `371` 블록을 읽는다.
- **변경:**
  - [x] 310의 typed 영수증 사실과 현재 SQL scalar 값의 의미 비교를 312 동기 쓰기로 옮기고 live-check는 좁은 상태를 직접 읽는다. NULL·시간대·소수초·최신 일치 영수증·재검토 조건의 동등성 시험을 통과했다.
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
  - `repository_live_heads_0046_46.sql`(`8119211535387059239`, 평균 **3.29 ms**, 15초 구간 **5.46 ms**, 런타임 총 실행 시간 1위)이 채널의 모든 `UPCOMING` 세션(196행, 원소 218만 개 = 압축 11.7 MB)의 `ignored_absence_scheduled_for` 배열을 매번 TOAST에서 읽었다. 일반 `PARTIAL`은 부재를 전혀 평가하지 않는다는 최초 전제는 §14에서 반증됐다. 다른 kind의 지연된 positive가 중간에 수명을 승격하면 저장된 현재 slot을 재평가해야 한다.
  - `lifecycle_origin IN ('metadata_only', 'legacy_unknown')`이고 positive clock이 없는 `UPCOMING` 55행(원소 57.7만 개, 3.1 MB)도 `applyAbsenceToKnownSession` 초입에서 바로 반환되어 배열을 쓰지 않지만 매번 적재된다.
  - `loadLiveAbsenceSlots`는 `len(channelIDs) == 0`일 때도 단일 `OR` 쿼리(`repository_live_absence_slots.sql`)에 빈 배열 `$1 = '{}'`을 넘겨 generic plan에서 `BitmapOr`(GIN 인덱스 포함)를 수행한다.
- **변경:**
  - [x] 먼저 scalar head를 잠근 뒤 scoped negative·첫 positive replay 필요 여부를 판단한다. 일반 `PARTIAL`도 저장된 현재 slot을 시간 인덱스로 조회하며, slot이 없으면 ignored 배열을 읽지 않는다. slot이 있으면 현재 payload에 나타나지 않은 재평가 대상의 이력만 읽는다.
  - [x] positive clock이 없는 `metadata_only`·`legacy_unknown`은 배열을 생략한다. 단, 이번 positive로 observed가 되어 replay에 참여하는 세션의 기존 이력은 정확도를 위해 읽는다.
  - [x] replay 채널이 없으면 `scheduled_for`와 현재 coverage만 검사하는 전용 SQL을 사용하며 custom/generic plan 시험을 통과했다.
- **기대 효과와 경계:** `repository_live_heads_0046_46.sql` 평균 **~0.2 ms** 목표는 운영 미검증이다. 일반 `PARTIAL`의 과거 이력 GIN 스캔과 불필요한 TOAST 적재는 피하되, 정확성을 위해 현재 slot의 한정 조회를 유지한다. 모든 일반 `PARTIAL`의 absence 조회가 0회라는 보장은 폐기한다.

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

## 7. 1차 검증 및 운영 수용 결과

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
- `40127146d17ef0d6a5c24bc1f5b8a4c94c37dcfb`의 전체 local CI·NilAway·race/non-race와 API ARM64 빌드를 통과했다. 통합 전용 게이트도 통과했다. 승인된 운영 `db-migrate`는 **1 적용 / 170 생략 / 총 171**로 310을 적용했고 checksum `439a086a0b98a1c3f35cf84cf6afae24aacedbeb7676b1f8260b4bae1c0830de`가 일치한다. 원본 사실 53건 모두 typed 값의 JSON 왕복과 일치했다. **310은 이 시점부터 수정 금지**다.
- 310 이후 후보 SQL의 운영 실행계획은 여전히 약 7.1/3.0ms였다. API 활성화 전에 읽기 전용 후보 비교를 계속했고, S1은 active 원본 FULL JOIN과 누락 상대편 batch, S2는 좁은 typed frame과 `bool_or` 집계로 정리했다. 두 소비자는 영수증 세대를 한 번 조인하며 S1은 최신 **일치** 시각을 선택한다.
- 선택한 후속 후보는 운영에서 약 **S1 6.2ms / 372블록, S2 2.3ms / 259블록**이었다. 같은 statement/snapshot에서 기존·신규 결과의 양방향 차이는 각각 **0행**이다. 전체 runtime 시험 **34.540s** 및 두 독립 검토를 통과했다. full-aux scan·숫자 quotient bucket·window·과도한 inlining 실험은 이득이 없거나 더 느려 채택하지 않았다.
- 노드 계측을 끈 비교와 EXPLAIN 없는 40회 prepared SELECT도 별도 수행했다. 후자의 한 표본은 S1 약 4.2~4.9ms, S2 약 1.6~1.8ms였지만 **이를 기존 EXPLAIN 수용 기준과 바꿔 목표 달성으로 표시하지 않는다.** 블록 목표와 시간 목표를 분리해 후자의 미완료를 유지한다.
- 최종 API 후보 `8dd380cfc28962d364e26ddd9715d016c8d5110d`도 전체 local CI·NilAway·race/non-race와 ARM64 빌드를 통과했다. **2026-10-14 06:26:52 UTC**에 API만 새 이미지로 기동했고, bot/admin health·인증된 LLM readiness·RestartCount 0을 확인했다. 새 로그의 ERROR/FATAL/PANIC은 0건이며 PostgreSQL·Valkey·Iris 연결과 기동 후 관측 **156건 PROCESSED**를 확인했다. worker·collector c의 이미지와 기동 시각은 그대로다.
- 적용 명령에서 migration one-shot이 shell script의 상속 stdin을 소비하여 뒤의 API `up`이 실행되지 않은 것을 revision 검증이 잡았다. migration은 이미 성공했으므로 반복하지 않고, stdin을 닫은 별도 `up -d --no-build --no-deps --wait`를 실행해 위 새 revision을 확인했다. 마지막 이미지의 migration 재검증 결과는 **0 적용 / 171 생략**이다.
- 최종 운영 수용은 각 모드에서 첫 회를 별도 예열로 구분하고 이어지는 **10회 전부**를 집계했다. 읽기 전용 guard와 기존 runtime 역할을 유지했다. startup/동시 부하의 변동을 숨기지 않는다.

| 배포 후 SQL | 계획 모드 | shared hit+read 블록 | 평균 ms | 최소 ms | 최대 ms |
| --- | --- | ---: | ---: | ---: | ---: |
| S1 observability | custom | 387 | 8.3713 | 6.556 | 14.210 |
| S1 observability | generic | 387 | 6.5813 | 6.034 | 7.345 |
| S2 live-check | custom | 260 | 3.5409 | 2.456 | 5.435 |
| S2 live-check | generic | 260 | 2.7891 | 2.264 | 3.427 |

- **수용 상태: 블록 목표 달성, 시간 목표 미달.** 최초 추가 작업 전 동일 SQL의 별도 5회 예열 후 표본은 S1 custom/generic **12.6708/12.354ms·1,112블록**, S2 **5.2302/5.5356ms·786블록**이었다. 데이터·동시 부하가 고정된 장기 벤치마크가 아니므로 감소율을 SLA로 취급하지 않는다.
- 복구점: `hololive-api:rollback-sql-performance-20261014t043829z`, `/opt/hololive-bot/compose/sql-performance-closure-20261014T043829Z/previous/`. 310의 원본 보존으로 이전 API 이미지는 계속 작동하지만, 이전 이미지의 migration runner를 새 ledger에 실행하지 않는다. 새 DB 백업·원격 Git push/tag/Release는 없었다. 로컬 임시 PostgreSQL·baseline overlay·시험 인증 상태는 제거했다.
- 후속 동기 read model의 `8dd380cfc` 기준 정적 잠금 조사에서는 모든 쓰기를 덮는 공통 video/channel 잠금이 없음을 확인했다. live subject, `schedule:<group>`, 기존 행 잠금만 쓰는 video-check/finalizer, missing-row upsert가 서로 다르며 수동 영수증 기록은 **SERIALIZABLE**을 요구한다. 현재 트랜잭션 helper는 DB 격리 기본값을 상속한다.
- **[INFERENCE]** 원본 변경 후 파생 상태를 미리 계산하고 나중에 잠그면 서로 다른 원본 표를 갱신하는 트랜잭션이 오래된 혼합 상태를 덮어쓸 수 있다. 새 동기 파생 상태에는 잠금 뒤의 신선한 snapshot, 여러 영상의 일관된 잠금 순서, SERIALIZABLE 기록·삭제 경로의 충돌 규약이 필요하다. 단순 SQL 조정이나 immutable 영수증 열 추가와 다른 쓰기 경로 계약 확대이므로, 사용자 결정 없이 구현하지 않는다. 현재 시간 목표를 낮추거나 충족한 것으로 처리하지 않는다.
- 수용 문서 통합 전 별도 `refactor/metrics-log-code-20261014-r3` fast-forward로 checkout HEAD가 `c98d54f2c80bc96905101096fb590703b331a9ea`로 바뀌었다. 이 작업의 두 SQL과 310은 변경되지 않았고, 별도 변경에는 311과 `10.1.0` 릴리스가 포함됐다. 읽기 전용 확인에서 API **07:33:25 UTC**, worker **07:33:00 UTC** 기동의 `c98d54f2c`가 healthy였다. 이를 이 작업의 배포나 전체 검증 결과로 주장하지 않으며 다시 이전 이미지로 덮어쓰지 않았다. 단일 checkout writer 경계를 지키기 위해 추가 Git ref 변경을 중단하고, 이 수용 기록은 작업 트리에 보존한다. 새 revision과 섞일 수 있는 추가 writer 목록 조사는 중단했다.

## 9. 승인된 쓰기 경로 확대

- 사용자가 **쓰기 경로까지 확대**를 선택했다. 동기 read model의 설계·구현·동시성 검증·운영 적용을 포함하며, S1 450블록/3ms와 S2 280블록/1.5ms 목표를 유지한다. 지연 허용 캐시·재시도·fallback으로 정확도를 낮추지 않는다.
- 작업 위치는 `../hololive-bot-live-readmodel`, branch `perf/sql-live-readmodel-20261014`, base `db35db809801830c4ac4ed1a03a60fa85203054c`다. `db35db8`은 `c98d54f2c` 이후 운영 기록만 추가한 commit이다. 원래 checkout의 별도 작업과 메모를 건드리지 않는다. 310·311은 수정하지 않고 새 migration부터 적용한다.
- 설계 기준: 관련 writer는 공통 DB guard를 **claim/source 행 잠금보다 먼저** 획득한다. guard는 실제 MVCC 쓰기를 통해 오래된 REPEATABLE READ/SERIALIZABLE snapshot의 충돌을 드러낸다. 수동 영수증 기록의 SERIALIZABLE·CAS·append-only·불명 결과 처리 계약을 유지한다.
- 원본 session/head/pending/availability 및 영수증 변화는 같은 트랜잭션의 동기 파생 상태로 반영한다. 원본이 없거나 양쪽 상태가 비활성이면 읽기 모델의 행도 정리한다. 원본 대형 ignored 배열은 복사하지 않는다. 직접 DML·삭제·FK 효과 및 기존 maintenance 경로도 writer 목록에 포함하며 기술적 변경과 판정/신선도 변경을 구분한다.
- 먼저 guard·refresh 시점·다중 영상 잠금 순서의 반례를 검증한다. 원본을 읽기 전에 획득한 잠금과 별도 fresh snapshot 없이 단순 trigger/upsert만 추가하는 설계는 채택하지 않는다. 쓰기 비용·경합도 읽기 성능과 함께 측정한다.
- 실행 항목(§10~13에서 완료): (1) writer/retention/receipt 소유권과 순서 확정, (2) 스키마·원자적 갱신·모든 writer 전환, (3) consumer SQL의 직접 read model 조회 전환, (4) NULL/시간대/정밀도/동시 변경/rollback/삭제/재기록/격리 충돌 검증, (5) 독립 검토·전체 게이트·실제 smoke, (6) 승인된 API writer 정지 창에서 migration·신규 API 시작·운영 목표 수용.
- 새 원본/쓰기 규약을 적용한 뒤에는 구 API 이미지만의 롤백을 안전하다고 가정하지 않는다. 배포 전에 원본 보존·전진 복구 절차와 실제 새/구 바이너리 호환 경계를 확인한다. 기존 새 백업 중단과 원격 Git 미발행 경계를 유지한다.

## 10. 동기 읽기 모델 구현 및 로컬 검증

- 구현 2명(스키마·writer), 독립 검토 2명(동시성/의미·권한/배포)으로 분리했다. 소비 SQL·실행 검증·운영 적용은 통합 소유자가 담당한다.
- 312는 `youtube_live_runtime_state`와 singleton `xid8` guard를 추가한다. session/head의 활성 합집합만 유지하며 ENDED 상대 행·고아 head를 보존한다. 큰 ignored 배열은 저장하지 않는다. 현재 검토 가능하고 310 typed 사실이 일치하는 영수증 중 최대 `recorded_at`을 저장하며 시간 경과 조건은 저장하지 않는다.
- 모든 관측 확정의 `finalizeTx`는 claim보다 먼저 guard를 획득한다. 별도 종료 finalizer는 후보 잠금 전, 증거 retention은 원본/FK 잠금 전, 수동 검토와 head 복구는 source snapshot/잠금 전에 획득한다. 기존 NOW transaction cutoff·SERIALIZABLE/CAS·append-only·실패 결과 의미를 유지한다.
- 직접 DML의 BEFORE STATEMENT guard와 VOLATILE AFTER STATEMENT refresh는 fresh snapshot으로 갱신한다. 원본 변경·동기 모델·삭제·TRUNCATE·FK 효과는 같은 트랜잭션이며 실제 값이 같으면 모델 행을 다시 쓰지 않는다. runtime은 모델 SELECT와 guard EXECUTE만 허용하고 내부 SECURITY DEFINER 함수의 넓은 기본 권한은 회수한다.
- 실제 로컬 `db-migrate`는 PostgreSQL 18.6과 운영 init-db 역할/기본 ACL을 사용해 **173 적용 / 0 생략**으로 성공했다. schema golden을 생성했다. 전체 dbtest **59.682s**, sourceobservation **50.261s**, runtime **67.761s**, targetprojection **11.226s**를 통과했다. 실제 migration을 적용한 head 복구 CLI도 동기 상태·비대상 보존과 stale/count/new-positive/미검토 trigger 거절을 통과했다.
- 실제 SQL 파일을 runtime 역할로 실행한 동일 statement/snapshot의 구·신규 양방향 차이는 두 SQL 모두 **0행**이었다. 합성 fixture는 10,000 종료 이력·215 활성·53 영수증(현재 일치 52건)이며 운영 데이터 복제는 없다. 각 모드 첫 회 예열 후 10회 전부를 집계했다.

| 로컬 SQL | 모드 | shared hit+read | 평균 ms | 최소 ms | 최대 ms |
| --- | --- | ---: | ---: | ---: | ---: |
| S1 observability | custom | 11 | 0.8366 | 0.809 | 0.865 |
| S1 observability | generic | 11 | 0.8384 | 0.773 | 1.007 |
| S2 live-check | custom | 8 | 0.2352 | 0.214 | 0.328 |
| S2 live-check | generic | 8 | 0.2454 | 0.228 | 0.301 |

- **쓰기 비용:** 같은 합성 DB의 4 clients·prepared·각 250건(session+head 갱신)에서 기존 경로는 평균 **0.349ms / 11,475.13 tx/s**, 동기 경로는 **15.979ms / 250.32 tx/s**, 양쪽 실패 **0/1,000**이었다. 전역 직렬화와 갱신 비용을 감추지 않는다. tmpfs·fsync off의 로컬 표본으로 운영 처리량/SLA를 보장하지 않는다. 이후 모델 읽기는 **11블록 / 0.773ms**, 원본 결과 차이는 여전히 **0행**이었다.
- CI가 지적한 Go 내 SQL literal은 소유 query 파일로 옮겼고, 새 경쟁 시험의 유지보수성/whitespace는 동작을 유지한 helper 분리로 수정했다. 영수증 loop 같은 폐기 구현 세부 assertion은 삭제하고 의미·블록 상한 시험은 유지했다.
- 최종 `scripts/ci/local-ci.sh`는 **589.21s**에 통과했다(전체 lint·NilAway·race/non-race 포함). 별도 integration-only 게이트도 **82.91s**에 통과했다. 실제 publish/consume benchmark의 fleet 36건 추정은 **415.15ms / 3,600ms 예산**이었다. stack DB access policy도 통과했다. 운영 읽기 전용 사전 표본은 최근 5분 **702건 PROCESSED**, 그 시점 queue 상태는 PROCESSED뿐이었다. 로컬 포화 쓰기 시험을 운영 처리량 보장으로 바꾸어 해석하지 않는다.
- 운영 전환은 새 이미지 검증 후 **API 정지 → migration 312 → 신규 API만 시작**한다. migration은 source_observations에도 쓰기 배제를 잡아 collector INSERT가 잠깐 대기할 수 있다. `lock_timeout=3s`는 획득 대기 상한이지 전체 backfill 시간 상한이 아니다. 구 API와 새 영수증 writer는 잠금 순서가 달라 image-only rollback을 허용하지 않고 원본 보존·guard-first 수정 이미지로 전진 복구한다. 새 백업·원격 Git 발행은 하지 않는다.

## 11. 운영 312 수용과 S1 target 집계 보완

- `14e102dca17b4e0ed2d7c9bbe0f647da125006aa`의 ARM64 API를 kapu에서 빌드·검증했다. **2026-10-14 08:45:13 UTC** API 정지 창에서 `312`를 **1 적용 / 172 생략 / 총 173**으로 적용했고 **08:45:20.491 UTC** 신규 API가 시작됐다. 세 health/readiness·RestartCount 0·새 ERROR/FATAL/PANIC 0건·PostgreSQL/Valkey/Iris 연결과 변경 시작 후 76건 PROCESSED를 확인했다. worker·collector c의 image/StartedAt은 그대로다.
- 312 checksum은 `6d7e79605a7b4604964251480338a5a7c0b05683a7d243875f7c2d15bb5cc6c5`로 일치하며 이제 수정 금지다. 모델 228행·현재 일치 영수증 53건, 실제 runtime 역할의 모델 쓰기/내부 refresh 권한 거부를 확인했다. 두 SQL의 동일 statement/snapshot 양방향 결과 차이는 **0행**이다.
- 각 모드 첫 회 예열 후 10회 평균: S2 custom/generic **5블록·0.3176/0.3094ms**로 수용했다. S1은 **68블록·3.4366/3.1847ms**로 시간 목표 미달이다. 목표를 완화하지 않는다.
- S1 단일-use CTE·집계 후 빈 kind 보충·rate bucket·직접 roster·clock 비교 등의 읽기 전용 후보를 검증했다. 일부 표본은 2.9ms였지만 반복 평균은 3ms를 넘었고 변동을 숨기지 않는다. 이 후보들은 적용하지 않으며 원래 timestamp/numeric 집계식을 유지한다.
- 남은 구조 집계는 승인된 동기 쓰기 경로 확대 안에서 **기존 projection guard**를 재사용해 CURRENT/STAGING target bundle에만 저장한다. 새 전역 잠금·collector 변경·lease/현재 시각 cache는 없다. mapping은 DB view 한 곳으로 이동하고 `kind/subject`별 RPC 수·최소 cadence/created/not_before만 원본 target과 같은 트랜잭션에서 확정한다. RETIRED bundle은 보관하지 않는다.
- 실행 순서(§12~13에서 완료): 313 및 원자적 refresh/retention 잠금 전환 → 기존 S3의 600 CURRENT/48,000 RETIRED·1행 변경 **100블록** 상한과 동시성/권한/의미 시험 → 실제 SQL smoke·독립 검토·영향 게이트 → 승인된 API-only migration/배포 → S1/S2 운영 custom/generic 수용. target 변경은 영향받은 bundle key만 갱신하며 header 기술적 heartbeat는 전체 bundle을 다시 쓰지 않는다.
- 복구 자료: `hololive-api:rollback-live-readmodel-20261014t084251z`, `/opt/hololive-bot/compose/sql-live-readmodel-20261014T084251Z/previous/`. 초기 파일 준비에서 일반 사용자 디렉터리 권한과 원래 없던 maintenance 경로를 확인해, 성공한 이미지 tag·기존 manifest 보존을 반복하지 않고 준비만 완료했다. migration/기동은 한 번 성공했으며 새 백업·원격 Git 발행은 없다.

## 12. Target bundle 동기화 검증

- 313 구현과 회귀 시험을 2명, 동시성/의미와 권한/배포 독립 검토를 2명으로 분리했다. 두 검토 모두 남은 구체적 결함이 없었으며 실행 검증은 통합 소유자가 담당했다.
- 기존 singleton 누락의 fail-closed 계약을 유지했다. 자동 재생성 제안은 채택하지 않았다. 통합 시험에서 PL/pgSQL OLD/NEW와 transition alias의 이름 충돌을 발견해 별칭을 분리했다. 오류 문구 고정 assertion은 제거하고 실제 거절·원본 미변경·guard 미복구를 검증한다.
- 새 동시성·권한·backfill·rollback·retention 시험은 **9.019s**, 전체 targetprojection은 **6.872s**에 통과했다. 기존 S3 **100블록** 상한은 그대로다. schema golden을 생성했다. 실제 migrator 역할의 `db-migrate`는 **1 적용 / 173 생략 / 총 174**, 다음 실행은 **0 적용 / 174 생략**으로 성공했다.
- 운영의 observation kind별 개수/간격만 집계해 합성 fixture를 **592 target / 451 bundle**로 확장했다(운영 원문·ID 복제 없음). 동일 SQL snapshot 차이는 0행이었다. 로컬 S1 custom/generic은 구조 집계 전 **42블록·2.0523/1.9976ms**에서 동기 bundle 조회 **41블록·1.5616/1.5997ms**였다. 첫 회 예열 후 10회 전부를 사용했으며 운영 수용 수치로 취급하지 않는다.
- 별도 실제 SQL smoke에서 IDLE/ACTIVE/DEFERRED·missing lease·전부 sleeping·NULL 완료·±infinity·빈 kind·만료된 projection의 구·신규 결과 차이는 모두 **0행**이다. 운영 판단에 쓰는 timestamp/numeric 식은 변경하지 않았다.
- Target 쓰기 비용(4 clients, 각 250건): 기존 guard+target 갱신 **0.822ms / 4,868.15 tx/s**, 동기 bundle **3.267ms / 1,224.37 tx/s**, 양쪽 **실패 0/1,000**. 이후 구·신규 조회 차이도 0행이었다. 이 역시 tmpfs·fsync off 로컬 표본으로 운영 처리량을 보장하지 않는다.
- 전체 CI의 race 단계는 두 기존 fixture의 새 잠금 계약 불일치를 잡았다. 역사적 권한 fixture에 현재 guard 함수와 열별 최소 권한을 적용했고, guard를 쥔 writer 밖에서 header를 갱신하던 만료 시험은 사전 만료 예약·reader statement 시각 검증·자연 만료로 바꿨다. 20초 timeout과 대기 후 만료 거절 요구는 그대로다. 해당 race 시험은 runtime **26.546s**, dbtest **5.570s**에 통과했고 영향 lint도 통과했다.
- 원본 checkout은 별도 원격 통합으로 `376afe702f5bcdb59740f1387291457d595614a2`가 됐으며, 작업 base 대비 차이는 deunhealth 의존성 복구 2파일과 changelog뿐이었다. 이 별도 변경을 보존해 최종 로컬 통합하며 원격 Git 발행은 하지 않는다.

## 13. 최종 운영 목표 수용 (2026-10-14)

- 별도 deunhealth 복구를 보존해 통합한 `98b579e5c7a4910c440a79c20295f441f2de74ff`에서 전체 `scripts/ci/local-ci.sh`가 **483.49s**에 통과했다(architecture·lint·NilAway·race/non-race). 최종 313 소스의 `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh --integration-tests-only`도 **70.66s**에 통과했다. 실제 임시 PostgreSQL·Valkey로 dispatchoutbox·summarizer·YouTube dispatch·collector joblease를 실행했다.
- kapu에서 10.1.0 ARM64 API 이미지를 빌드·검증·전송했다. 운영 image ID는 `sha256:7b71ba69f3fa2dfdbaab115c2c6ae13f1cc7966ebc1b0be5941250512a45f9ae`이며 revision이 위 소스와 일치한다. **09:50:13 UTC** API 정지 창에서 313을 **1 적용 / 173 생략 / 총 174**로 적용했고 새 API는 **09:50:20.189 UTC** 시작됐다. 원격 빌드·worker/collector 재기동은 하지 않았다.
- 313 배포 파일 SHA-256은 `828e4ac51add62cc220642db5f8619503ee3a4aae1d409f7f7420f2572e7a9df`로 로컬 원본과 일치한다. ledger 적용 시각은 **09:50:17.142 UTC**이며 **313은 이제 수정 금지**다.
- 실제 API bot/admin health·인증된 LLM readiness, healthy·RestartCount 0, PostgreSQL/Valkey/Iris 연결을 확인했다. 새 컨테이너 로그에 ERROR/FATAL/PANIC·권한·인증서·OOM 오류가 없었다. worker와 collector c는 기존 `c98d54f2c` 이미지와 **07:33:00 / 07:34:17 UTC** 기동 시각을 유지했다.
- 읽기 전용 guard `on`의 수용 표본에서 신규 API 시작 이후 **349건 PROCESSED**, queue는 **PENDING 1건 / PROCESSING·DEAD_LETTER 0건**이었다. 전환 사전 확인 ID 이후 a/b/c/d 네 collector 모두 새 관측을 보냈다. 5초 초과 DB lock wait는 0건이었다. 이 단기 표본을 장기 처리량 보장으로 해석하지 않는다.
- runtime 역할로 원본 target의 독립 mapping 집계와 동기 bundle을 같은 statement/snapshot에서 비교해 양방향 차이 **0행**을 확인했다. bundle은 **451행**, RETIRED bundle은 **0행**, 유효 CURRENT는 **1세대**, live model은 **232행**이었다. 실제 구·신규 S1/S2 SQL의 양방향 결과 차이도 각각 **0행**이었다.
- 운영 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`는 각 SQL·모드에서 첫 회 예열 후 이어지는 **10회 전부**를 집계했다. 읽기 전용 guard와 기존 runtime 역할을 유지했고 블록은 shared hit+read 합이다.

| 최종 운영 SQL | 계획 모드 | shared hit+read 블록 | 평균 ms | 최소 ms | 최대 ms |
| --- | --- | ---: | ---: | ---: | ---: |
| S1 observability | custom | 55 | 2.9860 | 2.347 | 4.963 |
| S1 observability | generic | 55 | 2.4347 | 2.313 | 2.668 |
| S2 live-check | custom | 5 | 0.3039 | 0.265 | 0.345 |
| S2 live-check | generic | 5 | 0.3100 | 0.283 | 0.358 |

- **수용 완료:** custom/generic 모두 S1 **450블록·평균 3ms**, S2 **280블록·평균 1.5ms** 이하다. S1 custom 최대값은 3ms를 넘고 평균도 상한에 가까우므로, 모든 요청의 3ms 상한이나 장기 SLA를 달성했다고 주장하지 않는다. 동기 쓰기의 직렬화·쓰기 비용은 §10/§12의 실측대로 남으며, S4 0.2ms·S5 WAN 잠금 50% 감소와 전체 기존 TOAST 재압축은 검증 범위 밖이다.
- 313 직전 복구점은 `hololive-api:rollback-target-bundles-20261014t094909z`(312 포함 `14e102dca`, image `sha256:6fe6aa39f3a87de372772b58933125abe91a694df360319f2d5365452d185508`)와 `/opt/hololive-bot/compose/sql-target-bundles-20261014T094909Z/previous/`의 manifest다. 둘의 존재를 확인했으며 삭제하지 않는다. 이 API의 313 잠금 호환성과 별개로 이전 migration runner는 새 ledger에 실행하지 않는다. 312 이전 writer로의 image-only rollback과 삭제 데이터 복원은 보장하지 않으며 원본 보존·guard-first 전진 복구 경계를 유지한다.
- 새 DB 백업·원격 Git push/tag/Release는 하지 않았다. 문서 정리 commit은 배포 소스 revision과 구분한다.

## 14. 구현 리뷰 회귀 수정 — 로컬 검증, 운영 미적용

- 영역별 검토 4명과 독립 반증 2명으로 확정한 P2 두 건을 사용자가 수정하도록 승인했다. 구현 2명과 수정 독립 검토 2명으로 분리하고 실행 검증은 통합 소유자가 수행했다.
- **S4:** `metadata_only UPCOMING`일 때 건너뛴 COMPLETE slot이 지연된 `video_live_check` 승격 뒤에도 누락됐다. 일반 PARTIAL의 조기 반환 대신 기존 current-slot SQL을 사용하고, 실제 저장 slot이 있을 때 보이지 않은 대상의 ignored 이력을 복원한다. PARTIAL에 부재 coverage를 만들거나 video-check 의미를 바꾸지 않는다. 절대 0-query assertion과 기록용 wrapper는 제거하고 TOAST·중복·grace 회귀를 유지한다.
- **기록 역할:** 310의 generated-column decoder는 invoker 권한을 검사하지만 기존 별도 기록 역할의 EXECUTE가 누락됐다. 새 `314_youtube_live_review_recorder_decoder_acl.sql`이 기록 함수의 기존 ACL grantee에 decoder EXECUTE만 부여한다. 새 기록 역할에는 기존 기록 의존 권한, guard 및 decoder EXECUTE를 별도로 구성해야 한다. 배포된 migration은 변경하지 않았다.
- **실제 로컬 smoke:** PostgreSQL 18.6과 운영 init-db 역할을 사용했다. 실제 `db-migrate`로 310 이전 **170 적용 / 0 생략**, 별도 NOINHERIT·NOSUPERUSER 역할의 기록 성공, 310~314 **5 적용 / 170 생략**, 재실행 **0 적용 / 175 생략**을 확인했다. 이전·이후 영수증의 generated facts와 runtime 검토 시각이 일치했고 미승인 역할은 SQLSTATE `42501`로 거절되어 영수증 수가 유지됐다.
- 같은 DB에서 합성 관측을 queue에 넣고 실제 runtime 역할의 Claim·Finalize·Consumer를 실행했다. COMPLETE T → 지연된 video-check T−1분 → 다른 LIVE 영상만 포함한 비어 있지 않은 Holodex PARTIAL T → COMPLETE T+2분 순서에서 부재가 **0 → 0 → 1 → 2**, 마지막 상태가 **ENDED**였다(grace 0). 수정 전 동일 시나리오는 부재 1회·LIVE로 남았다.
- **회귀 시험:** 실제 publisher·consumer로 같은 slot 재처리, ignored UPCOMING 이력·TOAST 보존, 새 coverage 없는 PARTIAL, grace 0의 종료와 grace 1시간의 종료 후보를 검증했다. 별도 기록 역할은 310 이전 성공부터 314 및 SQL 본문 재적용 뒤 기록·facts·runtime 판정과 미승인 거절까지 검증한다. 초기 UTC/KST 표현 비교 실패와 트랜잭션 callback의 fatal assertion으로 인한 정리 대기는 시각 값 비교·트랜잭션 밖 assertion으로 수정했다. lint가 지적한 긴 시험은 동작 단계별 helper로 나누고 불필요해진 고정 영상 인자를 모든 caller에서 제거했다.
- **최종 게이트:** `env -u TEST_DATABASE_URL GOEXPERIMENT=jsonv2 LOCAL_CI_GO_SCOPE=changed BASE_REF=HEAD HEAD_REF=HEAD bash scripts/ci/local-ci.sh`가 **352.33초**에 통과했다. 영향 Go 범위는 API·dbtest이며 architecture·manifest·SQL 소유권·vet·staticcheck·golangci-lint(0 issues)·NilAway·build·race/non-race를 포함한다. race 결과는 sourceobservation **62.087초**, dbtest **78.596초**였다. stack DB access policy도 통과했다. 별도 integration-only 실행은 하지 않았다.
- 수정 독립 검토 2건에서 추가 확정 결함이 없었다. 임시 PostgreSQL·SQL/Go smoke·overlay·시험 인증 상태를 제거했다. 314는 기존 함수의 ACL만 보충하므로 schema golden은 변경하지 않았다.
- 이 후속 수정의 운영 migration·배포·Git 발행은 수행하지 않았다. §13의 운영 성능 수치를 새 코드의 측정치로 재사용하지 않는다.

Fallback delta: 새 재시도·대체 경로를 추가하지 않는다.
