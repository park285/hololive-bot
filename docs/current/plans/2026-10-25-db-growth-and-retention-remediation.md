# Hololive DB 증가와 보존 적체 해소

**상태: 1단계 처리량 개선(v12.2.0, migration 341)을 2026-10-25 운영에 적용했고 24시간·7일 관찰 중입니다. 2·3단계는 미실행입니다.**

collector 경계 작업과의 실행 순서와 의존은 [통합 계획](2026-10-25-youtube-collection-and-retention-remediation.md)이 소유합니다. 이 문서는 DB 단계의 관측 근거, 보호 불변식, 세부 체크리스트를 계속 소유합니다.

단기는 기존 보호 조건을 유지하면서 삭제 가능한 원본의 적체를 해소하고, 이후 인덱스의 물리 회수 이득을 재측정합니다. 장기는 LIVE·UPCOMING 상태가 모든 과거 관측을 보호하는 구조에 유한한 보존 경계를 설계합니다. 처리량 개선만으로 보호 데이터의 장기 증가까지 해결됐다고 판단하지 않습니다.

## 관측 근거와 원인

2026-10-25 15:29~15:37 KST, authoritative primary `hololive-seoul` / `100.100.1.8` / `holo-postgres` / `hololive`를 조회했습니다. PostgreSQL 18.6이며 모든 DB 세션에서 `transaction_read_only=on`을 증명하고 문장 10~30초·잠금 3초 제한을 적용했습니다. 업무 payload·운영 SQL 원문은 출력하지 않았습니다.

- 최종 DB 크기는 **3,088,987,839 bytes = 약 2.88 GiB**, WAL은 별도 240 MiB입니다. DB 크기에 WAL을 더하거나 빼서 relation 절감량으로 계산하지 않습니다.
- 아래는 Prometheus의 **10월 16일 12:00 KST** 표본과 **10월 25일 15:33 KST 전후** 표본 비교입니다. MiB는 2²⁰ bytes이며 테이블 크기는 인덱스를 포함합니다. 네 테이블 증가 1,113.0 MiB 중 인덱스 증가가 956.0 MiB, **85.9%**입니다. 전체 DB 증가분의 비율로 확대하지 않습니다.

| 테이블 | 10월 16일 MiB | 10월 25일 MiB | 증가 MiB | 그중 인덱스 증가 MiB |
| --- | ---: | ---: | ---: | ---: |
| `source_observations` | 690.2 | 1,076.2 | 385.9 | 286.9 |
| `source_observation_applications` | 660.4 | 1,000.9 | 340.5 | 305.3 |
| `youtube_collection_target_reasons` | 75.2 | 324.0 | 248.8 | 236.8 |
| `youtube_collection_targets` | 61.3 | 199.0 | 137.8 | 127.0 |

### 인덱스 할당 증가

`pgstattuple_approx`의 원본·감사 heap 빈 공간은 각각 약 2.50%·1.13%, dead tuple 비율은 거의 0%였습니다. 본체의 대규모 빈 공간이 주원인은 아닙니다. `pgstatindex` 결과는 다음과 같습니다.

| 인덱스 | 현재 크기 MiB | leaf 밀도 | 해석 |
| --- | ---: | ---: | --- |
| `uq_source_observation_application_entity` | 약 421.3 | 47.03% | 부분적으로 빈 leaf가 많아 재구성 검토 대상입니다. |
| `uq_source_observation_identity` | 약 287.6 | 65.08% | 낮은 밀도와 leaf fragmentation 37.21%를 관측했습니다. |
| `youtube_collection_target_reasons_pkey` | 약 275.4 | 89.45% | 삭제된 페이지 29,198개가 파일에 남아 있습니다. |
| `youtube_collection_targets_pkey` | 약 150.3 | 90.07% | 삭제된 페이지 16,178개가 파일에 남아 있습니다. |

마지막 두 인덱스의 삭제 페이지는 합계 약 **354.5 MiB**입니다. 향후 재사용될 수 있는 공간이며, 전부 즉시 디스크에서 회수된다는 뜻은 아닙니다. 일반 VACUUM·autovacuum은 테이블과 인덱스를 항상 최소 크기로 재작성하지 않습니다. 당시 invalid index, 1분 초과 트랜잭션, 복제 slot은 모두 0이었습니다.

