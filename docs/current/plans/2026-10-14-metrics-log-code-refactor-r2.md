# 메트릭·로그·코드 분석 기반 리팩토링 2차 (2026-10-14)

1차 계획([2026-10-14-metrics-log-code-refactor.md](2026-10-14-metrics-log-code-refactor.md), 로컬 커밋 `7c568c91c`)이 끝낸 C1·P1·V1·V2·R1·R2·D1~D8 위에서 남은 문제를 고친다. 기준은 `7c568c91c`(main `9feb5b267` v9.3.2 + 1차 커밋 1개)이고, worktree는 `/home/kapu/work/refactor2-20261014/hololive-bot`(branch `refactor/metrics-log-code-20261014-r2`)다.

## 조사 방법

운영 Prometheus(`https://100.100.1.3:9090`, 7일), `logs/remote/*`(2026-10-14 09:57 동기화), 코드 정적 분석(golangci-lint 강화 설정, dupl 100, deadcode)을 15개 차원으로 나눠 조사했다. 발견 125건 중 1건은 1차 계획과 겹쳤고, 남은 124건을 근거·규칙 관점에서 다시 검증해 61건을 확정했다. 63건은 근거 부족, 이전 계획에서 처리됨, 규칙 위반(새 fallback·가드 시험), 효과 대비 위험 과대로 기각했다.

## 측정 요약

- 프로세스 자원은 병목이 아니다(CPU 최대 0.02 core, API RSS 최대 308 MiB).
- 확정된 실제 결함:
  - alarm-worker Tier1 채널의 due 판정 시계가 tick 시각과 갱신 시각이 달라 격번 tick을 건너뛴다. 5분 전 알림 191건 중 105건이 약 60초 늦었고 2건은 5분 알림이 빠졌다(LOG-1).
  - 생일 방송 runner가 30분마다 이미 받은 방에도 다시 발행해 publish 중복의 69%를 만든다(AW-01).
  - YouTube plane CounterVec이 라벨 시리즈를 첫 사건에 생성해, retry·retention 실패의 첫 사건이 `increase()`에서 0으로 사라진다. 10/8 retry 실패 1건·retention 실패 2건이 메트릭에서 0이다(O1).
  - 1차 P1의 burst wake가 처리 뒤에 나가, 활성 worker가 처리하는 동안 polling 안전망이 멈춘다(P1-1).
  - `alarm_upcoming_checkpoints`는 하루 약 13.5k번 쓰지만 읽는 곳이 없다(AW-04).
- calendar 콜드 렌더 6.7 s는 멤버 사진 최대 24장을 순차 다운로드해서 생긴다(BOT-2).
- pgxpool·DB 슬롯 대기·외부 API 지연 메트릭이 없다(O1·O2·O4).
- 로그 소음: DB 장애 때 후보 탐색 WARN이 1초마다 반복되고(8,020건), Holodex 실패 1건이 3줄로 남고, 종료 중 취소한 잡이 실패 WARN으로 기록된다.
- 컨벤션: "x: x:" 이중 래핑, helper 이름 메아리 래핑 102곳, `<무엇> failed:` 절형 문구 49곳, slog error 속성 3형 혼용, room_id 비마스킹 24곳.
- 재활용: 자체 ctx sleep 7곳, 수동 pgx 트랜잭션, 자체 mustSQL 3벌, 수동 singleflight, 로그+500 응답 복제 14곳.

## 범위

확정 항목 원문(근거·검증 판정·보정된 변경안)은 이 계획의 작업 입력이다. 작업은 파일 범위가 겹치지 않게 두 단계로 나눈다.

| 단계 | 묶음 | 항목 |
| --- | --- | --- |
| 1 | API YouTube plane·DB pool 관측 | perf-api-youtube O1·O2(슬롯 대기·보유 히스토그램)·Q2·O3, observability-code O1 |
| 1 | collector | COL-07, observability-code O3·O5(collector), LOG-2, LOG-9, QCS-10 |
| 1 | alarm-worker | AW-01, AW-03, AW-04 1단계, AW-08, LOG-1, LOG-5, QW-1, QW-6, QW-7, QW-8, O5(worker), O6·LOG-4, PD-1(R2 strict env) |
| 1 | hololive-shared | SQL-8, SQL-10, QCS-01, QCS-02(미사용 전역·죽은 필드만), QCS-03, QCS-04(이력 주석만), QCS-07, QCS-08, QCS-09, PD-4, RU6, RU10 |
| 1 | API bot·admin·llm | BOT-2, BOT-6, BOT-8, Q3, Q5, Q7, Q9, P1-1, P1-2, prior-diff-v1 PD-1·PD-2, SA4(API 몫) |
| 2 | 모듈별 오류·로그 컨벤션과 재활용 | Q1, Q2, CV-1~CV-5, LOG-3, SA1, RU2, RU3, O4 |

