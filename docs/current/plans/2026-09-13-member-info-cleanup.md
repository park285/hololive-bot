# DB 기반 멤버 정보와 누락 멤버 보완

## Execution capsule
**Goal:** `!정보`의 내장 소개문 의존성을 제거하고 기수와 기본 정보, 신규 멤버 조회를 보존한다.
**Context:** 2026-09-13 착수 당시 members는 소속만 저장하고 기수는 원문/번역 JSON에서 추출했으며, holoAN 개인 3명이 운영 DB에서 누락되어 있었다. 현재 반영 근거와 검증 한계는 아래 T05 후속 대조를 따른다.
**Constraints:** 기존 ID·채널·구독·졸업 상태를 보존한다. 공식 미공개 값은 추측하지 않는다. 최초 범위는 로컬 구현·검증과 누락 멤버 보완이며, 게시·운영 적용은 후속 T05의 별도 승인 범위로 진행했다.
**Evidence:** main ad6616250; 2026-09-13 hololive-osaka 읽기 전용 조회; hololive.hololivepro.com/en/talents/ 및 holoAN 공식 개인 페이지.
**Success:** DB에만 있는 멤버도 조회되고 기수 미등록 멤버는 목록에서 누락되지 않는다. 소개문 JSON/서비스/캐시/관리자 API/수집 도구가 제거되고 회귀 검사가 통과한다.
**Output:** refactor/profile-cleanup-20260913 작업 트리, 멱등 migration, 코드 및 검증 기록. Hololive 계획 루트는 catalog의 legacy 모드이며 별도 PLN은 사용하지 않는다.

### T01 기본 정보 저장과 누락 보완
소유자: shared member repository 및 API migrations. units 배열과 official_link를 저장하고 기본 조회 경로에 생일·데뷔일을 포함한다. 74명 기존 기수(후부키 복수 기수 포함)와 ID 3개 유닛을 이관한다. holoAN 3명의 개인 행을 slug 기준으로 추가한다. 기존 값 충돌은 숨기지 않는다. AC01/V01에 연결된다.

### T02 조회와 목록 전환 및 프로필 제거
T01 이후 bot info/formatter, bootstrap/provider, admin routes 소유자가 DB 기본 정보로 전환한다. 미등록 기수는 소속별 미분류 그룹으로 표시한다. 원문/번역, 수집 CLI, 캐시 초기화, 전용 관리자 API와 불필요한 생성/배포 참조를 제거한다. AC02/V02에 연결된다.

### T03 종합 검증과 전달
T01/T02 이후 변경 diff, 생성물, 의미 있는 회귀·스키마·API 계약 검사를 확인한다. 운영 적용은 소유 migration runner만 사용하고 별도 서비스 배포는 수행하지 않는다. 실행하지 않은 운영 적용과 남은 승인은 명시한다. AC03/V03에 연결된다.

### AC01 데이터 보존과 조회
동일 channel_id를 공유하는 멤버는 개별 identity/units/official link를 보존한다. holoAN 3명은 공개된 데뷔일과 공용 채널을 사용하며 생일은 NULL이다. 재적용 때 중복 행이나 불필요한 UPDATE가 없어야 한다.

### AC02 신규 멤버 지원
프로필 파일 없이 이름/소속/기수/상태/날짜/링크를 표시한다. 알려지지 않은 정보는 생략하고 기수 미등록은 목록에 남는다. 알림·기념일의 기존 DB 데이터와 경로를 보존한다.

### AC03 정리 완결성
실행 경로에 TalentProfile, ProfileService, 번역 캐시, 수집 CLI가 남지 않는다. 폐기 API는 라우터에 등록되지 않는다. 운영 상태와 로컬 변경을 구별하여 보고한다.

### V01 migration과 repository 검증
`bash scripts/architecture/check-migration-manifest.sh`; shared member package tests 및 dbtest 신규 migration 재적용/identity 테스트; `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`로 생성물을 갱신한다.

### V02 명령·템플릿·라우팅 검증
bot info/formatter/handlers, admin app/http/api, bootstrap/runtime의 관련 테스트와 workspace entrypoint/배포 입력 계약을 실행한다. DB-only 신규 멤버, 기수 없음, 복수 유닛, 졸업 경고를 검증한다.

### V03 통합 확인
영향 Go 모듈 build/test 및 필요한 architecture 검사, final diff inspection을 수행한다. 운영 migration은 준비된 파일·검증·현재 DB precondition을 확인한 후 승인 범위와 migration runner 실행 가능성을 구분한다.

