# Valkey 멤버 중복 인덱스 축소

**Decisions:** `DEC-20260928-hololive-valkey-member-index-reduction` (governing)

이 문서는 취소된 초기 범위를 보존합니다. 사용자가 구현을 중단하고 전체 범위의 계획 검토를 요청했습니다. 코드 변경은 원복했으며, 현재 검토 문서는 [Valkey 의존 축소 계획](2026-09-28-valkey-dependency-reduction.md)입니다.

## Execution capsule

**Goal:** 멤버 검색과 YouTube 표시 이름 조회의 중복 Valkey 인덱스 의존을 제거한다.
**Context:** `hololive:members`는 PostgreSQL 멤버 snapshot으로 초기화되며 matcher와 YouTube API가 다시 읽는다.
**Constraints:** 기존 작업을 보존한다. 공개 HTTP·명령 형식, 별칭·조직 구분, 멤버 epoch 일관성, 알림·인증·제한 처리를 유지한다. 운영 변경·배포·출판은 별도 승인이다.
**Evidence:** `pkg/providers/member_providers.go`, matcher snapshot, YouTube API service의 현재 생산·소비 경로.
**Success:** 두 소비자가 기존 멤버 제공자로 동작하고 중복 hash의 runtime 생산·소비가 없으며 대상 테스트가 통과한다.
**Output:** 코드·회귀 테스트와 `docs/review/2026-09-28-valkey-member-index-reduction.md` 검증 기록.

## 실행

### T01 멤버 제공자로 소비 경로 통합

소유자는 hololive-shared providers/apiservice와 hololive-api matcher/bootstrap이다. matcher는 기존 `domain.LoadAllMembers`의 성공 snapshot만 사용한다. YouTube 이름 조회는 이미 주입 가능한 `MemberDataProvider`의 canonical channel representative를 사용한다. provider 오류를 정상 빈 목록으로 바꾸지 않는다. AC01과 V01로 검증한다.

### T02 중복 인덱스 초기화 정리

T01 뒤 shared provider의 `hololive:members` 초기화를 제거하고 새로 불필요해진 내부 코드·테스트를 정리한다. 기존 공개 cache API 삭제는 이번 범위에 포함하지 않는다. 멤버 epoch 및 원격 L2 캐시는 유지한다. AC02와 V01로 검증한다.

### T03 검증 및 의존성 잔여 범위 기록

T01·T02 뒤 diff를 검토하고 실제 테스트 결과, 후속 제거 후보, 운영 미적용 상태를 기록한다. 기존 변경분과 충돌하면 해당 파일 변경을 멈추고 소유 범위를 재확인한다. V01·V02 근거를 남긴다.

## 수용 기준

### AC01 검색과 표시 이름 보존

공식 이름·별칭·부분 일치·조직별 동명이인 검색은 멤버 제공자로 유지한다. 제공자 오류 후 재시도는 성공할 수 있으며 오류 결과를 미발견으로 저장하지 않는다. YouTube 채널명은 조직 suffix 없이 기존 canonical 이름을 사용한다.

### AC02 중복 Valkey 목록 접근 제거

runtime consumer에서 `cache.GetAllMembers` 호출과 bootstrap의 `InitializeMemberDatabase` 호출이 없다. 멤버 epoch/L2 캐시·세션·설정 Pub/Sub·rate limit·알림 상태는 기존 경로를 유지한다.

## 검증

### V01 대상 Go 검증

kapu 저장소 root에서 `go test ./hololive/hololive-shared/pkg/providers/... ./hololive/hololive-shared/internal/service/youtube/apiservice/... ./hololive/hololive-api/internal/planes/bot/internal/service/matcher/... ./hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/... ./hololive/hololive-api/internal/planes/admin/app/...`를 실행한다. matcher와 apiservice는 `-race`로 검사한다. 적용 가능한 NilAway 검사도 수행한다.

### V02 문서와 변경 검토

`git diff --check`, 대상 diff 검토와 DEC/PLN catalog 검증을 수행한다. 새 fallback은 추가하지 않는다. 남은 Valkey 의존은 제거 완료로 보고하지 않는다.
