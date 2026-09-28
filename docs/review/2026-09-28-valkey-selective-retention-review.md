# Valkey 선택적 축소 플랜 상세 리뷰

검토일: 2026-09-28. 사용자가 요청한 범위는 **필요한 곳에 Valkey를 유지하면서 불필요한 연결·복사·쓰기를 줄이는 계획**입니다. 코드·기존 테스트·현재 계약·저장소 내 관리 스크립트를 읽었습니다. 이번 리뷰에서 애플리케이션 코드, 운영 데이터, 서비스 설정은 변경하지 않았으며 실행 테스트·운영 계측도 하지 않았습니다.

후속 [실행 준비 리뷰](2026-09-28-valkey-implementation-readiness-review.md)에서 R10~R17을 추가했습니다. 특히 ACL mirror는 지속적 Valkey 장애의 권한 전파 의미가 달라질 수 있어 구현을 보류합니다. 이 문서의 초기 후보 순서보다 현재 플랜의 단계별 진입 조건이 우선합니다.

이후 사용자가 완전 fadeout 빅뱅 패치 전제의 재감사를 요청했습니다. 현재 제거 집합·공용 API 정리·release 전환 방식은 [빅뱅 감사](2026-09-28-valkey-bigbang-fadeout-audit.md)와 현재 플랜을 따릅니다.

## 검토한 이전 제안

`DEC-20260928-hololive-valkey-reduction-scope`에 연결됐던 초안은 T03에서 결과 캐시의 메모리 전환, T04에서 epoch/config의 PG revision 전환, T05~T08에서 알림·집계·인증·limiter의 PG 이전을 순서대로 요구했습니다. AC08은 Valkey를 남기려면 PG 대안의 실패 근거가 있어야 한다고 했고, T09는 잔여 0일 때 서버·라이브러리 정리까지 포함했습니다. 이 기준을 아래 근거로 수정합니다.

사용자 정정에 따라 이전 제안은 withdrawn으로 철회하고, `DEC-20260928-hololive-selective-valkey-retention`을 새 proposed 제안으로 기록합니다. 아직 accepted 후속 결정이 없으므로 정식 supersedes 관계나 실행 승인을 부여하지 않습니다.

## 발견 사항

### R01 높은 우선순위 — 유지 판단에 부적절한 입증 부담

**문제:** PG로 기술적으로 대체할 수 있으면 옮긴다는 기준은 사용자의 목적보다 넓습니다. Valkey 서버와 client를 계속 사용하는 상황에서 세션 저장·TTL 정리·nonce 원자성·공유 limiter를 PG에 다시 구현해도 운영 의존 하나가 사라지는 효과는 없습니다.

**영향:** DB 테이블·인덱스·정리 작업·동시성 검증과 배포 전환 범위가 늘어납니다. 추가 비용의 크기는 미측정이므로 성능 퇴보를 단정하지 않지만, 현재 요청에 이를 감수할 근거도 없습니다.

**수정:** 공유 임시 상태와 재사용 이익이 있는 캐시는 유지 대상으로 명시합니다. 유지 결정에 PG 실패 실험을 요구하지 않습니다. 제거는 중복 생산·소비 제거, 불필요한 실패 결합 해소처럼 구체적인 이익이 확인된 대상에 적용합니다.

### R02 높은 우선순위 — 멤버 hash의 관리 스크립트 소비 누락

**근거:** [member_providers.go](../../hololive/hololive-shared/pkg/providers/member_providers.go)의 `initializeMemberDatabase`가 `Name:Org → ChannelID`를 기록합니다. matcher와 YouTube API 외에도 [bot.sh](../../hololive/hololive-api/scripts/bot.sh)의 229·234행은 readiness 대기에서 `hololive:members:ready`와 `HLEN hololive:members`를, 461·462행은 상태 표시에서 같은 키를 읽습니다.

**영향:** Go 읽기 두 곳만 바꾸고 producer를 제거하면 스크립트의 readiness 대기와 표시가 실제 앱 상태와 어긋날 수 있습니다. 스크립트는 `holo-valkey`라는 기존 이름을 참조하므로 현재 실행 여부는 미확인이지만, 소비자가 존재한다는 사실은 확인됐습니다.

