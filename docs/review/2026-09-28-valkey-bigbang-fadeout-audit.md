# Valkey 선택적 축소 빅뱅 fadeout 감사

감사 전제는 **필요한 Valkey 기능을 유지하면서, 확정한 제거 기능의 구 경로를 하나의 완결된 패치에서 걷어내는 것**입니다. 이번 요청은 재감사이며 구현·배포 승인이 아닙니다. 2026-09-28 HEAD `3be7229b060b`와 관련 미커밋 작업 트리를 정적으로 확인했습니다. 코드 테스트·실제 cutover·Valkey 조회/삭제는 실행하지 않았습니다.

## 감사한 이전 전환안

직전 [플랜](../current/plans/2026-09-28-valkey-dependency-reduction.md)은 membernews를 먼저 정리하고, 멤버 hash는 B1에서 consumer만 전환하며 기존 writer를 유지한 뒤 B2에서 writer를 지우는 방식이었습니다. 공용 `cache.MemberCache`와 exported method의 제거도 별도 범위로 미뤘습니다. 이러한 중간 배포·호환 API 보존을 이번 빅뱅 제안에서는 폐기합니다.

`DEC-20260731-legacy-fade-out-no-dual-path`는 신 설계 도입 뒤 구 설계를 병존 경로로 남기지 않는 현재 accepted 제약입니다. 제거 기능을 신 코드에서 switch·nil fallback·deprecated wrapper·no-op API로 다시 선택하게 하지 않습니다. 보안상 필요한 마스킹과 과거 감사 기록은 기능 호환 경로가 아닙니다.

## 감사 결과

### F01 높은 우선순위 — 기존 B1/B2 소스·릴리스 구분은 빅뱅 요구와 불일치

writer를 남기는 중간 릴리스와 최종 제거 릴리스를 별도로 만드는 안은 완결된 한 패치의 종료 상태를 보장하지 못합니다. **수정:** 두 제거 기능의 reader·writer·bootstrap·mock·CLI를 한 source revision으로 정리하고, 중간 코드는 리뷰 중의 편집 순서로만 존재합니다. 최종 artifact에는 구 경로를 선택하는 코드가 없어야 합니다.

이는 fleet 배포가 자동으로 원자적이라는 주장은 아닙니다. old/new 동시 운영을 허용하지 않는 coordinated cutover 계획과 서비스 중단 범위가 별도로 필요합니다.

### F02 높은 우선순위 — 공용 cache API를 남기면 기능이 다시 살아날 수 있음

[cache/member.go](../../hololive/hololive-shared/pkg/service/cache/member.go)의 `MemberCache`는 `DomainCache → Client`에 포함됩니다. [member_cache.go](../../hololive/hololive-shared/pkg/service/cache/member_cache.go)는 `InitializeMemberDatabase`, `GetAllMembers`, `GetMemberChannelIDWithOrg`, `GetMemberChannelIDs`를 실제로 구현합니다. runtime 주입만 끊어도 이 API와 key writer가 남습니다.

**수정:** 한 패치에서 이 backend-specific interface·embedding·receiver method·helper를 제거합니다. 사용하지 않는 인자, deprecated alias, nil/no-op 구현은 남기지 않습니다. 외부 Go consumer가 새로 확인되면 같은 변경 집합에 포함하거나 출판을 차단합니다. Go 공개 심볼 삭제는 의도된 source contract 변경으로 제안에 명시하며, HTTP·queue·Pub/Sub 공개 계약 변경과 구분합니다.

`member.Cache`, `domain.MemberDataProvider`, repository의 `GetAllMembers`, matcher의 정상 `GetAllMembers`는 같은 기능이 아닙니다. 동일 문자열 전체 삭제는 잘못된 패치입니다.

### F03 높은 우선순위 — 테스트·mock까지 닫힌 범위를 만들지 못함

[cache/mocks/client.go](../../hololive/hololive-shared/pkg/service/cache/mocks/client.go)에는 `InitializeMemberDatabaseFunc`, map을 반환하는 `GetAllMembersFunc`, `cache.MemberCache` assertion이 있습니다. [client_domain.go](../../hololive/hololive-shared/pkg/service/cache/mocks/client_domain.go)는 corresponding methods를 구현합니다. [alarm_service_durability_test.go](../../hololive/hololive-shared/pkg/service/notification/alarmservice/alarm_service_durability_test.go)도 두 메서드를 연결합니다. matcher의 benchmark·failure test와 공유 cache 테스트도 구 API를 사용합니다.