## 범위 밖

- `alarm_upcoming_checkpoints` DROP migration(AW-04 2단계): DB 스키마 변경이라 승인이 필요하다.
- SQL 자산 이름 규칙(SQL-9): 기존 227개 일괄 개명은 이전 계획처럼 제외한다. 새 파일은 의도를 드러내는 이름을 쓴다.
- `pkg/constants`의 교차 모듈 값·API 전용 값 이동과 패키지 해체(QCS-02의 나머지), MIT 헤더 통일(QCS-04의 나머지): 변경량 대비 효과가 작다.
- 1차 계획의 범위 밖 항목(collector lease 빈 획득, contract generation 캐시, 메트릭 service 라벨, RateLimiter 의존성, deunhealth 이미지)은 그대로다.

## 규칙

- 새 재시도·fallback·호환 shim을 만들지 않는다. 실패·불명 결과는 드러낸다.
- 메트릭 이름·라벨은 바꾸지 않고 새 메트릭만 더한다(observability-stack 규칙·대시보드 참조 확인).
- 오류 문구는 `action: context: %w`, 주석은 한국어로 계약·이유만 적는다.
- 커밋·push·배포는 하지 않는다.

## 검증

- 변경 패키지 `go build`·`go vet`·`go test -race`, golangci-lint(저장소 설정).
- 마지막에 `./build-all.sh --build-only --no-bump`의 local CI gate.

Fallback delta: none.

## 진행 기록 (2026-10-14, worktree 미커밋)

- 15개 차원 분석과 차원별 검증 뒤, 모듈별 1단계 5묶음과 컨벤션 2단계 3묶음으로 구현했다. 최종 diff는 Opus 리뷰 2건을 거쳤다.
- 리뷰 후속:
  - 관리자 activity 로그의 room_id를 가명화했다(`settings_handler.go`, `api_alarm.go`). 운영과 같은 stdout activity logger로 시험한다.
  - AP rsync manifest에 collector 빌드 의존 파일 3개를 추가했다.
  - `contracts/alarm.md`의 Holodex 로그 설명을 실제 코드에 맞췄다.
  - CHANGELOG `## 미출시`에 strict env, YouTube plane DB 예산, 관측 변경을 적었다.
  - 1단계에서 소유 범위 문제로 빠진 AW-03을 반영했다. fallback 카운터는 이제 cache가 있는데 확정하지 못한 DB 조회만 센다.
- gate에서 드러난 NilAway 4건을 고쳤다(httpclientmetrics nil 응답, messagestrings 지연 등록, calendar 사진 슬롯 색인, 시험의 nil 오류).
- 처리하지 않은 항목:
  - QCS-02·QCS-04는 일부만 처리했다(범위 밖 절 참조).
- 후속 보완:
  - SA4: member row 수집 3벌을 `collectMembersFromRows` 하나로 합쳤다. 행·iteration 오류 문구는 그대로다.
  - QCS-02: `alarm_tier.go`를 alarm-worker `tier`로 옮겼다(tier 밖에서 쓰는 `LiveCatchupWindow`·`LiveCatchupSuppressWindow`만 공개). shared 내부 전용 값도 소비 패키지 비공개 값으로 옮겼다(서버 제한→`httpserver`, 본문 기본 한도→`middleware`, circuit·백오프→`apiclient`, 지연 재시도→`holodexprovider`, epoch 재확인 주기→`member`). AP manifest에서 `alarm_tier.go` 줄을 지우고 `httpserver/limits.go`를 더했다.
  - QCS-07: `sendoutcome`을 alarm-worker `internal/egress/sendoutcome`으로 옮겼다(소비처가 worker egress뿐). collector 빌드 의존이 아니어서 AP manifest에서 해당 줄을 지웠다.
  - CV-3·LOG-3은 검증 보정 범위(dbx 등)만 고쳤다.

