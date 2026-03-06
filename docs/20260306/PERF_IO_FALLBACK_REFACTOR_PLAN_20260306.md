# 성능 · I/O · Fallback · Legacy 리팩토링 실행 계획

> 작성일: 2026-03-06  
> 범위: `hololive-bot` Go 코드베이스  
> 방식: 정적 분석 기반 계획 수립 (`pprof`/실부하 측정 미포함)

---

## 1. 목표

다음 세션부터 리팩토링을 **작업 단위별로 바로 착수**할 수 있도록, 현재 코드베이스에서 확인된 문제를 아래 4축으로 정리한다.

1. **I/O 왕복 감소**
2. **fallback 정책 공통화**
3. **분기/if 복잡도 축소**
4. **legacy/compat 레이어 정리**

핵심 원칙은 다음과 같다.

- 신규 라이브러리 도입보다 **기존 프리미티브 재사용** 우선
- hot path는 **ORM 편의보다 배치 I/O** 우선
- fallback은 각 서비스 인라인 구현 대신 **공통 정책 계층**으로 승격
- legacy 지원은 무기한 유지하지 않고 **제거 일정**을 둔다

---

## 2. 최우선 결론

### 바로 효과가 큰 P0

1. **Redis rate limiter RTT 축소**  
   - 파일: `hololive/hololive-shared/pkg/service/ratelimit/sliding_window.go`
   - 현재: `Allow()` 1회당 다중 Valkey 왕복
   - 목표: **Lua 1회 호출**로 `allowed/current/retry_after` 동시 계산

2. **YouTube poller DB write 배치화**  
   - 파일: `hololive/hololive-shared/pkg/service/youtube/poller/pollers.go`
   - 현재: poll loop에서 개별 insert/update 다수
   - 목표: **`pgx.Batch` 기반 배치 저장소** 분리

3. **Holodex fallback 중복 fetch 제거**  
   - 파일: `hololive/hololive-shared/pkg/service/holodex/scraper.go`
   - 현재: 채널 1개 fallback에도 전체 schedule 페이지 재다운로드/재파싱
   - 목표: **`singleflight + short TTL page cache`** 도입

### 구조 개선 효과가 큰 P1

4. **YouTube/Holodex fallback 공통화**  
   - 파일: `hololive/hololive-shared/pkg/service/youtube/service.go`, `hololive/hololive-shared/pkg/service/holodex/service.go`
   - 목표: 공통 `FallbackPipeline`/`PrimarySecondaryFetcher` 도입

5. **Chzzk 조회 전략 개선**  
   - 파일: `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go`
   - 목표: 전체 라이브 페이지 스캔 대신 target 수 기반 전략 분기

6. **LLM fallback 구조 분리**  
   - 파일: `hololive/hololive-llm-sched/internal/llm/openai_client.go`
   - 목표: transport fallback과 schema별 후처리 분리

### 기술부채 정리에 해당하는 P2

7. **legacy alias/env/shim 제거 계획 실행**
8. **HTML/RSS parser 경량화 재검토**
9. **matcher 전략 체인 구조화**

---

## 3. 작업 스트림별 계획

## Stream A — Redis / Cache / Queue I/O

### A1. SlidingWindowLimiter Lua 통합 (P0)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/ratelimit/sliding_window.go`
- 문제
  - `Allow()`가 현재 2~3회 Valkey 왕복 구조
- 작업
  - `ZREMRANGEBYSCORE + ZCARD + oldest timestamp + allow/deny`를 Lua 스크립트 1회로 합치기
  - 반환값 표준화: `allowed`, `current`, `retry_after_ms`
- 완료 기준
  - limiter 1회 체크 시 Valkey `Do()` 1회
  - 기존 테스트 통과 + retry_after 계산 유지
- 리스크
  - Lua 반환 파싱 실수 시 rate limit 오판 가능
- 검증
  - 단위 테스트 추가: allow/deny/retry_after/동시성/경계 시점