### 보호 데이터와 삭제 가능 적체의 동시 축적

실제 API 설정은 retention 활성화, 120초 간격, 배치 1,000건입니다. 원본 보존은 live/community/video/shorts 7일, channel/video live check 2일, schedule 14일, profile/photo 30일이며 retired projection은 2일입니다.

- 7일 초과 `live_snapshot` 약 **48.9만 행** 중 약 **45.5만 행**은 LIVE·UPCOMING 영상에 대한 application 연결로 보호됩니다. 다른 보호 사유와 중복될 수 있습니다.
- 보호된 만료 live snapshot 중 7일 이상 미관측 UPCOMING 연결은 **9,869행**, LIVE 연결은 **2,777행**입니다. 두 범주는 중복될 수 있으며, 오래 미관측된 예약만으로 45.5만 행 전체를 설명하지 않습니다. 실제 종료 여부도 미관측 기간만으로 판단하지 않습니다.
- 2일 초과 `channel_live_check`는 약 **11.65만 행**이며, 만료 표본 100건은 모두 기존 보호 함수상 삭제 가능했습니다. 전체 11.65만 행이 삭제 가능하다는 전수 판정은 아닙니다.
- 다음 순회 창 1,000건을 같은 조건으로 조회하니 **996건이 보호 대상**이었습니다. 최근 6시간 원본 INSERT 약 52,940 / DELETE 약 143건이었고, 커서는 10월 16일 관측 구간을 진행 중이었습니다. 커서는 실제 전진했으므로 정지·교착으로 분류하지 않습니다.

315 함수는 호출당 합친 후보를 최대 1,000건 검사하며, 현재 source 경로는 tick마다 한 번 호출합니다. 따라서 명목상 하루 최대 약 72만 후보 순회이지 72만 행 삭제가 아닙니다. 보호된 과거 관측을 반복 검사하는 비용이 삭제 가능한 후속 종류의 정리를 늦춥니다. FK fan-out과 잠금 비용은 이 후보 상한으로 제한되지 않습니다.

최근 24시간 retention 오류 0과 실제 삭제 발생은 정리 실행의 증거입니다. **적체 해소의 증거는 아닙니다.** 최초 상태 보고의 정상화 판단은 이 조사로 수정합니다. Prometheus DB 용량 지표에는 과거 수집 공백이 있어 연속 7일 안정성을 주장하지 않습니다.

## 해소 순서

### 1 삭제 가능한 적체의 처리량 개선

- [x] `repository_retention.go`와 `supervisor_retention.go`에 source 원본의 제한된 다중 배치 순회를 설계·구현합니다. 기존 projection의 64배치 처리는 이미 적용된 별도 경로이므로 완료 작업을 다시 구현하지 않습니다.
- [x] 각 배치는 기존처럼 독립 트랜잭션으로 커서와 삭제를 함께 커밋합니다. tick 시작의 cutoff를 고정하고, 기존 `TransactionTimeout`·DB 슬롯 예산을 공유하며 배치마다 시간을 새로 부여하지 않습니다. 반복 상한은 대표 부하 실측으로 확정합니다.
- [x] 기존 공개 삭제 함수의 반환 계약을 유지하면서 검사 수·커서 진척·순회 완료를 식별할 내부 결과를 마련합니다. **삭제 0건이어도 커서가 전진하면 다음 창을 처리**하고, 진척 없음·순회 완료·배치 상한·취소·오류에서 종료합니다. 한 tick에서 같은 보호 순회를 무한 반복하지 않습니다.
- [x] 후속 payload·absence·checkpoint 정리에 실행 예산을 남깁니다. 원본 drain이 예산을 독점하지 않도록 원본 하위 예산과 후속 단계 진행을 검증합니다. 앞 배치의 확정 계수는 뒤 배치 실패에도 보존하고, 실패 문장을 자동 재시도하지 않습니다.
- [ ] 보호 prefix의 혼합 종류 순회가 여전히 지연을 만들면 종류별 커서·상한으로 공정하게 순회하는 후속안을 비교합니다. 커서 변경은 새 migration에서 처리하며 기존 315·321 등 적용 파일을 수정하지 않습니다.