**수정:** hash 제거 작업에 스크립트의 사용 경로 확인과 readiness 대체를 포함합니다. 운영 스크립트를 실제 실행해 조사하지 않고 우선 정적 호출 관계와 fake 명령 테스트를 사용합니다. consumer 정리 전 producer를 없애지 않습니다.

### R03 높은 우선순위 — 이름 저장소들이 같은 값을 가진다고 가정

**근거:** 멤버 hash는 `Member.Name`을 사용하지만 [state_member.go](../../hololive/hololive-shared/internal/service/notification/alarmcache/state_member.go)의 `ResolveMemberDataName`은 `ShortKoreanName → NameKo → Name` 순서입니다. [alarm_service_cache_write.go](../../hololive/hololive-shared/pkg/service/notification/alarmservice/alarm_service_cache_write.go)는 `HostID`와 구독 데이터의 fallback 이름도 처리합니다. [cache_warm_data.go](../../hololive/hololive-shared/pkg/service/alarm/cache_warm_data.go)는 alarm record의 `MemberName`을 모읍니다.

**영향:** `alarm:member_names`를 일반 멤버 provider 이름으로 일괄 치환하면 이름의 언어·우선순위·미등록 채널·진행자 표기가 바뀔 수 있습니다. 직접 조회를 반복하면 기존 batch 조회가 N+1 조회가 될 수도 있습니다.

**수정:** `hololive:members`와 `alarm:member_names`를 별도 대상으로 취급합니다. 후자는 기존 출력·fallback·공유 채널 대표·batch 조회를 재현하기 전까지 유지합니다. 표시 이름 경로 정리는 별도 조건부 작업으로 둡니다.

### R04 높은 우선순위 — ACL 값 복사와 변경 통지의 책임 혼합

**근거:** [service.go](../../hololive/hololive-api/internal/service/acl/service.go)의 `IsRoomAllowed`는 메모리를 읽고, [service_reload.go](../../hololive/hololive-api/internal/service/acl/service_reload.go)는 PG에서 읽습니다. 저장소 내 `acl:settings`, `acl:mode`, `acl:rooms:*`의 production 참조는 정의와 쓰기 경로에서 확인됐습니다. 그러나 [service_mutation.go](../../hololive/hololive-api/internal/service/acl/service_mutation.go)는 Valkey 쓰기 실패 시 DB와 메모리의 보상 rollback을 수행합니다. [api_room.go](../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_room.go)의 `publishACLChange`는 별도로 `config:update` 통지를 발행합니다.

**영향:** 값의 mirror가 불필요하다는 판단을 Pub/Sub 제거까지 확장할 수 없습니다. bot/admin plane은 같은 프로세스에서도 서로 다른 ACL Service 인스턴스를 사용합니다. 값 쓰기 제거는 mutation의 성공·실패 경계와 rollback 테스트까지 바꾸는 작업입니다.

**수정:** mirror는 제거 후보로 검증하고 변경 통지는 유지합니다. 외부 mirror reader 유무, DB 실패의 명시적 전파, 복제 인스턴스 reload, 차단 변경 반영을 검사합니다. 통지를 놓친 ACL이 다음 기동에 수렴하는 현재 한계도 [api_room.go](../../hololive/hololive-api/internal/planes/admin/internal/server/api/api_room.go) 주석에 존재하므로, 이를 “무손실 설정 전파”로 표현하지 않습니다. 손실 복구 개선은 별도 정확성 과제로 평가합니다.

### R05 중간 우선순위 — 외부 API·LLM 캐시를 일괄 메모리화

**근거:** [cache_manager.go](../../hololive/hololive-shared/pkg/service/holodex/provider/cache_manager.go)는 결과를 Valkey에 보관하고, [service.go](../../hololive/hololive-shared/pkg/service/holodex/provider/service.go)는 같은 서비스에서 공유 rate limiter도 구성합니다. API와 worker가 이 스택을 사용합니다. [summarizer.go](../../hololive/hololive-api/internal/planes/llm/internal/service/majorevent/summarizer/summarizer.go)는 결과를 24시간 캐시하며 cache miss 후 검색·생성·선택적 검토를 수행합니다.

