# UNIT B 멤버별 구독과 판별 미상 알림

**Decisions:** `DEC-20260906-hololive-unit-b-member-subscriptions` (governing), `DEC-20260906-hololive-mekpark-title-host-attribution` (context), `DEC-20260906-hololive-mekpark-host-ml-evaluation` (context)

## Execution capsule
**Goal:** UNIT B의 라이라·미라·네온을 각각 구독하고 진행자 미상 제목은 해당 채널의 모든 구독 방에 알린다.
**Context:** alarms는 room_id/channel_id로 유일하며 캐시와 worker의 두 알림 경로도 채널 단위다.
**Constraints:** 기존 전체 채널 구독과 알림 종류를 유지한다. ACHRORA 멤버 구독, 운영 DB 적용·발송·배포·Git 쓰기는 범위 밖이다.
**Evidence:** shared alarm repository/alarmservice, bot alarm command, worker checker/scheduler와 youtubedispatch의 대상 선정 경로를 확인했다.
**Success:** 멤버별 구독·해지·목록, 단독/공동 진행자 필터, 미상 fail-open, 방별 중복 방지가 로컬 회귀 검사에서 확인된다.
**Output:** 구독 도메인·API·SQL migration·명령·알림 대상 선정 변경과 테스트, 결과 보고서 및 운영 적용 조건을 남긴다.

## 실행 항목

### T01 구독 식별자와 저장 계약 확장

`mekparkhost`의 기존 멤버 사전을 재사용해 UNIT B의 정확한 구독 이름과 host ID를 제공한다. `domain.Alarm`과 추가/삭제 요청에 선택적인 host ID를 전달하고 기존 alarms의 방/채널 식별자를 방/채널/host로 확장한다. 빈 host는 기존 전체 채널 구독이다. 새 migration은 기존 행을 보존하고 UNIT B의 세 host만 허용한다. 기존 전체 구독 해지는 멤버 구독을 지우지 않는다. AC01, V01을 충족한다.

### T02 구독 서비스와 봇 명령 연결

T01 이후 alarmservice에서 멤버별 알림 종류를 독립적으로 추가·해지·조회한다. 채널 캐시는 해당 방의 전체/멤버 구독을 합친 조회 후보이며, 멤버 구독 해지 후 다른 구독의 알림 종류와 채널 registry를 보존한다. DB가 없는 경우 멤버 구독 성공을 가장하지 않는다. 봇의 기존 알람 추가/해지 문법에서 정확한 멤버 이름을 해석하고 목록·응답에 구독한 멤버을 표시한다. 알림 종류와 맞지 않는 다음 방송을 멤버 방송으로 표시하지 않는다. AC02, V02를 충족한다.

### T03 두 알림 경로에 구독 필터 적용

T02 이후 shared alarm 소유자가 구독 행과 제목 판별 결과로 대상 방을 계산한다. 전체 채널 구독은 항상 포함하고, 확인된 진행자는 해당 멤버 구독을 포함하며, 진행자 미상이면 해당 알림 종류를 구독한 UNIT B의 모든 방을 포함한다. 공동 진행자는 합집합, 외부 유닛 게스트는 멤버 진행자 구독 대상에 포함하지 않는다. 데이터 조회 실패는 기존 오류·재시도 경로로 전달한다. worker의 upcoming/schedule-change/live-catchup 및 YouTube outbox의 기존 방송·영상·쇼츠·커뮤니티·마일스톤 경로가 같은 정책을 사용하며 기존 방별 중복 방지·발송 원장을 보존한다. AC03, V03을 충족한다.

### T04 통합 검증과 결과 기록

T03 이후 migration 재생·스키마 snapshot, 영향 패키지의 race/build/lint/NilAway와 계약 검사를 실행한다. 관련 없는 admin/sourceobservation 변경을 보존하고 최종 diff를 검토한다. 보고서에는 실제 검사 결과, fail-open의 범위, 미적용 migration과 구독 writer/worker의 운영 전환 조건을 기록한다. AC04, V04를 충족한다.

## 수용 기준

### AC01 구독 저장 독립성

한 방에서 UNIT B 전체와 세 멤버을 함께 구독할 수 있고 각 멤버의 알림 종류와 해지가 서로 덮어쓰지 않는다. 실제 YouTube channel_id는 유지하고 가짜 채널을 만들지 않는다. 빈 host와 기존 데이터는 전체 구독 의미를 유지한다.

### AC02 사용자 명령과 캐시 일관성

멤버 이름으로 추가·해지하고 목록에서 멤버과 종류를 확인할 수 있다. 중복 추가는 멱등이며 한 멤버의 해지가 다른 멤버/전체 구독을 제거하지 않는다. 캐시 재구축과 부분 실패 이후에도 DB의 전체 구독 집합을 복구할 수 있다.

### AC03 fail-open과 알림 경로 일치

단독 진행자, 공동 진행자, 외부 게스트, 멤버 단서 없는 제목과 빈 제목을 검증한다. 미상일 때 UNIT B 관련 구독 방의 합집합만 포함하며 비구독 방이나 다른 유닛 구독을 확장하지 않는다. 한 방이 여러 구독과 일치해도 같은 이벤트의 알림은 한 번이다. 채널 단위 배치 안에서 서로 다른 진행자의 제목이 섞여도 항목별 대상이 유지된다.

### AC04 검증된 로컬 변경과 운영 경계

의존성·collector·DB의 운영 자료를 바꾸지 않는다. migration은 로컬 테스트 DB에서만 재생하고 이전 migration/타 세션 변경을 보존한다. 요구된 검사 실패나 미실행이 있으면 원인과 남은 범위를 기록한다.

## 검증

### V01 도메인과 실제 PostgreSQL 계약

mekparkhost 구독 이름/판별 테스트와 alarm repository의 실제 DB CRUD 테스트를 수행한다. `./scripts/architecture/check-migration-manifest.sh` 및 `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 새 migration과 생성 schema를 확인한다.

### V02 명령·서비스 회귀

bot alarm handler, shared alarm API/client, alarmservice와 cache warm 테스트로 멤버별 추가/종류/해지/목록, 재구축, 오류 전파를 검증한다. 기존 전체 채널 구독 테스트를 유지한다.

### V03 알림 대상 회귀

worker checking/scheduler와 youtubedispatch에서 동일 채널의 여러 제목·멤버·전체 구독을 포함한 회귀 테스트를 실행한다. 대상 조회 실패를 빈 성공 결과나 판별 미상으로 처리하지 않는지 확인한다.

### V04 종합 검사

영향 Go 패키지의 `go test -race`, `go build`, `golangci-lint run -c .golangci.yml`, `go vet -vettool=/home/kapu/go/bin/nilaway`를 수행한다. `git diff --check`와 `bash ../tools/checks/check-decision-catalog.sh check --submodules`를 통과시키고 결과 보고서로 T/AC/V를 닫는다.