**1단계 구현 결과 (v12.2.0, 로컬 검증)**

- migration 341이 315의 창 처리를 `delete_source_observation_retention_window(kinds, cutoffs, limit)`로 옮겨 `deleted_ids`, `passed`(다음 창이 다시 보지 않는 후보 수, 삭제분 포함), `traversal_completed`(커서 초기화)를 돌려줍니다. 공개 함수 `delete_source_observation_retention_batch`는 삭제 id 행 반환을 유지하고 이 함수에 위임하므로, migration 뒤 이전 image로 되돌려도 같은 동작입니다. 커서 상태 표 권한(runtime 회수)과 잠금 순서·창 크기·보호 재검사는 그대로입니다.
- `deleteEvidenceWindows`가 tick 시작 cutoff로 창을 최대 8개(`sourceRetentionMaxWindows`) 이어서 처리합니다. 순회 완료·`passed` 0·창 상한·오류에서 멈추고, 첫 창 뒤에는 이번 tick의 가장 느린 창만큼 걸려도 남은 DB 시한의 절반 안에 끝날 때만 다음 창을 시작합니다.
- 반복 상한 근거(운영 읽기 전용, 2026-10-25): 원본 창 호출은 2026-08-23 이후 29,269회 평균 673.9 ms·최대 9,737 ms(`pg_stat_statements`)이고, source phase tick은 최근 24시간 중앙값 1.74초·p95 2.43초입니다. 기본 시한 10초에서 시간 예산이 보통 4~7개 창에서 먼저 멈추고, 8은 창이 빠를 때 한 tick의 삭제를 8,000행으로 제한합니다. 대표 동시 부하 비교는 하지 않았으므로 운영 관찰로 확정합니다.
- 실패한 창 앞의 확정 삭제·검사량은 `FailedTable`과 함께 보고하고 다시 실행하지 않습니다. 새 지표는 `hololive_youtube_plane_retention_source_scanned_total`과 `..._source_traversals_total`입니다.
- 테스트: 보호 창을 지나 같은 tick 삭제와 후속 단계 실행, 창 상한에서 멈춘 뒤 새 Repository가 DB 커서로 재개, 삭제 상한을 채운 창의 재개 위치, 순회 완료 보고, runtime 전용 실행 권한, 부분 실패 계수와 지표. 기존 315 테스트가 위임된 공개 함수를 계속 검증합니다.
- 하지 않은 것: 대표 분포 fixture(원본 약 168만·감사 약 450만 행) 부하 비교, 종류별 커서 후속안. 운영 적용 전 0단계 기준선이 필요합니다.

**1단계 운영 적용 (v12.2.0, 2026-10-25)**

- 릴리스: #701에 적체 나이 테스트의 시각 기준 수정 #702를 더해 main `9d9d569990d7c155f1fdc0d5693d1b95ef85bad5`에 tag `v12.2.0`과 GitHub Release를 게시했습니다. 첫 tag push는 pre-push gate에서 그 테스트가 부하 중 시각 차이(2.37초)로 실패해 막혔고, 원격에 올라간 tag는 없었습니다.
- 적용 전 기준선(운영 읽기 전용, 적용 직전 24시간):

  | 항목 | 기준선 | 허용 범위 |
  | --- | --- | --- |
  | DB 슬롯 대기 p99 | consume 4.9 ms, claim 49.7 ms, live_end 1.02 s, retention_source 0.13 s | consume < 50 ms, claim < 250 ms, live_end < 2 s |
  | retention_source 슬롯 보유 p95 | 2.43 s | ≤ 7 s, timeout 0 |
  | queue 최고 나이 | 최대 2.59 s | < 30 s |
  | WAL | 288 MiB(18 segment) | < 1 GiB |
  | DB 크기·여유 | 3,124,106,943 B, hololive-seoul 여유 35.0 GB | 여유 > 25 GiB |
  | 원본 보존 | INSERT 193.3K·DELETE 172.5K(`pg_stat`), retention 오류 0, live 1.66M행 | 오류 0 |
  | 적체 나이 | 시계열 없음 | `unknown` |

  - 허용 범위는 기준선과 기존 시한(DB 작업 10초)으로 정한 운영 판단 값입니다. 넘으면 계획의 중단 조건으로 봅니다.
  - 원본 창 호출(`pg_stat_statements`): 12:58Z에 29,269회·누적 19,724,615 ms, 13:59Z에 29,300회·19,760,356 ms입니다. 그 61분 동안 tick당 창 1개, 평균 1.15초, 창당 약 959행을 지웠습니다.