**수정:** 사용처까지 모두 같은 패치에서 교체합니다. `member_cache_test.go`처럼 삭제된 저장 포맷만 검사하는 테스트는 제거할 수 있으나, 별칭·조직·에러·동시성·allocation 보장은 새 provider fixture에서 유지합니다. mock의 stream·CAS·set 기능과 durable 알림 회귀는 유지합니다. 테스트 디렉터리 제외로 잔재를 숨기지 않습니다.

### F04 높은 우선순위 — script 옵션·환경변수와 보안 fixture가 함께 남음

[bot.sh](../../hololive/hololive-api/scripts/bot.sh)는 `hololive:members`/ready sentinel 외에 `CORE_MEMBER_HASH_SOFT_MIN_COUNT`, `CORE_MEMBER_HASH_SOFT_TIMEOUT_SECONDS`, `--no-ready-wait`를 사용합니다. [test-bot-env-loader.sh](../../hololive/hololive-api/scripts/test-bot-env-loader.sh)는 그 옵션으로 start를 호출한 뒤 command substitution 거부를 검사합니다.

**수정:** 제거 기능에만 쓰이는 옵션·env lookup·대기 루프·상태 표기를 함께 지우는 안을 명시합니다. 제거한 옵션은 지원하지 않는 인자로 실패해야 하며 alias로 받지 않습니다. env-loader test는 새 CLI로 호출해 실제 literal env 검증에 도달해야 합니다. 단순히 start가 실패했다는 이유만으로 보안 검증을 통과시키지 않습니다. 기존 `/ready`와 process-start 확인을 멤버 완전성 검사로 표기하지 않습니다.

### F05 높은 우선순위 — 공용 cache 인자의 일괄 제거는 유지 기능을 훼손

matcher는 별도 `alarm:member_names` fallback을, YouTube apiservice는 channel statistics cache를, membernews bootstrap은 LLM cost tracker를 같은 cache 인자로 사용합니다. `warm_member_cache` 도구는 member epoch/L2 warmup 역할도 가집니다.

**수정:** 없어지는 능력은 중복 hash API와 membernews mirror repository 의존입니다. 해당 capability만 제거하며 필요한 인자·연결은 유지합니다. consumer wiring 검증과 실제 shared module compile이 잔재 grep보다 우선합니다.

### F06 높은 우선순위 — 빅뱅이어도 의미 충돌과 권한 전파 문제는 남음

동일 channelID의 이름 선택은 map 순회 기반이었고, ACL mirror 제거는 Valkey 장애 중 rollback되던 변경을 PG 성공+통지 실패로 바꿀 수 있습니다. 프로세스를 함께 새 버전으로 바꿔도 다음 장애 때 이 문제는 그대로 생깁니다.

**수정:** 이 패치의 제거 집합을 membernews room mirror와 `hololive:members` 기능으로 고정합니다. YouTube 표시 이름은 기존 shared provider의 최소 영속 ID 대표를 사용하는 정규화안을 명시적으로 제안합니다. matcher 후보 우선순위는 보존합니다. ACL mirror·alarm 표시 이름·epoch·limiter 등은 유지 집합에 고정하며 “다음 패치에서 지울 임시 호환 경로”로 취급하지 않습니다.

### F07 중간 우선순위 — 전체 문자열 0건 기준은 로그 보호까지 제거할 수 있음

[privacylog/cachekey.go](../../hololive/hololive-shared/pkg/privacylog/cachekey.go)는 `membernews:room_names`의 room field를 마스킹합니다. 과거 key에 대한 negative I/O 테스트·단발성 정리 명세·감사 문서에도 literal은 남을 수 있습니다.

**수정:** 완료 기준은 구 기능의 실행 가능한 생산·소비·선택 경로 0개입니다. source+test의 backend-specific API 선언·호출은 0이어야 하지만, 마스킹과 폐기 명세의 literal은 정확한 파일·용도별로 검토합니다. broad exclusion·테스트 파일 숨김·기존 보안 규칙 삭제로 grep을 맞추지 않습니다.