### A2. Cache raw-bytes 경로 검토 (P2)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/cache/service.go`
  - `hololive/hololive-shared/pkg/service/alarm/queue/{publisher,consumer}.go`
- 문제
  - JSON `[]byte ↔ string` 왕복 복사 존재
- 작업
  - 즉시 착수보다는 P0/P1 이후 빈도 높은 경로만 raw API 도입 검토
- 완료 기준
  - 실제 hot path에서만 적용, 범용 API 복잡도 과증가 금지

---

## Stream B — DB ingest / Outbox / Delivery

### B1. YouTube poller 배치 저장소 분리 (P0)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/poller/pollers.go`
  - 신규 후보: `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch.go`
- 문제
  - poll loop의 개별 write가 과다
- 작업
  - hot path만 GORM 밖으로 분리
  - 후보 API
    - `BatchUpsertVideos`
    - `BatchUpsertSessions`
    - `BatchInsertViewerSamples`
    - `BatchUpsertOutbox`
- 완료 기준
  - poll loop 내 개별 `Create/Save/Updates` 호출 수 대폭 감소
  - batch 실패 시 contextual error 유지
- 리스크
  - SQL conflict 처리 누락 시 데이터 정합성 문제
- 검증
  - repository 단위 테스트
  - 기존 poller 테스트 + 충돌/중복 입력 케이스 추가

### B2. Delivery dispatcher 제한 병렬화 (P1)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/delivery/dispatcher.go`
  - 필요시 `shared-go/pkg/workerpool/pool.go` 재사용
- 문제
  - batch를 가져와도 순차 발송
- 작업
  - bounded parallel send 도입
  - `MaxInFlight` 설정 추가
- 완료 기준
  - batch 처리 시간이 단일 느린 room에 과도하게 종속되지 않음
- 리스크
  - 같은 room 순서 보장 필요 여부 확인 필요
- 검증
  - per-room ordering 요구 확인 후 테스트 추가

---

## Stream C — Holodex / YouTube 외부 호출 경로

### C1. Holodex official schedule page cache (P0)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/holodex/scraper.go`
- 문제
  - 채널 단건 fallback에도 전체 페이지 재다운로드/재파싱
- 작업
  - `singleflight.Group` 추가
  - 짧은 TTL의 page/result cache 추가
  - 가능하면 `map[channelID][]*domain.Stream` 재사용
- 완료 기준
  - 같은 시점 다중 fallback에서 외부 fetch 1회로 수렴
- 리스크
  - 너무 긴 TTL은 stale 데이터 유발
- 검증
  - 동시 요청 테스트
  - TTL 만료 전/후 동작 테스트

### C2. Holodex org 조회 병렬도 재조정 (P1)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/holodex/service.go`
- 문제
  - `org=all` 시 `Parallelism: 1`
- 작업
  - distributed/local rate limiter 전제하에 병렬도 2~3 검토
- 완료 기준
  - `all` 조회 latency 감소
- 리스크
  - upstream rate limit 악화 가능
- 검증
  - 테스트 + 설정값으로 병렬도 제어

### C3. YouTube fallback 공통화 (P1)
- 대상 파일
  - `hololive/hololive-shared/pkg/service/youtube/service.go`
- 문제
  - `GetUpcomingStreams`, `GetChannelStatistics`가 동일한 primary→fallback 흐름 중복
- 작업
  - `PrimarySecondaryFetcher[T]` 또는 `FallbackPipeline` 추출
  - quota check / metrics / partial-result 정책 분리
- 완료 기준
  - 중복된 fallback 제어 코드 축소
- 검증
  - primary hit / fallback hit / blocked / partial / total fail 케이스 테스트

---

## Stream D — Chzzk / LLM / API 클라이언트