- 적용 순서(`hololive-bot-ops` no-build pair 절차):
  1. kapu에서 arm64 이미지 `hololive-api:app-9d9d56999-20261025T135925Z-arm64`(`a6af5d1eb589`)를 빌드했습니다. label은 12.2.0과 위 revision이고, CA bundle은 운영 api와 같습니다(`e9c7b63ee2623386…`, 148개). 원격 Id가 같음을 확인했습니다.
  2. 배포 트리의 root·api `VERSION`을 12.2.0으로 설치했습니다. 이전 값은 `/var/tmp/app-9d9d56999-20261025T135925Z/{root,api}-VERSION.previous`에 있습니다(root 12.1.1, api 12.1.2). alarm-worker는 코드 변경이 없어 12.1.1로 둡니다.
  3. prepare: rollback 태그 `hololive-api:rollback-20261025T135925Z`(v12.1.1 `7da9907bc2aa`), 이전 pair capture, 후보 pair 검증, PostgreSQL 연결 예산 검사(allocated 53/60, reserve 4)를 통과했습니다.
  4. `hololive-db-migrate`: `applied=1 skipped=201 total=202`(341)입니다. `hololive_runtime`만 새 함수 실행 권한이 있습니다.
  5. cutover: 14:01:02Z 시작, healthy, 재시작 0, `APP_VERSION` 12.2.0, 새 로그 표지 0입니다. alarm-worker도 healthy·재시작 0입니다.
- 첫 관찰(14:01~14:10Z, source tick 5회):
  - 첫 tick은 348건을 검사해 327건을 지우고 순회를 마쳤습니다(커서 초기화). 이후 4 tick은 모두 창 상한 8개에서 멈춰 tick당 8,000건을 검사했고, 누적 검사 32,348건·삭제 378건입니다. 창 하나가 약 0.6초라 구현 결과에서 예상한 시간 예산(4~7개)보다 상한이 먼저 걸립니다. 적용 전에는 tick당 1,000건이었으므로, 보호 prefix(약 45.5만 행) 한 순회가 약 15시간에서 약 1.9시간으로 줄 것으로 봅니다(추론).
  - retention_source 슬롯 보유 p95 4.88 s(최대 bucket 5 s), 슬롯 대기 p99 consume 5.0 ms·claim 155 ms·live_end 0.96 s, queue 최고 나이 2.01 s, WAL 288 MiB, 여유 35.1 GB, retention 오류 0입니다. 모두 허용 범위 안입니다.
  - 지켜볼 것: 배경 작업 `projection_refresh`의 슬롯 대기 p99가 0.50 s로, 적용 전 24시간의 8분 구간 최대 0.27 s를 넘었습니다. `queue_snapshot`(0.99 s)과 `collection_snapshot`(0.46 s)은 적용 전 범위 안입니다. 원본 창이 슬롯을 더 오래 잡는 영향으로 보며(추론), foreground 지연이 허용 범위를 넘지 않으면 중단 조건이 아닙니다.
- 남은 관찰: 첫 24시간 6시간 구간과 7일 추적은 아래 "검증과 완료 기준"을 따릅니다. 순회 시간은 `..._source_traversals_total` 증가 간격으로, 보호량은 검사량에서 삭제량을 뺀 값으로 봅니다.
- rollback: `hololive-api:rollback-20261025T135925Z`로 pair를 되돌리고 `VERSION` 두 파일을 이전 값으로 복원합니다. 341은 남겨도 이전 image의 공개 함수 동작이 같습니다. 지워진 만료 원본은 복원되지 않습니다.

