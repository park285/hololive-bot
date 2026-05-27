# Hololive Kakao Bot Go - Conventions

현재 `hololive/hololive-kakao-bot-go` 코드베이스에 맞춘 작업 기준 문서입니다.
코드를 추가/수정하기 전에 이 문서를 우선 참고하세요.

---

## 1. 현재 모듈 역할

이 모듈은 **카카오톡 봇 ingress + command routing runtime**입니다.

현재 실제 런타임/도구 엔트리포인트:

- `cmd/bot`
  - 메인 봇 런타임
- `cmd/test_db_integration`
  - PostgreSQL / member repository 점검용
- `cmd/tools/fetch_profiles`
  - 프로필 수집 도구
- `cmd/tools/verifyhash`
  - 해시 검증 도구
- `cmd/tools/warm_member_cache`
  - member cache warm-up 도구

중요:

- **Admin API는 이 모듈의 책임이 아닙니다.**
- 운영용 `/api/holo/*`, `/api/auth/*`, `/oauth/callback` 제어 plane은 `hololive-admin-api`가 담당합니다.
- 이 모듈의 HTTP 라우터는 현재 `internal/app/http/ProvideBotRouter` 기준으로
  - `/webhook/iris`
  - shared trigger internal route
  만 노출합니다.

---

## 2. 현재 패키지 경계

### `internal/app/`

애플리케이션 조립 / 런타임 / wiring 계층입니다.

주요 역할:

- `app.BuildRuntime(...)`
- `app.Build(...)`
- runtime lifecycle
- HTTP server start / shutdown
- bootstrap / dependency assembly
- wiring container build

하위 역할:

- `internal/app/bootstrap/`
  - 서비스 조립 / provider wiring
- `internal/app/http/`
  - bot 전용 router 제공
- `internal/app/runtime/`
  - HTTP server / lifecycle helper
- `internal/app/wiring/`
  - container / dependency views

규칙:

- 새 의존성 조립은 `internal/app/bootstrap` 또는 `internal/app/wiring`에 둡니다.
- 런타임 생명주기 코드는 `internal/app/runtime` 또는 상위 `internal/app` wrapper에 둡니다.
- HTTP route 추가는 먼저 **이 모듈 책임 범위인지** 확인하세요.

### `internal/bot/`

봇 핵심 오케스트레이션 계층입니다.

주요 역할:

- `Bot`
- `MessageIngress`
- `CommandRouter`
- `CommandTransport`
- `BotLifecycle`
- dependency validation / command initialization

규칙:

- ingress 필터링(ACL, self sender skip, command parse)은 여기서 처리합니다.
- command handler가 Iris transport를 직접 다루지 않게 유지합니다.
- lifecycle / readiness / shutdown 흐름은 `bot` 계층에서 관리합니다.

### `internal/adapter/`

외부 메시지 표현 계층입니다.

주요 역할:

- `MessageAdapter`
- `ResponseFormatter`
- message parser
- formatter
- user-facing message constant

규칙:

- 사용자 응답 문자열은 가능한 한 이 계층에서 조합합니다.
- command handler에 긴 사용자 메시지 포맷을 직접 박지 않습니다.
- 새 명령어를 추가할 때는 parser / formatter / message constant를 함께 맞춥니다.

### `internal/command/`

명령어 비즈니스 진입점입니다.

주요 역할:

- `Registry`
- `Dispatcher`
- command handlers (`handler_*`)
- member info / member news command logic

규칙:

- 새 명령은 `Registry`에 등록 가능한 `Command` 구현으로 추가합니다.
- 명령 키 해석 / 라우팅은 registry + normalizer를 사용합니다.
- command handler는 데이터 조회/행동 수행에 집중하고, transport 세부사항은 bot/adapter에 맡깁니다.

### `internal/service/`

봇 전용 보조 서비스 계층입니다.

현재 핵심 패키지:

- `service/matcher`
  - 멤버명/별칭 매칭
- `service/streamfeed`
  - org 단위 live/upcoming feed 결합
- `service/streamschedule`
  - 단일 채널 schedule 결합
- `service/streamcommon`
  - stream merge / match helper
- `service/majoreventclient`
  - `hololive-llm-sched` major event internal API client
- `service/membernewsclient`
  - `hololive-llm-sched` member news internal API client

규칙:

- 외부 서비스 호출 래핑은 `*_client` 패키지에 둡니다.
- stream merge 규칙은 `streamcommon`을 우선 재사용합니다.
- 멤버명 검색/정규화는 ad-hoc로 짜지 말고 `matcher`를 통합니다.

---

