# 멤버 기본 정보 정리 검증 — 2026-09-13

## 결과와 범위

`refactor/profile-cleanup-20260913` (base `ad6616250`)에서 구현하였다.
작업 트리: `/home/kapu/work/iris-stack/.worktrees/profile-cleanup-20260913/hololive-bot`.
기존 상세 소개문 대신 DB 기본 정보를 조회하며 기수·유닛은 `members.units` 배열로 저장한다.
기존 `CMD_PROFILE` 사용자 템플릿 필드 계약은 유지하지만 소개문/하이라이트 필드는 비워 둔다.
프로필 원문·번역 JSON, 공식 명단 embed, 수집 CLI, ProfileService, 번역 캐시 읽기/초기화,
전용 관리자 `/api/holo/profiles` 및 `/api/holo/profiles/name` 경로를 제거하였다.
기존 소속, 멤버 ID, 채널 ID, 구독과 졸업 상태는 변경하지 않는다.
신규 기수는 DB에 등록하면 재빌드 없이 기본 조회가 가능하며, 기수 미등록도 목록에 남는다.
공용 채널 조회는 요청한 멤버의 이름·별칭을 우선한다.

## 운영 조회와 공식 근거

`hololive-osaka (100.100.1.8) / holo-postgres / hololive`를
`PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=10000'`와
`psql --no-psqlrc -v ON_ERROR_STOP=1`로 조회했다. 매 세션 `SHOW transaction_read_only` 결과는 `on`이었다.
Hololive 분류는 81행이며 모두 suborg가 NULL이다(개인 74, 공식/유닛 채널 6, 수동 등록 1).
현행 공식 목록에 있는 holoAN 개인 3명은 등록되지 않았다. 이들은 기존 holoAN 공용 채널을 사용한다.
공식 페이지의 YouTube 링크는 모두 `@holoANroom`이며 공개 채널 페이지의 externalId는
`UCozx5csNhCx1wsVq3SZVkBQ`로 기존 DB의 holoan-room과 일치했다.

| 대상 | 공식 링크 | 공개된 데뷔일 | 생일 |
| --- | --- | --- | --- |
| Izuki Michiru / 이즈키 미치루 | https://hololive.hololivepro.com/en/talents/izuki-michiru/ | 2025-10-15 | 미공개, NULL |
| Hanazono Sayaka / 하나조노 사야카 | https://hololive.hololivepro.com/en/talents/hanazono-sayaka/ | 2025-11-10 | 미공개, NULL |
| Kazeshiro Yuki / 카제시로 유키 | https://hololive.hololivepro.com/en/talents/kazeshiro-yuki/ | 2025-12-30 | 미공개, NULL |

전체 명단: https://hololive.hololivepro.com/en/talents/
통합 발표: https://hololive10th-anniversary.hololivepro.com/en/news/04/
ID 유닛 대조: https://hololive.hololivepro.com/talents/ayunda-risu/ (AREA15),
https://hololive.hololivepro.com/talents/kureiji-ollie/ (holoro),
https://hololive.hololivepro.com/talents/kobo-kanaeru/ (holoh3ro).
그 외 기존 74명의 기수 정보는 제거 전 등록 데이터에서 이관하고 후부키 복수 기수를 보존했다.
신규 3명은 데뷔한 현행 공식 인원이며 미발표 신규 유닛이나 퇴사한 스태프를 추측하여 추가하지 않았다.

## 마이그레이션과 승인 경계

`197_member_info_units.sql`: units 및 official_link 컬럼 보완, holoAN 3명 INSERT,
기존 74명 slug 기반 기수 이관과 누락 공식 링크 보완. 한 트랜잭션이며 기존 동일값 UPDATE는 하지 않는다.
같은 신규 slug가 다른 소속/채널에 연결된 경우 명시적으로 실패한다.