보존 기간 단축, 수집 주기 저하, 전체 원본 강제 삭제, queue/replay/head/CURRENT/STAGING/ACTIVE lease/live-session/pending-end 보호 해제는 이 단계에 포함하지 않습니다. 전역 live guard → 커서 → 원본 잠금 순서, 잠금 후 보호 재검사, CASCADE와 원자적 커밋을 유지합니다. 단순 interval 단축이나 무제한 배치 증가는 기본 해소안으로 삼지 않습니다.

### 2 보호 이력의 장기 증가에 유한한 경계 설계

1단계로 삭제 가능 적체는 줄일 수 있지만, 계속 UPCOMING인 영상에 연결된 모든 과거 관측의 보존은 남습니다.

- [ ] live snapshot → applications → 세션 참조를 추적해 현재 상태·종료 후보·late positive·replay·감사에 필요한 근거를 구분합니다. 슬롯별 관측 수집과 원본의 장기 저장 필요를 분리합니다.
- [ ] 후보는 세션별 필요한 근거 관측을 명시적으로 참조하고, 그 근거와 나머지 queue/replay/head 등의 보호를 유지하면서 오래된 중간 관측에 기존 TTL을 적용하는 구조입니다. 최신 한 건만 남기는 방식으로 단정하지 않고, 필요한 근거 집합·과거 재처리 가능 범위를 먼저 확정합니다.
- [ ] 참조 전환과 삭제 사이 경합, 늦은 관측·보호 재활성화, rollback·재시작을 검증합니다. 새 참조를 먼저 확정한 뒤 기존 보호를 전환하며 불완전 backfill 상태에서 삭제를 허용하지 않습니다.
- [ ] 장기 UPCOMING은 실제 관측·수명 상태 계약으로 검토합니다. 오래 미관측됐거나 예정 시각이 지났다는 이유만으로 종료 처리하지 않습니다.

이 단계는 321의 광범위한 보호 의미와 replay/audit 범위를 바꿀 수 있으므로 **계약 확정과 명시적 승인 대상**입니다. 현행 보호를 유지하는 1단계와 독립적으로 검토할 수 있습니다. 이 결정을 보류하면 보호 데이터의 무기한 증가 위험은 미해결로 보고합니다.

### 3 적체 정리 후 필요한 인덱스만 공간 회수

- [ ] 정리가 진행된 뒤 네 인덱스의 크기·밀도·삭제 페이지·자연 재사용 추세를 재측정합니다. PG 18의 기존 VACUUM/인덱스 기능을 사용하며 새 extension은 도입하지 않습니다.
- [ ] 회수 이득과 재성장 속도를 비교해 `REINDEX INDEX CONCURRENTLY`의 exact 대상·순서를 준비합니다. 우선 후보는 낮은 밀도의 감사·원본 UNIQUE 인덱스이며, projection PK는 빈 페이지가 신규 쓰기에 재사용되는지 확인한 뒤 판단합니다. 354.5 MiB와 밀도 기반 추정치를 확정 절감량으로 합산하지 않습니다.
- [ ] 승인된 운영 절차에서 한 번에 한 인덱스만 수행합니다. 새 인덱스·임시 공간·WAL 여유, 제약/의존성, 장기 snapshot, 예상 I/O와 잠금 영향을 실행 직전에 확인합니다. CONCURRENTLY도 대기·실패·부하가 없다는 보장은 아닙니다.
- [ ] 실패 시 index의 valid/ready 상태와 제약 연결을 확인하고 전용 복구 절차로 처리합니다. 무조건 재실행·old index 삭제를 하지 않습니다. 완료 후 UNIQUE/PK 무결성, invalid index 0, 크기·쿼리 지연·WAL·디스크 여유를 확인합니다.

원본·감사 heap은 빈 공간이 작으므로 현재 근거로 `VACUUM FULL`을 우선하지 않습니다. 인덱스 재구성은 일회성 회수이며 보존·쓰기 구조 개선을 대체하지 않습니다.

## 검증과 완료 기준

