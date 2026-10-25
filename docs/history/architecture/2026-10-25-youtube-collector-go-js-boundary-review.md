# YouTube collector Go·JS 경계 구조 리뷰

> 2026-10-25에 수행한 읽기 전용 구조 리뷰 기록입니다. 제품 코드 수정, 커밋, 배포를 수행하지 않았으며 아래 권장 사항은 승인된 작업이 아닙니다. 런타임 소유권은 [PROJECT_MAP](../../current/PROJECT_MAP.md), collector 계약은 [youtube-collector 서비스 문서](../../current/services/youtube-collector.md), PO issuer 절차는 [youtube-collector runbook](../../current/runbooks/youtube-collector.md)을 따릅니다.

## 결론과 범위

Go와 Node(YouTube.js)를 함께 쓰는 현재 구조는 유지하는 것이 맞습니다. Go가 프로세스 수명주기, lease, 발행, 요청 한도를 소유하고 Node helper는 private UDS 뒤의 자식 프로세스로만 붙어 있어 경계 설계 자체는 견고합니다. 개선할 곳은 경계가 새는 세 지점입니다.

1. 판정 로직 일부가 helper에 있습니다.
2. wire 계약을 두 언어가 손으로 복제하며, 이를 함께 검증하는 테스트가 거의 없습니다.
3. 일부 실패가 정상값이나 부분 성공으로 바뀝니다.

- 대상: `hololive/hololive-youtube-collector`의 `internal/runtime/youtubejs`, `internal/runtime/youtubejscollector`, `internal/runtime/pobroker`, `cmd/po-broker`, `youtubejs/src`, collector·issuer의 Dockerfile, Compose, native systemd 배포 경로, 관련 shared `sourceobservation` 계약.
- 기준: `origin/main` `4b4eb6485`. 리뷰를 시작한 checkout은 `2b7810f3b`였으나 그 커밋은 migration과 publish plan 테스트만 바꾸며 위 대상 파일을 건드리지 않습니다.
- 방법: Go 프로세스 경계와 패키징은 주 검토자가 직접 읽었습니다. JS helper 내부, PO issuer, Go·JS wire 계약은 읽기 전용 보조 검토 세 건으로 나눠 읽었고, 결론에 쓰인 주장은 주 검토자가 해당 위치를 다시 열어 대조했습니다.
- 전수 감사가 아닙니다. 운영 호스트, 실제 YouTube 응답, 운영 메트릭은 확인하지 않았습니다.

## 현재 구조

```text
collector Go ──spawn · UDS(0700 dir) · HTTP JSON RPC──▶ helper Node (youtubei.js) ──fetch──▶ YouTube
                                                         │ UDS keep-alive
                                                         ▼
                                     po-broker Go ──spawn(Setpgid, NDJSON stdio)──▶ worker Node (BotGuard, 네트워크 없음)
```

- collector Go는 helper를 직접 spawn하고 socket 디렉터리와 정리를 단독 소유합니다. helper가 종료되면 runtime fatal로 끝나고 Compose 또는 systemd가 재시작합니다.
- helper는 `/v1/bootstrap`, `/health`, 목록 RPC(`/v1/community`, `/v1/content`, `/v1/channel`), 확인 RPC(`/v1/channel_live_check`, `/v1/video_live_check`)를 제공합니다.
- PO token은 Google이 내려주는 BotGuard JS를 실행해야 하므로 별도 issuer(`youtube-po-b/c` 또는 `hololive-youtube-po.service`)에서 발급합니다. issuer는 네트워크와 비밀을 갖지 않습니다.
- Compose에서는 collector와 helper가 한 이미지로 배포됩니다. native AP는 host의 `/usr/bin/node`를 사용합니다.

## 발견 사항

확인 수준의 "직접 확인"은 주 검토자가 해당 코드를 열어 본 항목이고, "보조 검토 확인"은 보조 검토가 확인하고 주 검토자가 일부만 대조한 항목입니다.