사용자는 코드 개선·프로필 정리·누락 멤버 데이터 보완을 승인하였다.
운영 데이터 변경 요청은 유효하며, 커밋·애플리케이션 배포·remote Git push는 실행하지 않았다.
현재 운영 DB/Valkey는 변경하지 않았고 위 SQL은 로컬 테스트 DB에만 적용했다.
배포 순서는 검증된 새 migration 바이너리 실행 후 hololive-api 교체이다.
`cmd/db-migrate`는 migrations.FS를 바이너리에 내장하므로 SQL 파일 복사만으로 적용되지 않는다.
운영 적용은 migrations/CONVENTIONS.md의 db-migrate 경로와 hololive-bot-ops 배포 규칙을 따라야 한다.
이전 애플리케이션으로 되돌려도 additive 컬럼과 개인 행은 읽을 수 있다. 데이터 롤백/삭제는 자동 수행하지 않는다.
기존 영구 번역 캐시는 코드 배포 후 해당 namespace만 정리해야 하며 아직 삭제하지 않았다.

## 검증

- migration manifest 검사: 통과.
- schema snapshot 생성 및 전체 hololive-dbtest: 통과.
- migration 반복 적용, 기존 ID/채널/구독/졸업 상태 보존, holoAN 3명, identity 충돌 거부: 통과.
- workspace 전체 Go 모듈 테스트 (`go test ./internal/workspace`): 통과 (111.238s).
- member repository, bot handlers/formatter의 race 검사: 통과.
- member repository, info/formatter, admin app/http의 NilAway: 통과, 진단 0건.
- API/shared/dbtest golangci-lint: 통과. 자동 수정은 포맷 관련이며 suppressions를 추가하지 않았다.
- 명령 entrypoint 계약, AP rsync build 입력 검사: 통과. import graph를 재생성했다.
- 최초 YouTube.js helper 테스트는 새 worktree의 youtubei.js 미설치로 실패했다.
  기존 lockfile로 `npm ci --ignore-scripts` 후 해당 테스트와 전체 workspace 테스트가 통과했다.
- 신규 이름/공용 채널/기수 없음/복수 기수/기본 정보 렌더링/졸업 상태 회귀를 검증했다.

계획 T01/T02, AC01/AC02, V01/V02는 위 구현·검증으로 충족한다.
T03/AC03/V03의 로컬 정리와 검증을 완료했으며 운영 적용은 커밋·배포 단계로 남아 있다.
Fallback delta: 기존 번역 대체 경로를 제거했다. 기수 미등록 표시와 공개되지 않은 기본 정보 생략은 승인된 동작이다.

## 적대적 리뷰 수정 — 2026-09-14

초기 통과 기록만으로는 보장되지 않았던 4개 결함을 수정했다. 아래 기록이 해당 경로의 이전 완료 판단을 대체한다.

1. 채널 대표는 동일 channel_id 중 최소 영속 ID의 행으로 정의했다. repository 단건/사진 조회, 배치 사진 결과,
   스냅샷 메모리 인덱스, 병렬 warm-up이 같은 대표를 사용한다. 이름·별칭 조회는 개인 정보를 채널 대표로 저장하지 않는다.
   검증할 snapshot이 없는 이전 분산 channel cache 값은 채택하지 않고 기존 repository 조회 경로로 대표를 확인한다.
   fresh bootstrap에서도 holoan-room을 개인 3명보다 먼저 만든다. 현재 운영 DB의 기존 그룹 행은 변경하지 않는다.
2. `!정보`는 error-aware 전체 멤버 snapshot에서 개인을 직접 찾는다. 기존 소속 파서·접미사 정규화를 재사용하며
   정확 이름/별칭과 부분 이름/별칭을 순서대로 비교한다. 개인을 Channel로 축약하지 않고 동률은 모호성 응답으로 처리한다.
   SQL alias 조회는 lower 동등 비교로 바꾸어 `%`/`_`를 literal로 취급한다. DB 오류와 미발견을 구분한다.