### 검증 (마지막 수정 이후)

- `./build-all.sh --build-only --no-bump`의 local CI 전 단계가 통과했다. 포함 단계: architecture gate, tidy, vet, staticcheck, golangci-lint, NilAway, build, collector·X Spaces helper 시험, AP manifest, PG capacity, YouTube plane 예산, race 시험, non-race 시험. integration 태그 시험은 기본 설정대로 건너뛰었다.
- 이어지는 이미지 bake는 main과 같은 기존 결함으로 실패했다(`deploy/images/deunhealth` `cmd/app/main.go:155: unknown field BuildDate`).
- `--skip-local-ci hololive-api alarm-worker youtube-collector`로 변경 서비스 이미지(api, worker, collector, po-sandbox) 빌드는 성공했다.
- 운영 효과는 배포 뒤 확인한다. 확인할 지표: `hololive_alarm_youtube_upcoming_decisions_total{selection}` 비율, 생일 방송 publish duplicate 수, `hololive_db_pool_*`, 슬롯 대기·보유 히스토그램.

## 릴리스·배포 (v10.1.0, 2026-10-14 사용자 승인: push·PR, 로컬 main 통합, 운영 배포, checkpoint 테이블 DROP)

- migration 311이 `alarm_upcoming_checkpoints`를 지운다. 이전 worker가 이 표에 쓰므로 중앙 cutover 순서는 새 image 적재 → alarm-worker 재생성 → migration 311 → hololive-api·collector `c` 재생성이다. 그다음 Seoul `b`, Osaka `a`, Osaka2 `d` 순서로 교체한다.
- 311 적용 뒤 10.0.0 이하 worker image로 rollback하면 upcoming 후보 저장이 실패한다. rollback이 필요하면 표를 다시 만드는 migration을 먼저 적용한다.

### 운영 배포 결과 (2026-10-14, UTC)

- **revision:** `c98d54f2c80bc96905101096fb590703b331a9ea`(10.1.0). local CI·`hololive-dbtest`·pre-push gate를 통과한 뒤 PR #636으로 게시했다. tag와 GitHub Release는 만들지 않았다.
- **중앙:**
  - `hololive-api:rollback-20261014T073158Z`, `hololive-alarm-worker:rollback-20261014T073158Z`를 보존했고, 바뀐 트리 파일 6개는 `deploy-backups/pre-v10.1.0-20261014T073158Z/deploy-files.tar`에 있다.
  - 07:33:00Z worker 교체 → migration 311(applied=1, skipped=171, total=172) → 07:33:25Z api 교체 순서로 진행했다. 표 삭제를 확인했다.
  - collector `c`와 issuer는 `po-central-cutover.sh`로 07:34Z에 교체했다. 백업은 `backups/po-c-20261014T073337Z`다.
- **AP:**
  - Seoul `b`의 첫 시도는 rsync 권한 오류로 cutover 전에 멈췄고 실행 중인 컨테이너는 바뀌지 않았다. 원인은 10.0.0 배포 때 root로 풀린 manifest 범위 경로 39개다. 오늘 이전 상태처럼 ubuntu 소유로 되돌린 뒤 다시 배포했고 07:36Z에 기동했다. 되돌린 목록은 `backups/seoul-collector-20261014T073445Z/root-owned-before-chown.txt`에 있다.
  - Osaka `a`는 release `20261014T073649Z-c98d54f2c80b-osaka`, Osaka2 `d`는 `20261014T074005Z-c98d54f2c80b-osaka2`로 완료 검사를 통과했다.
- **수용:** 중앙 4개 컨테이너와 AP 3대가 같은 revision이고 healthy이며 재시작은 0회다. 교체 이후 중앙 오류 표식은 0건이다. `hololive_db_pool_*`, `hololive_http_client_*`, slot wait, retention `phase` 지표가 수집된다.
- **rollback:** api·worker는 위 rollback tag를 `prod`로 되돌리고 트리 백업을 복원한다. 단 worker를 10.0.0으로 되돌리려면 `alarm_upcoming_checkpoints`를 먼저 다시 만들어야 한다. collector c는 `po-central-cutover.sh rollback backups/po-c-20261014T073337Z`, b는 `ap-rollback.sh seoul`, a·d는 `ap-host-native-rollback.sh <ap>`다.