**영향:** 메모리 전환은 API/worker 및 여러 서비스 인스턴스 사이의 hit 공유와 앱 프로세스 재시작 후 재사용을 잃을 수 있습니다. 공유 limiter를 남겨도 중복 upstream 시도가 quota를 소모하거나 제한 대기를 늘리는 문제는 남습니다. LLM은 실제 비용도 달라질 수 있습니다.

**수정:** 기본은 기존 Valkey 캐시 유지입니다. key별 hit 공유·payload·호출 비용과 원하는 변경 이익이 확인된 경우만 선택적으로 메모리를 검토합니다. [official_schedule_fetcher.go](../../hololive/hololive-shared/internal/service/holodex/provider/htmlscraper/official_schedule_fetcher.go)의 전체 페이지는 이미 로컬 TTL·singleflight·clone을 사용하므로 이 경로를 신규 Valkey 제거 성과에 포함하지 않습니다. 채널별 결과 캐시는 별도입니다.

### R06 중간 우선순위 — 선택적 wakeup을 곧바로 삭제 대상으로 지정

**근거:** [publisher.go](../../hololive/hololive-shared/pkg/service/alarm/queue/publisher.go)의 `publishWakeup`은 PG insert 후 3초 guard와 5초 list expiry로 신호를 보냅니다. [alarm_dispatch_idle.go](../../hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_idle.go)는 blocking wait/backoff 및 polling 경로를 함께 갖습니다.

**영향:** polling으로 동작 가능하다는 사실은 latency와 idle DB query 측면에서 삭제가 유리하다는 증거가 아닙니다. 짧은 고정 polling은 idle DB 조회를 늘릴 수 있고 긴 polling은 새 발송의 대기를 늘릴 수 있습니다. 실제 차이는 운영 profile과 부하에 따라 달라지며 미측정입니다.

**수정:** wakeup은 payload 없는 최적화로 유지합니다. 신호 손실 시 PG polling이 due delivery를 계속 찾는 계약을 보존합니다. wakeup 제거는 이 플랜의 완료 조건에서 제외합니다.

### R07 중간 우선순위 — 데이터 캐시와 epoch 조율을 함께 이관

**근거:** [cache_epoch.go](../../hololive/hololive-shared/pkg/service/member/cache_epoch.go)는 epoch read/advance, Pub/Sub, 주기적 reconciliation, regression/실패 시 stale snapshot 우회를 구현합니다. [cache_epoch_test.go](../../hololive/hololive-shared/pkg/service/member/cache_epoch_test.go)에는 remote bump 경합·missed notification·reconnect·두 프로세스 수렴·이전 epoch key 접근 차단 회귀가 있습니다.

**영향:** 작은 중복 hash를 지우기 위해 이 프로토콜까지 PG revision으로 재작성하는 것은 필요 범위를 넘습니다. L2 payload를 줄이는 실험과 invalidation authority를 바꾸는 변경도 서로 다릅니다.

**수정:** epoch/Pub/Sub는 유지합니다. L2 payload의 축소 여부는 hit율·load 절감·메모리 사용량을 확인한 뒤 따로 결정합니다. 단순히 cache 인자를 nil로 주입하는 변경은 허용하지 않습니다.

### R08 높은 우선순위 — PG ledger 존재만으로 claim·억제 의미의 동등성을 추정

**근거:** [dedupe_key.go](../../hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/dedupe_key.go)는 source kind·schedule/category별 event identity를 구성합니다. [outbox_grouper_live_suppression.go](../../hololive/hololive-alarm-worker/internal/egress/youtubedispatch/outbox_grouper_live_suppression.go)는 upcoming marker의 `NotifiedAt`과 15분 창을 읽어 catch-up을 억제합니다. [locker.go](../../hololive/hololive-shared/pkg/service/delivery/locker.go)는 Valkey 오류를 호출자에게 돌려주며 digest는 실행하지 않습니다(2026-09-28 정정: 최초 리뷰의 "락 없이 진행하는 경로"는 stack-audit T19에서 이미 제거됨).