3. 공식 페이지 74개를 2026-09-13~14에 대조하여 공개 생일 74명과 데뷔일 73명을 NULL인 값에만 backfill한다.
   확인한 항목명은 誕生日, 初配信日, デビュー日, デビュー이다. AZKi 현행 페이지에는 데뷔일 항목이 없어 추정하지 않는다.
   생일의 기준 연도 2000은 공개된 월·일 보관용이며 실제 출생 연도가 아니다. 기존 비NULL 날짜는 보존한다.
4. 생성 API의 units/officialUrl/생일/데뷔일/소속/짧은 한국어 이름/채널 필드를 INSERT하고 다시 읽는다.
   실제 POST→GET 테스트로 성공 응답 후 정보가 사라지지 않음을 확인했다.

추가 검증:

- 채널 단건/사진/배치 조회의 대표 일치, 병렬 warming과 개인 별칭 조회 후 채널 대표·알림명 보존.
- cold cache에 개인 행이 남아 있어도 repository 대표를 반환.
- 공용 채널의 부분 영문 이름, 대소문자, 연속 공백, 한국어 접미사, 소속 한정, 모호한 이름, literal `%`/`_`.
- 미발견 응답으로 DB 실패를 감추지 않는 명령 검사.
- 구라의 NULL 날짜 복원, 기존 아멜리아 날짜 보존, 반복 migration, fresh holoAN 그룹 대표.
- 전체 workspace Go 테스트 통과(71.846s), member/handlers/admin API race 통과, 관련 NilAway 진단 0건.
- 독립 적대적 리뷰어 2명이 수정 경로를 읽고 기존 finding 모두 해결·추가 확정 결함 없음을 확인했다.
  리뷰어는 별도 테스트나 운영 조회를 수행하지 않았으며 실제 검사 실행은 주 작업자가 수행했다.

초기 린트 수정과 파일 편집이 겹쳐 테스트 파일 포맷이 깨진 검사는 실패로 처리했다.
해당 테스트를 복구하고 수정 실행을 직렬화한 뒤 독립 lint cache 및 최종 전체 검사로 재검증했다.

운영 DB/Valkey, 커밋, 배포에는 아직 변경이 없다. 수정이 공용 member repository/cache에 있으므로
운영 적용 시 hololive-api뿐 아니라 이 경로를 사용하는 worker/collector의 빌드·배포 범위를 확인해야 한다.
기존 API 단독 교체 메모만으로 전체 런타임의 대표 선택 변경이 완료됐다고 판단하지 않는다.

최종 전달 검사: API/shared/dbtest 전체 린트 0건, API/shared/alarm-worker/collector 빌드,
마이그레이션 manifest 및 AP rsync manifest 검사, import graph 재생성, `git diff --check` 모두 통과했다.
마지막 테스트 보조 코드 정리 후 member·handlers race도 재통과했다. 원본 checkout과 sibling worktree는 clean이다.
작업 결과는 `refactor/profile-cleanup-20260913` 브랜치(기준 `ad6616250`)의 미커밋 diff로 보존했다.

## 게시 준비 — 2026-09-14

커밋·push·main 합류·운영 적용이 승인되어 T05를 시작했다.
최초 push gate의 `GOWORK=off go mod tidy -diff`가 삭제된 수집 CLI의 goquery 직접 의존성 표기를 발견했다.
API go.mod에서 같은 v1.13.0을 indirect로 이동했다. 버전과 go.sum 변경은 없다.
배포 진입점 정적 계약 5종은 통과했다. 별도 실행한 meta-repo 검사 중 decision inventory는
원본 iris-stack의 기존 INVENTORY.tsv stale 상태로 실패했다. hololive-bot 작업과 무관한 meta 생성물은 수정하지 않았다.
저장소별 게시 게이트는 정상 hook으로 별도 실행하고 그 결과를 기준으로 게시한다.

## 게시와 운영 반영 확인 — 2026-09-25