## 실행 근거

로컬 구현과 검증 결과는 [멤버 정보 정리 검증](../../review/member-info-cleanup-20260913.md)에 기록하였다.
2026-09-13 당시 T01/T02 및 AC01/AC02/V01/V02를 검증했고 T03/AC03/V03의 로컬 단계는 완료했다. 당시 미수행이던 운영 적용의 후속 근거는 아래 T05 대조 절에 연결한다.

### T04 적대적 리뷰 결함 수정
리뷰에서 확인한 채널 대표 오염, 기본 날짜 이관 누락, 생성 입력 손실, wildcard 조회를 수정한다.
채널 대표는 동일 채널 중 최소 영속 ID의 행으로 정의하고 SQL·메모리·분산 캐시를 일치시킨다.
개인 조회는 채널 매처를 거치지 않고 멤버 스냅샷의 이름·별칭을 정확/부분 비교하며 개인 identity를 보존한다.
기존 공개 날짜는 공식 페이지에 대조하고 DB NULL만 채운다. 생성 API는 지원 필드를 저장한다.

### AC04 리뷰 회귀 방지
개인 이름/별칭/부분 조회 후에도 채널 대표가 유지되고 병렬 워밍업 결과가 같아야 한다.
%/_는 문자 그대로 조회된다. 모호한 개인 후보는 임의 확정하지 않는다.
기존 NULL 날짜 이관, 비NULL 보존, POST 입력의 재조회 보존이 검증되어야 한다.

### V04 리뷰 수정 검증
member repository/cache, info command, admin member 생성 경로, migration의 재현 테스트를 추가한다.
관련 테스트 및 race/NilAway/golangci-lint, 최종 모듈 테스트와 diff를 확인한다.
이전 검증 완료 기록은 AC04에 대한 증거가 아니며 새 결과로 보완한다.

2026-09-14: T04/AC04/V04는 추가 재현 검사와 독립 리뷰어 2명의 재검토로 검증했다.
2026-09-14 리뷰 수정 시점에는 운영 적용 전이었으며, 해당 시점의 로컬 검증 결과는 위 검증 기록의 적대적 리뷰 수정 절에 보존한다.

### T05 게시 및 운영 적용 — 2026-09-14 승인
사용자가 작업 결과의 커밋·push·메인 합류·라이브 반영을 승인했다. 대상은 hololive-bot 저장소와
중앙 hololive-api/hololive-alarm-worker/youtube-collector-c 및 AP 수집기 a/b/d이다.
검토한 197 migration의 기수·기본 날짜·누락 개인 3명 추가, 필요한 로컬 빌드·전송·서비스 교체를 포함한다.
다른 작업 변경, 비밀 값 변경, rollback 자료 삭제는 제외한다.

실행 순서: 깨끗한 커밋 생성 → 필수 push gate 및 원격 메인 합류 → 정확한 커밋의 로컬 아티팩트 빌드·검증
→ 기존 운영 이미지/배포 트리 보존 → 중앙 migration → 중앙 및 AP 순차 교체 → revision/readiness/DB 사후 확인.
완료 조건은 원격 main과 실행 revision 일치, migration ledger 성공, 데이터 보존 및 신규 개인 조회,
모든 대상 health/readiness 성공이다. 운영 결과 증거는 검증 기록에 남긴다.

## T05 후속 대조 — 2026-09-25

PR #499의 main 병합과 운영 반영을 [후속 읽기 전용 검증](../../review/member-info-cleanup-20260913.md#게시와-운영-반영-확인--2026-09-25)으로 대조했다.
API와 수집기 a/b/c/d의 source revision `909f876d0c9c4dd34207afdb5b6376198a214a87`,
worker의 `4d81838a4c143d48896e4d728809a76738f31854`는 모두 멤버 정리 병합을 포함한다.
운영 migration 197의 체크섬, holoAN 개인 3명과 공용 채널 대표, 복수 기수 및 여섯 runtime의 readiness를 확인했다.
기능 반영을 확인한 참고 기록으로 분류하며 같은 migration·배포를 다시 실행하지 않는다.
worker의 revision 차이 때문에 원래 T05의 main/revision 일치를 이번 관측의 PASS로 표시하지 않는다.
최초 배포 당시의 실행 시각·revision 일치는 이 후속 조회로 복원하지 못한다.
원문 메시지 발송이나 DB·Valkey 변경을 수행하지 않았다.
