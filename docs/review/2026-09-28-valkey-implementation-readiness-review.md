# Valkey 선택적 축소 플랜 실행 준비 리뷰

검토일: 2026-09-28. 대상은 [선택적 축소 플랜](../current/plans/2026-09-28-valkey-dependency-reduction.md)입니다. 목표와 유지 항목은 유지하면서 실제 구현·검증·전환에 필요한 조건을 검토했습니다. HEAD는 `3be7229b060b`이며 여러 미커밋 변경이 포함된 작업 트리를 읽었으므로 HEAD만으로 전체 조사 상태를 재현할 수 있다고 주장하지 않습니다. 이번 작업은 문서 수정과 정적 조사만 수행합니다.

이 문서의 B1/B2 consumer-first 전달안은 후속 [빅뱅 fadeout 감사](2026-09-28-valkey-bigbang-fadeout-audit.md)와 현재 플랜으로 대체됐습니다. 실패·이름·TTL 관련 정적 근거는 남기되 중간 writer 릴리스나 exported 구 API 보존을 현재 실행 방식으로 사용하지 않습니다.

## 근거 범위

Hololive의 production 코드·테스트·스크립트에서 key literal과 상수·method 호출을 추적했습니다. 추가로 로컬 `iris-console`, `shared-go`, `iris-client-go`, stack `tools`, stack `deploy`의 Go/TS/TSX/shell/YAML에서 `hololive:members`, `membernews:rooms`, `membernews:room_names`, `acl:settings`, `acl:mode`, `acl:rooms:` literal을 검색했고 해당 검색에서는 결과가 없었습니다. 이는 런타임 command monitoring, 저장소 밖 도구, 문자열 조합 consumer의 부재를 증명하지 않습니다. Iris 루트·native·tools와 운영 secret 디렉터리는 조사하지 않았습니다.

## 추가 발견 사항

### R10 높은 우선순위 — ACL mirror 제거가 통지 실패의 영향까지 확대

**확인:** [service_mutation.go](../../hololive/hololive-api/internal/service/acl/service_mutation.go)의 `SetEnabled`, `SetMode`, `AddRoom`, `RemoveRoom`은 PG 변경 후 mirror 실패 시 보상 rollback합니다. [service_db_test.go](../../hololive/hololive-api/internal/service/acl/service_db_test.go)의 `*RollsBackStateOnCacheSyncError` 테스트가 이를 고정합니다. [api_room.go](../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_room.go)는 mutation 성공 뒤 Pub/Sub를 호출하고, 통지 실패는 HTTP 요청을 실패시키지 않습니다. bot의 ACL은 별도 메모리 인스턴스이며 통지를 받아 PG에서 다시 읽습니다.

**추론:** 지속적인 Valkey 장애에서 mirror만 제거하면, 이전에는 rollback되던 차단 변경이 PG에 성공하고 bot 인스턴스에는 전달되지 않을 수 있습니다. 사용자는 성공 응답을 받았지만 봇은 이전 허용 상태를 유지하는 새 경로입니다. 기존도 mirror 성공 직후 통지가 실패하는 창이 있으므로 현재 구현이 권한 전파의 원자성을 완전히 보장한다는 뜻은 아닙니다.

**플랜 반영:** ACL은 첫 구현 묶음에서 보류합니다. “reader가 없다”만으로 쓰기·rollback을 삭제하지 않습니다. 값 복사 제거와 별개로 두 ACL 인스턴스의 권한 축소, offline subscriber, publish 실패, PG commit 뒤 취소에 대한 성공/실패·수렴 계약을 먼저 확정해야 합니다. 실패를 가리는 PING 확인이나 같은 목적의 dummy write를 추가하지 않습니다. Pub/Sub를 유지한다는 문장만으로 이 게이트를 통과할 수 없습니다.

### R11 중간 우선순위 — startup DB 확인은 경고 수준

**확인:** [repository_cache.go](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/repository_cache.go)는 처음 `ListSubscribedRooms`를 호출하고 DB 오류를 반환합니다. [bootstrap_alarm.go](../../hololive/hololive-api/internal/planes/llm/runtime/bootstrap_alarm.go)의 `initMemberNewsService`는 warmup 오류를 경고로 기록하고 Service를 반환합니다. Subscribe/Unsubscribe는 PG 실패를 반환하고, mirror 실패는 로그 후 성공합니다.