## 3. 현재 실제 메시지 흐름

메시지 처리는 현재 대략 아래 순서입니다.

1. Iris webhook 수신
2. `internal/app/http/ProvideBotRouter`
3. `bot.MessageIngress`
4. `adapter.MessageAdapter.ParseMessage`
5. `bot.CommandRouter`
6. `command.Registry.Execute`
7. command handler
8. `adapter.ResponseFormatter`
9. `bot.CommandTransport`
10. Iris 송신

규칙:

- self sender 판정, ACL 차단, unknown command drop은 ingress에 둡니다.
- parser 단계 전에 명령어 문자열을 제각각 정규화하지 않습니다.
- formatter 없이 handler에서 최종 응답 문자열을 직접 완성하는 방향을 피합니다.

---

## 4. 현재 선호 유틸리티

## `hololive-shared/pkg/util`

현재 이 모듈과 가장 밀접한 공용 유틸 함수는 여기 있습니다.

```go
ToKST(t)
FormatKST(t, layout)
NowKST()
MinutesUntilFloorPtr(target, reference)
FormatKoreanNumber(n)
NormalizeSuffix(s)
ApplyKakaoSeeMorePadding(text, instruction)
IsValkeyNil(err)
NewCircuitBreaker(failureThreshold, resetTimeout, healthCheckInterval, healthCheckFn, logger)
```

주의:

- 과거 문서에 있던 `MinutesUntilCeil` 기준으로 쓰지 마세요.
- 현재 코드는 `MinutesUntilFloorPtr`를 기준으로 맞춥니다.

## `shared-go/pkg/stringutil`

문자열 정규화는 ad-hoc보다 이 패키지를 우선합니다.

```go
Normalize(s)
NormalizeKey(s)
TrimSpace(s)
Slugify(s)
ContainsString(slice, item)
StripLeadingHeader(text, header)
TruncateString(s, maxRunes)
```

특히:

- command key / sender / member query 정규화 → `Normalize`
- 검색용 key → `NormalizeKey`
- 공백 정리 → `TrimSpace`

## `shared-go/pkg/httputil`

내부 API client는 직접 `http.Client`를 반복 구현하지 말고 `JSONClient`를 우선 사용합니다.

현재 사용 위치:

- `internal/client/majorevent`
- `internal/client/membernews`

## `shared-go/pkg/runtime/*`

런타임 시작/종료 공통 로직은 여기와 맞춥니다.

주요 패키지:

- `runtime/bootstrap`
- `runtime/lifecycle`
- `runtime/automaxprocs`

## `shared-go/pkg/workerpool`

비동기/병렬 처리용 worker pool은 직접 구현보다 이 패키지를 우선합니다.

---

## 5. 현재 핵심 상수 소스

이 모듈은 더 이상 `internal/constants`를 갖고 있지 않습니다.
상수는 대부분 `hololive-shared/pkg/constants`에서 가져옵니다.

특히 자주 보는 그룹:

### Cache / scheduling

- `constants.CacheTTL`
  - `LiveStreams`
  - `UpcomingStreams`
  - `ChannelSchedule`
  - `ChannelInfo`
  - `ChannelSearch`
  - `NextStreamInfo`
  - `NotificationSent`
  - `TwitchNotification`

### Timeout / server

- `constants.AppTimeout`
  - `Build`
  - `Shutdown`
- `constants.RequestTimeout`
  - `AdminRequest`
  - `BotCommand`
  - `WebhookProcessing`
  - `AlarmService`
- `constants.IrisConnection`
  - `ReadyTimeout`
  - `RetryInterval`
  - `PingTimeout`

### Stream / alarm policy

- `constants.HolodexAPIParams`
- `constants.ChzzkConfig`
- `constants.DefaultTargetMinutes`
- `constants.Tier*Window`, `Tier*Interval`

규칙:

- 숫자/시간 리터럴을 새로 박기 전에 `constants`에 이미 있는지 먼저 확인합니다.
- org 문자열(`Hololive`, `Stellive`, `all`)도 raw string보다 상수를 우선합니다.

---

## 6. 현재 코드베이스에서 중요한 서비스 경로

### Stream 목록 / 일정

- org 단위 live/upcoming → `internal/service/streamfeed`
- 채널 schedule → `internal/service/streamschedule`
- stream merge helper → `internal/service/streamcommon`

규칙:

- Chzzk + YouTube 병합 로직을 새로 짜지 말고 기존 서비스/헬퍼를 재사용합니다.
- Stellive/Chzzk 확장 시 `streamfeed` / `streamschedule`의 merge 규칙을 우선 수정합니다.