이 절은 위 2026-09-13~14의 미커밋·미배포 메모 뒤에 확보한 읽기 전용 관측이다.
[PR #499](https://github.com/park285/hololive-bot/pull/499)는 `2026-09-13T16:17:05Z`에 병합되었다.
2026-09-25 관측 당시 `git ls-remote origin refs/heads/main`은
`909f876d0c9c4dd34207afdb5b6376198a214a87`을 반환했다. 이후 main의 이동과 구분한다.
`git merge-base --is-ancestor`로 멤버 정리 병합 `7693d0b936dfc702ca9515d14a6498fd176a1970`이
아래 두 운영 revision에 포함됨을 각각 확인했다.

| 운영 대상 | 관측 당시 source revision | 확인 결과 |
| --- | --- | --- |
| 중앙 API, 수집기 c | `909f876d0c9c4dd34207afdb5b6376198a214a87` | container healthy, restarts 0, H3 readiness 성공 |
| 중앙 alarm-worker | `4d81838a4c143d48896e4d728809a76738f31854` | container healthy, restarts 0, H3 readiness 성공 |
| AP 수집기 b | `909f876d0c9c4dd34207afdb5b6376198a214a87` | container healthy, restarts 0, H3 readiness 성공 |
| AP 수집기 a | 설치 manifest `909f876d0c9c4dd34207afdb5b6376198a214a87` | native active/running, 누적 NRestarts 2, H3 readiness 성공 |
| AP 수집기 d | 설치 manifest `909f876d0c9c4dd34207afdb5b6376198a214a87` | native active/running, NRestarts 0, H3 readiness 성공 |

worker의 revision 차이는 기록대로 보존한다. 두 소스 모두 이 변경을 포함하지만 원래 T05의
main/revision 일치를 이 관측에서 충족한 것으로 판정하지 않는다. 최초 배포 당시의 일치도
이번 조회로 복원할 수 없다. 이번 문서 정리를 위해 재배포하지 않았다.
native 설치 manifest와 현재 readiness를 확인했으며, 누적 재시작 2회를 0회로 표시하거나 원인을 추정하지 않는다.

Hololive 정본 PostgreSQL의 관리 소켓에서
`PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=10000 -c lock_timeout=2000'`,
`psql --no-psqlrc -v ON_ERROR_STOP=1`로만 조회했다. 선행 guard 및 각 조회의
`SHOW transaction_read_only` 결과는 모두 `on`이었다.

- `schema_migrations`의 `197_member_info_units.sql` 적용 시각은 `2026-09-13 16:20:40.033934+00`이다.
- 저장된 SHA-256 `9573f2ce0eeac6ad454ee455f015395a4151ba3f2294723a64d49a201dffa295`가 현재 migration 파일의 `sha256sum`과 일치했다.
- holoAN 개인은 정확히 3행·서로 다른 slug 3개다. 공용 채널·Hololive 소속·`holoAN` unit·공식 링크·공개 데뷔일이 모두 일치하고 생일은 모두 NULL이다.
- `holoan-room`은 1행이며 같은 채널의 최소 영속 ID로 대표를 유지한다.
- `shirakami-fubuki`는 1행이며 기수 배열 길이 2를 유지한다.
- 관측 당시 Hololive 분류는 89행, 기수 등록은 82행이다. 이를 착수 당시의 81행과 혼동하지 않는다.

T05 관련 게시·기능 반영·데이터의 후속 근거를 보완했으며, 원래 모든 완료 조건의 재검증을 뜻하지 않는다.
기존 로컬 회귀와 보존 검증은 앞 절의 결과를 유지한다.
이번에는 배포·migration 재실행, DB/Valkey 쓰기, 과거 번역 캐시 삭제나 실제 `!정보` 메시지 발송을 하지 않았다.
현재 조회만으로 과거의 모든 데이터 변경 이력이나 Valkey namespace 삭제를 증명한다고 주장하지 않는다.