| # | 심각도 | 요약 | 확인 수준 |
|---|---|---|---|
| 1 | 중간 | channel_live RPC 하나가 내부에서 최대 33회 upstream 호출 | 직접 확인 |
| 2 | 중간 | `CANCELLED` 상태를 JS는 내보내고 shared 계약은 거부 | 경로 직접 확인, upstream 발생 여부 미확인 |
| 3 | 중간 | 영어 badge 문구로 상태 판정, 모르는 문구는 `ENDED` | 직접 확인 |
| 4 | 중간 | `classifyPublication`이 `WaitingStateConfirmed`를 확인하지 않음 | 직접 확인 |
| 5 | 중간 | PO 발급 저하가 collector Go에서 보이지 않음 | 직접 확인 |
| 6 | 중간 | continuation 단계 SDK 예외가 부분 성공으로 바뀜 | 직접 확인 |
| 7 | 중간 | 공유 Innertube가 첫 RPC 요청 컨텍스트에서 초기화됨 | 생성 경로 직접 확인, SDK 동작은 보조 검토 확인 |
| 8 | 중간 | mint 중 취소 시 broker는 세대를 퇴역, controller는 유지를 가정 | 보조 검토 확인 |
| 9 | 낮음~중간 | wire 계약 원천이 네 곳이고 공유 fixture가 하나뿐 | 일부 직접 확인 |
| 10 | 낮음 | Go와 Node가 한 메모리 상한을 공유하는데 Node 상한이 없음 | 직접 확인, V8 기본값 영향은 추론 |
| 11 | 낮음 | 종료 flush, native Node 고정, 시각 파싱, watch 보강, 타입 검사 공백 | 혼합 |

### 1. channel_live RPC의 숨은 upstream fan-out

**근거:** [fetch-channel.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-channel.mjs) 10행의 `maxScheduleMetadataLookups = 32`와 109행 이후 `enrichUpcomingSchedules`는 시각이 없는 예정 행마다 [live-metadata.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/live-metadata.mjs) 13행의 `/player`를 순차 호출합니다. Go [RateLimiter](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/ratelimiter.go)는 RPC 시작 간격만 벌리고, [scheduler_build.go](../../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/scheduler_build.go) 165행의 `jobMaxUpstreamCalls`는 이 job을 1회로 셉니다. 반면 [fetch-content.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-content.mjs) 110행은 "목록 RPC 하나가 숨은 영상별 호출을 만들지 않는다"는 원칙을 밝히고 player 확인을 Go 소유 RPC로 옮겼습니다.

**영향:** 예정 영상이 많은 채널에서는 RPC 하나가 `getChannel` 1회와 player 최대 32회를 2초 간격 제한 없이 보냅니다. 보조 검토에 따르면 각 player 호출은 재시도 transport를 타므로 물리 요청은 더 늘 수 있습니다. 같은 RPC 실행 시한(기본 30초) 안에 순차 호출을 모두 끝내야 하고, 32건을 넘으면 리소스 한도가 아니라 `parser_drift`로 분류됩니다.

**수정 방향:** 예정 영상 player 조회를 content publication처럼 Go가 소유하는 `video_live_check` 단위 RPC로 옮겨 기존 limiter와 호출 예산을 통과하게 합니다. 옮기지 않는다면 helper가 실제 upstream 호출 수를 응답에 보고하고 Go 예산에 반영합니다.

### 2. `CANCELLED` 상태의 계약 불일치

**근거:** [fetch-channel.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-channel.mjs) 292행은 행의 `status`가 `CANCELLED`면 그대로 내보내고, helper 응답 검증도 이를 허용합니다. 같은 파일 58·79행의 `live_query.statuses`는 `ENDED`, `LIVE`, `UPCOMING` 세 값으로 고정됩니다. shared [payloads_normalize.go](../../../hololive/hololive-shared/pkg/contracts/sourceobservation/payloads_normalize.go) 251행은 coverage 밖의 상태를 거부합니다.