**플랜 반영:** warmup 제거 뒤 동일한 일회성 DB 조회와 warning 수준 진단을 유지하는 것이 제안입니다. 이를 mandatory readiness나 fatal startup으로 승격하지 않습니다. repository의 cache 인자는 제거할 수 있어도 같은 bootstrap에서 `ProvideLLMCostTracker`에 전달하는 cache 인자는 유지해야 합니다. 서비스 전체에서 cache 인자를 없애는 기계적인 정리는 금지합니다.

### R12 높은 우선순위 — producer 제거 순서와 오래된 값의 회수 조건 부족

**확인:** [infra.go](../../hololive/hololive-shared/pkg/providers/modules/infra.go)는 API/worker에서 공통 멤버 provider를 구성합니다. [member_cache.go](../../hololive/hololive-shared/pkg/service/cache/member_cache.go)의 초기화는 `DEL → HSET`이며 TTL을 설정하지 않습니다. `membernews:rooms`·`membernews:room_names`도 대상 쓰기에 TTL이 없습니다.

**영향:** 앱 하나에서 writer를 지워도 다른 버전/프로세스가 다시 쓸 수 있습니다. 반대로 모든 writer를 먼저 없애면 구버전 reader가 남아 있는 동안 오래된 hash를 계속 읽거나 eviction 뒤 빈 값을 볼 수 있습니다. TTL을 기다리면 정리된다는 계획도 맞지 않습니다.

**플랜 반영:** B1 모든 known reader 전환, B2 공통 producer 제거, B3 별도 승인된 운영 key 정리로 구분합니다. B1 기간의 기존 writer 유지는 버전 호환을 위한 유한 전환이며 새 dual-read fallback을 만들지 않습니다. B2 이후 rollback은 PG로 hash를 재생성하는 구버전 startup 동작까지 확인해야 합니다. 무중단·무위험 rollback을 보장하지 않습니다. 코드의 I/O 제거와 실제 Valkey 메모리 회수를 별도 성과로 기록합니다.

### R13 높은 우선순위 — 같은 채널의 여러 멤버 이름은 현재도 선택 방식이 다름

**확인:** 멤버 hash의 field는 `Name:Org`입니다. [apiservice/service.go](../../hololive/hololive-shared/internal/service/youtube/apiservice/service.go)의 `storeChannelNameMap`은 Go map을 순회하며 같은 channelID의 이름을 덮어씁니다. 따라서 여러 field가 같은 channelID를 가리킬 때 선택이 안정적이지 않습니다. 기존 `TestStoreChannelNameMap_LastWriteWinsOnChannelCollision`은 field 하나만 사용해 실제 충돌을 검증하지 않습니다. 반면 [channel_representative.go](../../hololive/hololive-shared/pkg/service/member/channel_representative.go)는 최소 영속 ID를 대표로 선택합니다. matcher는 전체 목록의 첫 channel entry를 중심으로 다른 이름·별칭을 합칩니다.

**플랜 반영:** “모든 표시 이름을 그대로 유지한다”는 무조건적 수용 기준을 피합니다. 충돌 없는 데이터는 동등성을 검증하고, 공유 channel·동명 동조직·이름의 colon·빈 channel·hash-only 행은 명시한 fixture로 처리합니다. YouTube 표시 이름의 대표 규칙은 기존 shared provider의 최소 ID를 따르는 안을 제안하되, 기존 비결정적 선택을 정규화하는 변경으로 명시하고 적용 전 확정합니다. matcher의 후보 우선순위까지 동시에 바꾸지 않습니다.

### R14 중간 우선순위 — 멤버 제공자에도 Valkey 의존과 별도 TTL이 남음

**확인:** [ServiceAdapter](../../hololive/hololive-shared/pkg/service/member/adapter.go)는 `member.Cache`를 사용합니다. epoch 실패 시 PG 우회가 이미 존재하며, 이 작업은 그 경로를 제거하지 않습니다. matcher의 snapshot과 match-result TTL은 각각 1분이고 shared `AllMembers` snapshot TTL은 5분입니다. [apiservice/service_channel_statistics.go](../../hololive/hololive-shared/internal/service/youtube/apiservice/service_channel_statistics.go)는 같은 service의 cache client로 통계 결과를 저장합니다.