**영향:** terminal delivery dedupe와 사전 처리량 억제·일정 전이·시간 창 기반 알림 억제는 같은 판단이 아닙니다. 반대로 Valkey 락을 발송 정확성의 최종 보장으로 설명하는 것도 코드와 맞지 않습니다.

**수정:** 재구성 가능한 조율·빠른 중복 억제는 유지하고 영속 발송 상태는 PG가 소유합니다. 기존의 Valkey-only terminal state가 실제로 발견되면 해당 계약 위반만 별도 정확성 수정으로 다룹니다. 이번 범위에서 lease 테이블을 새로 만들거나 전체 claim 계층을 삭제하지 않습니다.

### R09 중간 우선순위 — 서버 유지 상태의 성공 지표가 불명확

**근거:** [infra.go](../../hololive/hololive-shared/pkg/providers/modules/infra.go), bot lifecycle, worker readiness는 현재 Valkey를 필수 연결로 취급합니다. 세션·nonce·limiter·scheduler가 남으면 이러한 runtime 의존도 남습니다.

**수정:** 성과를 제거된 mirror 종류·소비자·명령 수·초기화 단계·특정 기능의 불필요한 오류 결합으로 평가합니다. client dependency·Compose service·readiness를 제거하지 않으며, “홀로봇이 Valkey 없이 동작한다”는 완료 문구도 사용하지 않습니다.

## 유지 추천의 한계

- 세션·nonce·rate limit, epoch/config 통지, wakeup, 공유 API·LLM 캐시는 목적이 확인돼 유지할 합리적 근거가 있습니다. 이것이 모든 키의 TTL·오류 처리·보안성이 검증됐다는 뜻은 아닙니다.
- [Valkey ephemeral 계약](../current/contracts/valkey_ephemeral_contract.md)은 cache/index/wakeup, 허용된 만료·eviction의 세션과 bucket을 인정하고 pending/retry/DLQ/quarantine/terminal dedupe·send-in-flight의 Valkey-only 저장을 금지합니다.
- Compose 소스는 `appendonly no`, `save ""`, `allkeys-lfu`입니다. 앱 프로세스 재시작 후 살아 있는 Valkey의 재사용과 Valkey 자체의 영속성을 구분해야 합니다. LLM 월간 경고 누계를 정확한 회계 원장으로 설명할 수 없습니다. 실제 배포 옵션·hit율·메모리 사용량은 이번에 확인하지 않았습니다.
- 원본 데이터 오류를 빈 목록으로 처리하거나 권한을 허용하는 새 fallback은 이 리뷰에서 제안하지 않습니다.

## 권장 구현 단위

| 순서 | 작업 | 기대 이익 | 주의할 경계 |
|---|---|---|---|
| 1 | membernews의 두 room mirror와 write-through 정리 | PG 결과 뒤 추가 Valkey 쓰기·warmup 감소 | repository 오류·startup DB 확인을 함께 없애지 않음 |
| 2 | `hololive:members` 소비를 기존 멤버 제공자로 통합 | 중복 hash 초기화와 재조회 제거 | bot.sh 소비, 별칭/조직/대표 이름, cold-load 실패 |
| 3 | ACL 값 mirror 제거 검증 | mirror 쓰기·보상 rollback 의존 축소 | Pub/Sub 유지, 공개 실패 계약 검토, 권한 변경 전파 |
| 조건부 | `alarm:member_names` 조회 통합 | 이름 저장 중복·직접 Valkey 조회 감소 가능 | 언어 우선순위·구독 fallback·batch parity를 먼저 증명 |
| 필요 시 | L2·외부 결과 캐시의 선택적 계측 | 비용 대비 이익 없는 개별 캐시만 축소 | 측정 없이 전체 메모리 전환하지 않음 |

## 검증 상태

정적 생산·소비 경로와 관련 회귀 테스트의 존재를 확인했습니다. 테스트 실행 결과나 운영 성능 향상을 주장하지 않습니다. 수정된 플랜의 문서 구조·경로·카탈로그는 별도로 검사하며, 코드 구현은 사용자의 플랜 검토 뒤 시작합니다.