**영향:** 이 상태의 행이 한 번 들어오면 helper는 200으로 응답하지만 Go envelope 생성에서 거부되어, 그 채널의 live snapshot job 전체가 매 poll마다 `parser_drift/DATA_CONTRACT`로 실패합니다. fail-closed이지만 영향 범위가 채널 전체입니다. 실제 YouTube.js 행에 이 상태가 오는지는 확인하지 않았습니다.

**수정 방향:** JS가 `CANCELLED`를 내보내지 않게 하거나, `live_query.statuses`와 coverage에 포함하도록 한쪽으로 정합니다.

### 3. 영어 badge 문구 기반 상태 판정

**근거:** [fetch-channel.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-channel.mjs) 295-300행은 `LockupView` badge 문구가 `live` 또는 `upcoming`이 아니면 `ENDED`를 반환합니다. 세션 언어는 SDK 기본값인 영어입니다.

**영향:** YouTube가 badge 문구나 구조를 바꾸면 진행 중인 방송이 오류 없이 `ENDED`로 발행됩니다. API 쪽 부재 판정에 주는 영향은 추론이며 확인하지 않았습니다.

**수정 방향:** 인식하지 못한 badge는 `parser_drift`로 거부하거나, `ENDED`에는 길이 badge 같은 양성 근거를 요구합니다.

### 4. 예정 최초공개 판정의 대기 상태 미확인

**근거:** [content_publication.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejscollector/content_publication.go) 288행 `classifyPublication`의 주석은 "대기 상태가 확인되지 않은 예정 영상은 UNRESOLVED"라고 하지만, 300행 부근의 코드는 `IsUpcoming`, `IsLiveContent`, `ScheduledAt`만 확인하고 `WaitingStateConfirmed`는 보지 않습니다. 현재는 JS가 대기 상태일 때만 `scheduled_at`을 내보내서 결과가 맞습니다.

**영향:** JS 쪽 규칙이 바뀌면 Go가 대기 상태가 확인되지 않은 영상을 `UPCOMING_PREMIERE`로 저장합니다. 거부되지 않고 조용히 잘못된 데이터가 됩니다.

**수정 방향:** `classifyPublication`이 `WaitingStateConfirmed == true`를 직접 요구하게 하고, helper 응답 검증에 "`scheduled_at`이 있으면 대기 상태 확인" 관계를 추가합니다.

### 5. PO 발급 저하의 비가시성

**근거:** [protocol.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/protocol.go) 44행의 `HealthResponse.Proof`는 [health.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/health.go) `Healthy`에서 decode와 검증만 하고 버려집니다. 저장소에 다른 소비자가 없습니다. runbook은 발급 실패 시 무토큰 player로 진행하는 것을 계약으로 두고, 상태 확인은 helper `/health` 직접 조회로만 안내합니다.

**영향:** challenge 구조 변경이나 native issuer의 연속 기동 실패로 발급이 계속 실패해도 collector `/ready`는 정상이고 Go 로그와 메트릭에도 나타나지 않습니다. 무토큰 저하가 무기한 지속될 수 있습니다. systemd start-limit 도달 시 socket까지 멈추는지는 운영에서 확인하지 않았습니다.

**수정 방향:** Go collector가 `proof.state`, `last_error`, 발급 누계를 메트릭으로 내보내고, `UNAVAILABLE` 지속을 관측 대상으로 둡니다.

### 6. continuation 예외의 부분 성공 전환

**근거:** [upstream-errors.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/upstream-errors.mjs) 94-97행은 helper 기원이 아닌 예외를 모두 `collection_failed`로 분류합니다. [pagination.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/pagination.mjs) 267행의 `isContinuationTransient`는 이 코드를 일시 실패로 보고, 228-238행은 앞 페이지까지만 담은 성공(`continuation_transient`)으로 바꿉니다.