**플랜 반영:** 멤버 제공자로 전환한 것을 전체 Valkey 독립으로 보고하지 않습니다. strict double은 제거 대상 key/API만 금지하고 epoch·통계·limiter 등 유지 경로를 허용해야 합니다. matcher snapshot/negative result의 TTL·invalidation 개선을 몰래 섞지 않으며, 멤버 수정·삭제 후 반영 시점도 기존 시간 정책 안에서 검증합니다.

### R15 중간 우선순위 — 기존 repository 테스트만으로 PG 통합 검증 완료라고 할 수 없음

**확인:** [membernews/repository_test.go](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/repository_test.go)는 `fakeMemberNewsPool`을 사용하며 기존 핵심 시나리오는 Subscribe idempotency·Unsubscribe·created-at 순서입니다. [repository_pgx_test.go](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/repository_pgx_test.go)는 nil adapter/scanner를 검사합니다. 이 두 파일 이름만으로 실제 PG replay/동시성 증거가 있다고 해석할 수 없습니다.

**플랜 반영:** 기존 fake 테스트에 DB 오류·startup warning·제거 대상 key 접근 부재를 검증할 회귀를 추가하고, bootstrap/interface 연결은 대상 패키지 compile/test로 확인합니다. SQL이 그대로인 단계에 광범위 DB migration 검증을 요구하지 않습니다. SQL·transaction 의미가 실제 바뀌면 기존 dbtest harness의 격리 integration을 추가합니다. 실행한 계층과 검사하지 않은 계층을 보고서에 구분합니다.

### R16 중간 우선순위 — script health를 멤버 초기화 완료로 오인 가능

**확인:** [bot.sh](../../hololive/hololive-api/scripts/bot.sh)의 hash readiness는 기본 50개·45초의 soft 대기이며 timeout 때 경고 후 계속합니다. 현재 [newBotReadyProbe](../../hololive/hololive-api/internal/planes/bot/runtime/bootstrap_bot_runtime_orchestration.go)는 PG/Valkey 의존을 확인하며 멤버 개수를 검사하지 않습니다. 스크립트는 자체적으로 실행·종료·로그·PID 파일을 다룹니다.

**플랜 반영:** 기존 readiness probe를 사용할 경우 “runtime dependencies ready”로만 표시하고 멤버 데이터 완전성을 주장하지 않습니다. 해시 개수에 의존한 soft readiness 의미의 변경과 CLI/env 호환성을 script 설계 단계에서 명시합니다. 새 health API를 만들거나 운영 script를 실행하지 않습니다. fixture root와 fake container/health/process 명령을 갖춘 harness에서 timeout·잘못된 응답·연결 실패·skip flag를 검사해야 합니다.

### R17 중간 우선순위 — 제거량 측정과 완료 근거가 아직 추상적

**확인:** 성공 경로에서 membernews 구독당 `SADD+HSET`, 해지당 `SREM+HDEL`이 각각 한 번입니다. warmup은 빈 목록에서 두 `DEL`, 비어 있지 않으면 여기에 `SADD+HSET`을 더합니다. 이는 정적 명령 수이며 전체 서비스의 처리량·RTT·latency 측정값이 아닙니다.

**플랜 반영:** 단계 A의 기대 명령 감소를 함수 호출당 2/2/2~4로 정의하고, 변경 뒤 대상 key 호출이 0인지 spy로 검증합니다. 기존 PG query 수와 사용자 결과도 비교합니다. hash 재조회 제거량은 matcher snapshot rebuild 및 YouTube service 초기화당으로 측정합니다. source grep·컴파일 통과·unit 테스트·integration·운영 확인은 서로 다른 근거이며 완료 기록에 섞지 않습니다.

## 실행 경계 결론

첫 구현 단위는 membernews room mirror입니다. 멤버 hash는 데이터 충돌 규칙·script readiness·구버전 consumer 정리가 선행 조건입니다. ACL mirror는 보안 관련 실패 의미가 바뀌므로 별도 정확성 검증 전까지 유지합니다. 알림 이름 cache는 유지하고 의미 검토만 진행합니다. 모든 분류에서 필요한 Valkey 기능은 계속 사용합니다.

이 리뷰의 추가 조건은 `DEC-20260928-hololive-selective-valkey-retention`의 공개 동작·보호 기능 보존 범위를 구체화한 것입니다. 새 구현 권한이나 운영 변경 권한을 부여하지 않습니다. 코드 테스트·운영 장애 재현은 아직 수행하지 않았습니다.