- [ ] 로컬 PostgreSQL 18.6 fixture에 관측 분포를 반영합니다: 원본 약 168만 행, 감사 약 450만 행, LIVE·UPCOMING 보호 원본 약 45.5만 행 규모와 보호 prefix 뒤 삭제 가능 종류를 포함합니다. 운영 payload는 복제하지 않습니다.
- [ ] 기존 제품 테스트를 확장해 보호 불변, 다중 배치 진척·상한, 삭제 0인 창 통과, 잠금 경쟁, 보호 전환, 부분 commit 뒤 오류, 취소·재시작, 후속 정리 진행을 검증합니다. SQL 함수 변경 시 실제 migration runner 재실행과 schema snapshot을 확인합니다.
- [ ] 같은 대표 동시 부하에서 consume 지연·queue age, DB 슬롯 대기, live guard 잠금, 배치 실행시간, WAL과 vacuum을 기존 경로와 비교합니다. 변경 표면에 맞는 저장소 Go/race/lint/빌드와 DB·retention 계약 검사를 실행합니다. 문서·스크립트 텍스트를 검사하는 새 게이트는 만들지 않습니다.
- [ ] 운영 적용 후 첫 24시간은 6시간 구간으로, 이후 최소 7일간 추적합니다. 삭제 가능한 신규 만료 유입보다 정리 처리량이 커서 적체가 감소하고, 최종적으로 한 전체 순회 안에 삭제 가능 후보를 처리하는지 확인합니다. 보호 행과 삭제 가능 행의 수·최고 초과 연령을 구분합니다.
- [ ] 원본 backlog가 제한 조회 때문에 미확정이면 `unknown`으로 유지합니다. 오류 0·삭제량 0·지표 부재를 적체 0으로 바꾸지 않습니다. 전체 순회 시간·검사량·보호량·삭제량을 함께 관측합니다.
- [ ] 물리 크기, live 행 증가, 재사용 공간을 따로 보고합니다. 인덱스는 회수 직후와 7일 뒤를 비교하고, 장기 보호 계약을 보류했다면 전체 DB 크기 수렴을 완료 기준 달성으로 표시하지 않습니다.

처리량 상향 전 foreground 지연·queue age·WAL·디스크 여유의 현재 기준선과 허용 범위를 확정합니다. 반복 timeout, 잠금/처리 지연 악화, WAL·여유 공간 예산 초과 시 추가 drain·인덱스 작업을 중지하고 기존 정리 경로의 보호는 유지합니다. 삭제된 만료 이력은 이미지 롤백으로 복원되지 않으며, 2단계의 보호 변경은 실제 복원 가능한 복구점·손실 범위를 포함해 별도 준비합니다.

## 실행 경계와 관련 코드

이번 요청은 문서화입니다. 코드·migration·runtime 설정·DB 변경, 배포, Git 커밋·원격 발행은 실행하지 않습니다. 후속 구현은 hololive-bot 소유 worktree에서 진행하며, 운영 활성화는 `hololive-bot-ops`와 `stack-platform-ops` 절차에서 대상·효과의 승인을 확인합니다. 설정 master를 바꿀 경우에도 platform secrets 절차를 따릅니다. 자동 재시도·fallback 추가는 제안하지 않습니다.

- [원본 보존 함수와 커서](../../../hololive/hololive-api/scripts/migrations/315_source_retention_bounded_scan.sql), [보호 판정](../../../hololive/hololive-api/scripts/migrations/321_source_application_vocab_cutover.sql)
- [원본 정리 caller](../../../hololive/hololive-api/internal/youtube/sourceobservation/repository_retention.go), [tick·DB 예산](../../../hololive/hololive-api/internal/planes/youtube/runtime/supervisor_retention.go), [적체 측정](../../../hololive/hololive-api/internal/youtube/sourceobservation/queries/repository_retention_backlog_evidence.sql)
- [migration 규약](../../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md), [10월 3일 projection 처리량 보완](2026-10-03-projection-retention-capacity.md), [10월 15일 DB 리뷰](2026-10-15-sql-db-review-handoff.md), [10월 16일 압축 적용 기록](2026-10-16-main-integration-handoff.md)