**영향:** continuation 응답 형상이 바뀌어 SDK가 TypeError를 던져도 매 실행이 "일시적 간극" 성공으로 보고됩니다. Go는 일시 장애와 영구 drift를 구분하지 못하고, 서비스 문서의 "parser drift는 DATA_CONTRACT" 원칙과도 어긋납니다.

**수정 방향:** `continuation_transient`는 network, 5xx, timeout처럼 transport에서 생긴 실패로만 한정하고, 그 밖의 SDK 예외는 `parser_drift`로 둡니다.

### 7. 공유 Innertube의 요청 컨텍스트 내 초기화

**근거:** [real-fetchers.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/real-fetchers.mjs) 33행 `innertubeClient`는 첫 목록 RPC에서 공유 인스턴스를 지연 생성합니다. [fetch-community.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/fetch-community.mjs) 102행 `createInnertube`는 `retrieve_innertube_config`를 지정하지 않아 생성 중 config 요청이 그 RPC의 signal을 탑니다. 보조 검토에 따르면 SDK는 이 요청의 실패를 로그만 남기고 넘어가며, 생성이 성공한 promise는 다시 만들지 않습니다. 테스트는 config 조회를 끈 상태로만 만듭니다. proof warm-up은 요청 컨텍스트에서 분리되어 있지만 이 초기화는 분리되어 있지 않습니다.

**영향:** 기동 직후 첫 RPC가 취소되거나 config 요청이 429를 받으면 이후 모든 수집이 config 없는 세션으로 돌 수 있고, cooldown 신호도 사라집니다. config가 없을 때 YouTube 응답이 실제로 달라지는지는 확인하지 않았습니다.

**수정 방향:** 공유 인스턴스를 bootstrap 단계에서 요청 컨텍스트 밖에서 만들고, config 조회는 명시적으로 끄거나 실패를 분류된 오류로 돌려줍니다.

### 8. mint 중 취소와 PO 세대 수명

**근거:** 보조 검토에 따르면 [broker.go](../../../hololive/hololive-youtube-collector/internal/runtime/pobroker/broker.go) 664-672행 부근의 mint 처리와 [retirement.go](../../../hololive/hololive-youtube-collector/internal/runtime/pobroker/retirement.go)는 worker IO 중 취소를 요청 중단으로 보고 세대를 퇴역시킵니다. [proof-controller.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/proof-controller.mjs) 97행은 "개별 RPC 취소만으로 공유 세대를 버리지 않는다"고 가정하며, JS 테스트의 fake broker도 세대를 유지합니다.

**영향:** collector RPC deadline이나 종료가 mint 구간과 겹치면 세대가 퇴역하지만 controller는 갱신 시각 전까지 죽은 세대를 계속 씁니다. 다음 호출은 무토큰으로 나가고, 그다음 호출에서 재발급 요청이 생깁니다.

**수정 방향:** broker가 취소 뒤에도 남은 frame을 읽고 세대를 유지하게 하거나, controller가 dispatch 뒤 abort를 받으면 session을 폐기하게 합니다. 어느 쪽이든 fake broker를 실제 동작에 맞춥니다.

### 9. wire 계약의 복제와 검증 공백

**근거:** wire 계약은 Go DTO([protocol.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/protocol.go)), JS 검증기(`rpc-collection-validation.mjs`, `rpc-live-check-validation.mjs`), `contracts.d.ts`, shared `sourceobservation` 계약 네 곳에서 손으로 유지됩니다. 두 언어가 함께 읽는 fixture는 `youtubejs/testdata/pagination-tuples.json` 하나이며, JS의 wire 검증기는 이 fixture로 검증되지 않습니다. 실제 `server.mjs`와 Go 클라이언트를 왕복시키는 오프라인 테스트는 없습니다. 이미 어긋난 값도 있습니다. [rpc.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/rpc.go) 238행의 `/v1/channel` 최소 성공 응답 크기는 171이지만 보조 검토가 계산한 JS 값은 160입니다. 실패 tuple 표(`upstream-errors.mjs` 10-24행, `rpc_failure.go` 40-58행)는 현재 일치하지만 양쪽에 복제되어 있습니다.