### F08 높은 우선순위 — source 원자성과 runtime 전환 원자성은 별도

공통 initializer가 API와 alarm-worker의 startup에서 실행됩니다. shared module만 바꾸고 두 artifact 중 하나를 배포하지 않으면 구 writer가 남습니다. Compose의 여러 서비스 교체는 분산 transaction이 아닙니다.

**수정:** kapu의 한 reviewed revision으로 API·worker 및 필요한 관리 파일을 준비하고, old runtime의 drain·종료와 자동 재기동 방지의 확인 후 새 release set을 시작하는 안으로 변경합니다. 단계 실패를 정상 mixed fleet로 승인하지 않습니다. 실제 호스트·replica·one-shot CLI 목록과 traffic quiescence 수단은 운영 전환 전에 owning ops가 확인해야 합니다. 현재 dirty worktree를 그대로 배포 가능한 provenance로 간주하지 않습니다.

### F09 높은 우선순위 — rollback을 임시 구 코드 복원으로 해석하면 fadeout 위반

새 코드 안에 구 hash reader/writer나 legacy flag를 되살리는 rollback switch는 단일 경로 원칙에 어긋납니다. 과거 이미지가 retained PG queue·worker profile·send-unit과 호환된다고도 보장할 수 없습니다.

**수정:** 기본 복구는 현 revision의 forward fix입니다. 구 release로 rollback하려면 새 프로세스를 먼저 정지하고, 사전 승인한 이전 API·worker·설정·script 세트를 통째로 복원하며 기존 backlog/lease/unknown-send preflight를 통과해야 합니다. 단순 cache key 복구가 이 조건들을 대체하지 않습니다. rollback은 이 패치 적용 상태의 완료 판정을 철회합니다.

### F10 중간 우선순위 — 키 삭제만으로 종료하거나 키 존치를 곧바로 미완료로 간주

대상 mirror에 TTL이 없으므로 자동 만료를 기대할 수 없습니다. 반면 과거 key가 존재해도 새 코드의 기능 경로가 이미 0일 수 있습니다.

**수정:** 소스 fadeout, runtime cutover, 저장된 폐기 key 회수를 세 증거로 구분합니다. key 회수는 소유·모든 writer 중지·rollback 조건을 확인한 뒤 정확한 key만 별도 승인으로 삭제합니다. cleanup이 미실행이면 data residue가 남았다고 보고하며 runtime fadeout을 데이터 전체 삭제와 혼동하지 않습니다. `alarm:*`, `member-cache:*`, 세션·nonce·rate bucket 전체 삭제는 금지합니다.

## 제안한 패치 종료 상태

- 제거 집합 두 기능은 reader/writer/interface/mock/helper/script/options까지 한 변경 집합에서 종료합니다.
- 유지 집합은 별도 소유 역할로 계속 사용하며 Valkey server·client·readiness도 남습니다.
- 새 artifact에 dual-read/dual-write/version switch/deprecated no-op API는 없습니다.
- old data 유무에 관계없이 같은 PG source·입력에서 새 결과가 같아야 하고, 제거 key의 read/write 명령은 0이어야 합니다.
- 현재 감사는 이 종료 상태를 증명한 실행 결과가 아닙니다. source 구현과 격리 회귀, 전체 module compile, 운영 release-set 전환 증거가 후속 필요합니다.

## 판정

직전의 단계적 플랜은 완전 fadeout 빅뱅 기준을 충족하지 않습니다. 이번 제안은 호환 writer/API 유지와 별도 패킷 배포를 없애고, 명시한 두 기능에 대해 완결된 패치 범위와 전환 게이트를 제시합니다. ACL 등 다른 유지 기능까지 무조건 제거해야 빅뱅이 완성되는 것으로 해석하지 않습니다.

운영 downtime 길이·활성 consumer·이미지/설정 provenance·공개 source/CLI 변경 수용은 아직 확인되지 않았습니다. 실행 승인이 주어져도 이 조건을 확인하기 전에는 운영 cutover를 수행할 수 없습니다.