### Member matching

- 이름/별칭/토큰 검색 → `internal/service/matcher`

규칙:

- 명령 핸들러에서 멤버 검색을 직접 구현하지 않습니다.
- `matcher.MemberMatcher`와 snapshot cache 흐름을 유지합니다.

### Major event / member news

- major event internal API client → `internal/client/majorevent`
- member news internal API client → `internal/client/membernews`

규칙:

- internal API 호출 contract 변경 시 client + command handler + formatter를 함께 갱신합니다.

---

## 7. formatter / message 규칙

현재 코드 기준으로 사용자 메시지 표현은 `internal/adapter`가 source of truth에 가깝습니다.

핵심 요소:

- `ResponseFormatter`
- `MessageBuilder`
- `messages.go`
- `formatter_*`
- `message_parser_*`

규칙:

- 새 사용자 메시지 카피는 가능하면 `adapter/messages.go` 또는 formatter 계층에 둡니다.
- command handler에서 한국어 문장을 길게 직접 조합하지 않습니다.
- Kakao “전체보기” 처리는 `ApplyKakaoSeeMorePadding` 기반 패턴을 우선 따릅니다.

---

## 8. command 추가/수정 규칙

새 명령어를 추가할 때의 기본 순서:

1. `internal/adapter/message_parser_*.go`
2. `internal/command/handler_*.go`
3. `internal/adapter/formatter_*.go`
4. 필요 시 `internal/adapter/messages.go`
5. `bot` 초기화 경로에서 registry 등록 확인
6. 관련 테스트 추가

규칙:

- parser 없이 handler만 추가하지 않습니다.
- formatter 없이 handler에서 직접 응답 문자열을 완결하지 않습니다.
- `command.Registry` / `ErrUnknownCommand` 흐름을 우회하지 않습니다.

---

## 9. runtime / wiring 규칙

현재 DI와 runtime 초기화는 수동 조립 기반입니다.

핵심 함수:

- `app.BuildRuntime`
- `app.Build`
- `app.ProvideBotRouter`
- `appwiring.BuildContainer`

규칙:

- 새 dependency는 `internal/app/bootstrap` 또는 `internal/app/wiring`에서 조립합니다.
- `cmd/bot/main.go`는 bootstrap entrypoint로 유지하고, 조립 로직을 넣지 않습니다.
- runtime wrapper와 실제 서비스 조립을 섞지 않습니다.

---

## 10. 현재 검증 명령

기본 검증:

```bash
cd hololive/hololive-kakao-bot-go
make fmt
make lint
make test
```

상황별:

```bash
go test ./internal/adapter/...
go test ./internal/bot/...
go test ./internal/command/...
go test ./internal/service/...
go build ./cmd/bot
go build ./cmd/tools/...
```

주의:

- subtree AGENTS 기준으로 `make fmt`, `make lint`, `make test`가 primary flow입니다.

---

## 11. 현재 코드베이스에 맞춘 금지 사항

다음은 현재 코드베이스 기준으로 **다시 만들지 말아야 하는 것**들입니다.

- `internal/util` 재도입
  - 현재 공용 유틸은 `hololive-shared/pkg/util` 및 `shared-go/pkg/*`
- `internal/config`, `internal/server`, `internal/repository`, `internal/platform` 같은 레거시 모놀리스 계층 재도입
- Admin API 책임을 이 모듈 router에 다시 섞는 것
- command handler 안에서 직접 Iris transport 호출
- 멤버 검색 로직을 handler마다 중복 구현
- stream merge 규칙을 service 밖에서 ad-hoc로 구현
- 사용자 메시지 문자열을 command handler에 직접 대량 하드코딩
- `http.Client` 직접 조립을 반복해 internal API client를 새로 만드는 것

---

## 12. 빠른 작업 가이드

### “새 명령을 추가”

- parser → command handler → formatter → registry 흐름으로 추가

### “메시지 카피를 수정”

- `internal/adapter/messages.go` / `formatter_*` 먼저 확인

### “멤버 검색 정확도를 바꾸고 싶다”

- `internal/service/matcher` 수정

### “Chzzk/YouTube 일정 병합을 바꾸고 싶다”

- `internal/service/streamfeed` / `internal/service/streamschedule` / `streamcommon` 수정

### “런타임 의존성 추가”

- `internal/app/bootstrap` / `internal/app/wiring`에서 조립

### “HTTP route 추가”

- 먼저 이 모듈 책임인지 확인
- bot runtime 책임이면 `internal/app/http`
- admin/control plane이면 보통 `hololive-admin-api`