### D1. Chzzk 조회 전략 개선 (P1)
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/service/chzzk/client.go`
- 문제
  - `GetLivesByChannelIDs()`가 전체 lives 페이지를 넘기며 target 탐색
- 작업
  - target 수 기준 전략 분리
    - 소수 채널: `GetLiveStatus` 병렬
    - 다수 채널: `GetLives` page scan 유지
  - short snapshot cache 검토
- 완료 기준
  - 소수 채널 조회 시 불필요한 전체 페이지 순회 제거

### D2. LLM fallback 구조 분리 (P1)
- 대상 파일
  - `hololive/hololive-llm-sched/internal/llm/openai_client.go`
- 문제
  - Responses→Chat fallback + `event_summary` sanitize가 한 함수에 섞임
- 작업
  - `TransportFallbackPolicy`
  - `PostProcessPolicy`
  - schema별 후처리는 caller 또는 strategy로 이동
- 완료 기준
  - transport 책임과 도메인 규칙 분리

### D3. 공통 HTTP client profile 정리 (P1)
- 대상 파일
  - `shared-go/pkg/httputil/client.go`
  - 레포 전반 raw `http.Client` 생성 지점
- 문제
  - timeout/transport/keepalive 정책이 서비스별로 제각각
- 작업
  - `ExternalAPI`, `Scraper`, `InternalService`, `LLM` profile 정의 검토
- 완료 기준
  - 신규 클라이언트 생성 경로 표준화

---

## Stream E — Legacy / Compat / Complexity

### E1. 제거 대상 inventory 확정 (P1~P2)
- 주요 대상
  - `hololive/hololive-dispatcher-go/internal/app/config.go`
  - `hololive/hololive-shared/pkg/config/config.go`
  - `hololive/hololive-shared/pkg/config/config_types.go`
  - `hololive/hololive-shared/pkg/contracts/subscription/types.go`
  - `hololive/hololive-shared/pkg/service/youtube/stats_repository_aliases.go`
  - `hololive/hololive-shared/internal/dbx/client.go`
  - `hololive/hololive-shared/pkg/providers/iris_sender_adapter.go`
- 작업
  - deprecated env/type/adapter/shim 목록화
  - 제거 순서와 유예기간 정의
- 완료 기준
  - “바로 제거”, “1릴리즈 후 제거”, “유지” 분류 완료

### E2. matcher 전략 체인화 (P2)
- 대상 파일
  - `hololive/hololive-kakao-bot-go/internal/service/matcher/matcher.go`
- 작업
  - `[]MatchStrategy` 기반으로 exact/partial/alias 전략 등록형 전환
- 완료 기준
  - 신규 전략 추가 시 분기 수정 범위 축소

---

## 4. 다음 세션 권장 착수 순서

### Step 1 — P0만 먼저 수행
1. `sliding_window.go` Lua 단일화
2. `holodex/scraper.go` singleflight + TTL cache
3. `youtube/poller/pollers.go` write path batch 분리

### Step 2 — fallback 구조 정리
4. `youtube/service.go` 공통 fallback executor 초안
5. `holodex/service.go` 공통 executor 연결

### Step 3 — 운영/호환성 정리
6. Chzzk 전략 개선
7. LLM fallback 구조 분리
8. legacy alias/env 제거 로드맵 확정

---

## 5. 완료 정의

이 계획의 완료는 문서 작성이 아니라 아래 상태를 의미한다.

- P0 3건이 코드 반영 + 테스트 완료
- fallback 공통 계층 초안이 shared에 존재
- legacy 제거 대상이 inventory + deadline을 가진 상태
- 다음 세션 작업자가 문서만 보고 바로 착수 가능

---

## 6. 다음 세션 체크리스트

- [ ] `sliding_window.go` 기존 테스트와 호출 흐름 재확인
- [ ] Holodex scraper에 cache/singleflight 넣을 구조 포인트 선정
- [ ] YouTube poller에서 GORM write hot path만 먼저 추출
- [ ] P0 구현 후 benchmark 또는 최소한 호출 수 비교 로그 추가
- [ ] 이후 fallback 공통화 설계로 이동

