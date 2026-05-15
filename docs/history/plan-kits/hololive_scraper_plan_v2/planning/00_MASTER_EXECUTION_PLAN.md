# Hololive YouTube Scraper 안정화 Master Execution Plan v2

## 0. 이 문서의 목적

이 계획은 “아이디어 목록”이 아니라, 실제 LLM 작업자 또는 개발자가 PR 단위로 구현하고 운영자가 단계적으로 배포할 수 있도록 만든 실행 계획입니다.

이전 diff pack은 코드 변경안을 phase별로 제공했습니다. v2는 그 위에 다음을 보강합니다.

- 작업 의존성 그래프
- PR 순서와 PR별 merge gate
- 테스트/CI 매트릭스
- 운영 rollout/rollback 기준
- metric/log/dashboard 설계
- browser/OCR 진단 경계
- 실패 시 원인별 대응 runbook
- LLM 작업자용 phase ticket
- 위험 등록부와 의사결정 기록

## 1. 전체 목표

현재 YouTube scraping 로직의 문제를 “차단 우회”로 풀지 않고, 다음 6가지 축으로 안정화합니다.

1. 실패 원인 분류
   - 403/429
   - timeout
   - transport
   - parser drift
   - empty response
   - channel unavailable
   - cooldown
   - unknown

2. adaptive backoff
   - 전역: 403/429 hard cooldown
   - 채널/source별: parser drift, timeout, transport, http status

3. source tiering
   - cached state / DB
   - RSS
   - HTML scraper
   - YouTube Data API fallback
   - raw HTML fixture capture
   - browser diagnostic snapshot

4. parser drift 대응
   - raw HTML snapshot
   - fixture 승격
   - parser test 추가
   - release gate

5. active channel priority
   - active/warm/cold channel tier
   - 전체 RPM 절감
   - latency-sensitive channel 우선

6. partial success 관찰성
   - 어떤 source가 실패했는지
   - 어떤 source가 복구했는지
   - API fallback이 quota를 얼마나 사용했는지
   - fallback 후에도 미복구된 채널은 무엇인지

## 2. 비목표

다음은 이번 안정화 작업의 목표가 아닙니다.

- YouTube 차단 우회 자동화
- CAPTCHA 또는 login wall 우회
- OCR을 운영 데이터 source로 사용하는 것
- browser rendering을 기본 수집 경로로 전환하는 것
- proxy rotation을 공격적으로 강화하는 것
- API fallback을 전체 채널 대상으로 확대하는 것

## 3. 변경 원칙

### 원칙 1. 기존 안정 구조는 유지합니다.

현재 upcoming 흐름은 scraper 우선, 실패 채널만 API fallback입니다. 이 방향은 맞습니다. 바꿔야 할 것은 수집 순서가 아니라 “실패가 관찰 가능한지”입니다.

### 원칙 2. 403/429는 전역 문제로 봅니다.

403/429는 특정 채널의 HTML 구조 문제가 아니라 요청 경로, rate limit, IP, YouTube 정책 문제일 가능성이 큽니다. 기존 hard backoff를 유지합니다.

### 원칙 3. parser drift는 채널/source 문제로 봅니다.

parser drift는 특정 page type, 특정 channel layout, 특정 source에서 발생할 수 있습니다. 그래서 channel/source health로 분리합니다.

### 원칙 4. snapshot은 실패 분석 도구입니다.

snapshot은 정상 수집 데이터가 아닙니다. parser drift 대응을 빠르게 하기 위한 artifact입니다.

### 원칙 5. browser는 진단 source입니다.

browser rendered HTML/screenshot은 “왜 parser가 깨졌는지” 확인하기 위한 최후 진단 source입니다. 기본 poller 경로가 아닙니다.

## 4. 전체 PR 시퀀스

| PR | 이름 | 목적 | 배포 가능성 | Rollback 난이도 |
|---|---|---|---|---|
| PR-01 | Failure taxonomy | 실패 reason 타입화 | 높음 | 낮음 |
| PR-02 | Parser drift error | parser drift sentinel error | 높음 | 낮음 |
| PR-03 | Channel health | channel/source adaptive backoff | 중간 | 중간 |
| PR-04 | Upcoming observability | scrape/API fallback reason/recovery 기록 | 높음 | 낮음 |
| PR-05 | Snapshot capture | raw HTML fixture 저장 | 중간 | 낮음 |
| PR-06 | Config/runtime wiring | env/config 연결 | 높음 | 낮음 |
| PR-07 | Extend operations | videos/shorts/community/stats 확장 | 중간 | 중간 |
| PR-08 | Browser diagnostic | browser snapshot 진단 path | 낮음/옵션 | 낮음 |
| PR-09 | Active channel tiering | active/warm/cold cadence | 중간 | 중간 |
| PR-10 | Runbook/dashboard | 운영 체계 완성 | 높음 | 낮음 |