**영향:** 양쪽 모두 unknown field를 엄격하게 거부하므로 drift는 운영에서 fail-closed로 드러납니다. 테스트 시점에는 잡히지 않고, 실패 tuple이 어긋나면 cooldown이 protocol mismatch로 바뀌어 not-before 재시도 정책을 잃습니다.

**수정 방향:** JSON Schema 생성은 이 규모(endpoint 5개)와 필드 간 관계 위주의 drift에 맞지 않습니다. 대신 `youtubejs/testdata`에 endpoint별 유효·무효 응답 본문과 실패 tuple fixture를 두고 Go(`decodeHelperSuccess`, `helperStatusError`, envelope 생성)와 JS(응답 검증기, `rpcErrorResult`)가 같은 fixture로 실제 동작을 검증합니다. stub fetcher로 실제 `server.mjs`를 띄우는 오프라인 왕복 테스트를 하나 둡니다. 최소 응답 크기처럼 손으로 복제한 상수는 Go가 최소 struct를 marshal해 계산합니다. 이 보강은 제품 동작을 실행하는 테스트이며 문서 동기화나 해시 매니페스트 게이트가 아닙니다.

### 10. 공유 메모리 상한과 Node heap

**근거:** Compose collector는 768m 상한 안에서 Go와 Node가 함께 돌며 [docker-compose.collector.yml](../../../deploy/compose/docker-compose.collector.yml) 179행의 `GOMEMLIMIT`는 Go에만 적용됩니다. [helper.go](../../../hololive/hololive-youtube-collector/internal/runtime/youtubejs/helper.go) 152행 `spawn`은 Node에 heap 상한 인자를 주지 않습니다. 예산은 "Node 관측 최대 176MiB"라는 주석에 기대고 있습니다. native AP도 `MemoryMax=768M` 안에서 같은 구조입니다.

**영향:** 대형 응답 파싱 등으로 Node가 커지면 cgroup OOM이 Go 또는 Node 중 하나를 죽이고, 어느 쪽이든 collector 재시작으로 이어집니다. 어느 프로세스가 죽을지와 Node 몫이 코드에 드러나지 않습니다. V8 기본 heap 계산이 실제로 어느 값이 되는지는 확인하지 않았습니다.

**수정 방향:** spawn 인자에 `--max-old-space-size`를 명시해 Node 몫을 코드로 고정합니다.

### 11. 낮은 심각도 항목

- **종료 flush:** [server.mjs](../../../hololive/hololive-youtube-collector/youtubejs/src/server.mjs) 68행의 `process.exit(0)`은 drain 완료 직후 실행되어 마지막 응답이 다 쓰이기 전일 수 있습니다. 다만 Go [runtime.go](../../../hololive/hololive-youtube-collector/internal/runtime/collectorruntime/runtime.go) 342행 `shutdown`이 scheduler를 먼저 멈춘 뒤 helper를 닫으므로 정상 종료에서의 영향은 작습니다.
- **native Node 고정:** native AP는 host `/usr/bin/node`를 배포 시점에 `^26.12.1` 범위로만 검사합니다. 이미지처럼 digest로 고정되어 있지 않아 host 패키지 갱신 뒤 버전이 달라질 수 있습니다.
- **시각 파싱 분산:** shorts는 형식 검사 없이 `Date.parse`를 쓰고(JS), community 상대 시각 추정은 Go RPC 계층(`rpc.go`의 `normalizeCommunityPosts`)에서 서로 다른 규칙으로 합니다.
- **watch 보강 경로:** `live-check.mjs`의 watch 페이지 보강은 429, 401/403, 네트워크 실패를 삼키고 본문을 상한 없이 읽습니다. 서비스 문서의 401/403 → CONFIGURATION 분류와 다릅니다.
- **타입 검사 공백:** upstream 모듈은 strict 검사를 받지 않고 손으로 쓴 `.d.mts`가 구현을 가립니다. 실제 런타임 계약은 RPC 응답 검증기가 지킵니다.
- **PO 세대 교체:** 약 12시간마다 세대 교체 동안 무토큰 구간이 생기며, 런타임에서 collector와 issuer의 revision 불일치를 감지하지 않습니다. 같은 SHA 쌍 배포는 배포 스크립트만 보장합니다.