## 5. 의존성 그래프

```text
PR-01 Failure taxonomy
  └─ PR-02 Parser drift error
       ├─ PR-03 Channel health
       ├─ PR-04 Upcoming observability
       │    └─ PR-05 Snapshot capture
       │         └─ PR-06 Config/runtime wiring
       └─ PR-07 Extend operations
             └─ PR-08 Browser diagnostic

PR-03 + PR-04 + PR-06
  └─ PR-09 Active channel tiering

All PRs
  └─ PR-10 Runbook/dashboard
```

## 6. Phase별 핵심 산출물

### PR-01/02 산출물

- `scraper.FailureReason`
- `scraper.FailureSource`
- `scraper.FailureDetail`
- `scraper.ClassifyFailure`
- `scraper.ErrParserDrift`
- `scraper.ParserDriftError`
- 403/429 Retry-After 보존

### PR-03 산출물

- `ChannelSourceHealth`
- `ChannelHealthStore`
- `fetchChannelSourcePage`
- `recordChannelSourceSuccess`
- `recordChannelSourceFailure`
- `recordParserDrift`

### PR-04 산출물

- `upcomingScrapeFailure`
- `upcomingScrapeResult.failures`
- `upcomingAPIFallbackResult.successfulIDs`
- `upcomingAPIFallbackResult.failedIDs`
- `observeYouTubeScraperFailure`
- `observeYouTubeScraperRecovery`

### PR-05/06 산출물

- `Snapshot`
- `SnapshotSink`
- `FileSnapshotSink`
- snapshot interval limiter
- config/env wiring
- snapshot default OFF

### PR-07 산출물

- recent videos HTML/RSS guard
- shorts guard
- community guard
- channel stats/snippet guard
- parser drift snapshot 확대

### PR-08 산출물

- `BrowserSnapshotFetcher`
- `CaptureBrowserDiagnosticSnapshot`
- browser default path 차단
- OCR 비사용 정책

### PR-09 산출물

- active/warm/cold classifier
- tiered registrations
- RPM budget recalculation
- tier별 metric/log

## 7. 구현 완료 기준

기능적으로 다음이 모두 만족되어야 합니다.

- scraper 실패가 reason/source로 분류됩니다.
- 403/429는 전역 hard backoff로 남습니다.
- parser drift는 channel/source health에 남습니다.
- upcoming API fallback은 어떤 실패를 복구했는지 기록합니다.
- parser drift snapshot이 제한적으로 생성됩니다.
- snapshot feature는 기본 OFF입니다.
- browser feature는 기본 OFF이며 기본 fetcher path가 아닙니다.
- active/warm/cold tiering 적용 후 전체 RPM이 감소합니다.
- CI에서 scraper/youtube/poller/runtime 테스트가 통과합니다.
- 운영자가 metric만 보고 “IP 차단인지, parser drift인지, timeout인지”를 구분할 수 있습니다.

## 8. 배포 전 필수 질문

각 PR merge 전 아래 질문에 답해야 합니다.

1. 이 PR은 default OFF인가, default ON인가?
2. default ON이면 기존 동작을 바꾸는가?
3. 장애 시 env 하나로 끌 수 있는가?
4. API quota 증가 가능성이 있는가?
5. Prometheus label cardinality가 늘어나는가?
6. snapshot 또는 artifact가 디스크를 채울 수 있는가?
7. browser 컨테이너 의존성이 생기는가?
8. partial success 정책을 바꾸는가?
9. live/upcoming latency가 나빠질 수 있는가?
10. rollback 시 state key를 정리해야 하는가?

## 9. 가장 중요한 구현 순서

가장 안전한 순서는 다음입니다.

```text
1. 실패 분류만 넣기
2. 로그/metric으로 관찰하기
3. upcoming fallback 관찰성 추가
4. channel health를 parser drift에만 제한적으로 켜기
5. snapshot을 parser drift에만 켜기
6. videos/shorts/community로 확장하기
7. browser diagnostic 수동 path만 추가하기
8. active/warm/cold tiering 추가하기
```

이 순서를 지키면 “갑자기 polling cadence가 바뀌는 문제”, “API quota가 예상보다 많이 쓰이는 문제”, “브라우저가 기본 경로로 들어가는 문제”를 피할 수 있습니다.