## 잘 된 점

- Go가 helper 프로세스를 단독 소유합니다. 0700 runtime 디렉터리, socket 소유자·권한 검증, 환경변수 허용 목록, SIGTERM 뒤 SIGKILL, `CLEANUP_TIMED_OUT` fatal이 갖춰져 있고, helper 종료를 프로세스 안에서 숨겨 재시작하지 않고 runtime fatal로 드러냅니다.
- 양쪽 모두 exact-key decode, protocol version 검사, 성공·오류 envelope 분리로 fail-closed입니다. Go provider semaphore와 helper `max_inflight`가 같은 설정값을 쓰고 health 응답으로 서로 검증합니다.
- 파서 오류를 요청별 async context로 귀속시켜 동시 호출을 격리하고, 탭 부재 증거를 실제 SDK로 테스트합니다.
- BotGuard JS는 네트워크 없음, 읽기 전용 rootfs, cap 제거, 메모리·PID 상한의 issuer 안에서 실행되며 issuer에는 비밀이 없습니다. broker의 상태기계, 직렬화, 퇴역 처리에서 경쟁 조건이나 누수는 찾지 못했습니다.

## 권장 순서

1. helper는 사실 추출만 하고 판정은 Go가 하는 원칙을 정합니다. 첫 대상은 1번 fan-out입니다.
2. 모르는 badge와 상태를 `parser_drift`로 거부하고(3번), `CANCELLED`의 계약을 한쪽으로 정하며(2번), `classifyPublication`이 대기 상태를 직접 확인하게 합니다(4번).
3. PO 발급 상태를 Go 메트릭으로 내보냅니다(5번).
4. continuation 일시 실패 분류를 transport 실패로 한정하고(6번), 공유 Innertube 초기화를 요청 컨텍스트에서 분리합니다(7번).
5. 공유 wire fixture와 오프라인 왕복 테스트를 추가합니다(9번).
6. Node heap 상한을 명시합니다(10번).

## 실행한 검사

보조 검토에서 실행했으며 모두 통과했습니다.

- `youtubejs/`의 `npm test`: 371개 통과, 실패 0
- `youtubejs/`의 `npm run typecheck`: 오류 없음
- `go test ./internal/runtime/youtubejs/... ./internal/runtime/youtubejscollector/...`: ok
- `go test ./internal/runtime/pobroker/... ./cmd/po-broker/...`: ok
- 같은 pobroker 대상의 `go vet`: 출력 없음, exit 0

## 실행하지 않은 검사와 한계

- `-race`: 리뷰 범위에서 허용한 명령이 아니어서 실행하지 않았습니다.
- `real_data_test.go`: 환경변수와 네트워크가 필요해 자동으로 건너뜁니다.
- SIGTERM 종료 경로, Innertube config 실패, mint 중 취소는 코드 경로로만 확인했고 재현하지 않았습니다.
- systemd start-limit과 seccomp 동작, 운영 메트릭, 실제 YouTube 응답 형상은 확인하지 않았습니다.
- 각 결함이 API 쪽 canonical 판정에 주는 영향은 추론입니다.
