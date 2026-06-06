# YouTube Producer resilience v6

작성일: 2026-06-05 KST
갱신일: 2026-06-06 KST (교차 리뷰 후속 패치 v6.1 반영)

## 적용 범위

이 패치는 단순 scraper retry 보강이 아니라 `youtube-producer` active-active 실행면 전체를 대상으로 한다.

- scraper fetch I/O per-attempt timeout, retry delay cap, Retry-After override
- empty / blocked / response-too-large successful HTTP response taxonomy
- ytInitialData extractor bounded scan 및 invalid candidate skip
- GlobalBudgetLimiter source cooldown write path
- YouTube scraper source-level hard failure의 Valkey 전파
- scheduler local wait 이후 global budget reservation 순서 조정
- Holodex `/users/live` producer-side batch registration 및 fallback scraper budget/cooldown accounting
- live batch persistence path
- YouTube scraper RPM fault envelope에서 Holodex live fallback 반영
- snapshot policy reason 확장
- 관련 단위 테스트 추가

## 핵심 결정

1. `JobRunGuard`는 계속 `(pollerName, channelID)` 중복 실행 방지에 집중한다.
2. `GlobalBudgetLimiter`는 source별 admission/in-flight와 source cooldown gate를 담당한다.
3. local scheduler rate limiter 대기 중에는 global budget slot을 점유하지 않는다.
4. Holodex live path는 channel별 poll 대신 synthetic global batch poller chunk로 실행한다.
5. YouTube가 200으로 반환한 consent/challenge/sorry 페이지는 parser drift가 아니라 blocked response로 본다.
6. source cooldown은 rate-limited, forbidden, blocked response처럼 AP 전체에 영향을 주는 YouTube scraper hard failure에만 기록한다.
7. Holodex live fallback scraper 비용은 `FallbackSourceUnits`로 기록해 fault envelope와 source cooldown reporting에는 반영하되, 정상 Holodex `/users/live` admission은 YouTube scraper cooldown에 막히지 않게 한다.

### v6.1 후속 결정 (2026-06-06)

8. Holodex live fallback의 채널별 실패는 `GetChannelsLiveStatusWithFailures`의 failed map으로 batch poller까지 전파한다. fetch 실패 채널은 "방송 없음"과 구분되어 `markEndedSessions` 대상에서 제외되고, partial 결과는 전체 channel set 캐시에 저장하지 않는다.
9. fallback의 source-level 오류 분류는 채널 순서와 무관하게 동작한다(마지막 오류가 아닌 전체 실패 집합을 스캔).
10. blocked response 시그니처는 도메인 고정 마커만 사용한다 — 일반 단어("captcha", "enable cookies")는 영상 제목/설명 오탐으로 fleet-wide cooldown을 유발하므로 제외하고 `google.com/recaptcha`를 추가.
11. ytInitialData 후보가 전부 invalid JSON이면 `ErrNotFound`를 반환해 parser drift 기록 경로를 보존한다(invalid fallback 반환 금지).
12. readiness의 `source_cooldown` 표시는 기록 시점의 TTL을 기억하고 만료 시 자동 해제한다 — fallback 전용 source처럼 reserve가 다시 돌지 않는 topology에서도 stale 표시가 남지 않는다.
13. body read 오류의 too-large 분류는 typed sentinel(`jsonutil.ErrBodyTooLarge`)만 사용한다(문자열 휴리스틱 제거).

## 롤아웃 확인

1. 배포 전 아래 5단계 패키지 테스트를 레포 루트에서 통과시킨다.
   ```bash
   (cd hololive/hololive-shared && go test ./internal/retry)
   (cd hololive/hololive-shared && go test ./pkg/service/youtube/scraper/internal/initialdata ./pkg/service/youtube/scraper/internal/scraping)
   (cd hololive/hololive-shared && go test ./pkg/service/youtube/poller/internal ./pkg/service/youtube/poller/internal/pollers)
   (cd hololive/hololive-youtube-producer && go test ./internal/runtime/polling)
   (cd hololive/hololive-youtube-producer && go test ./internal/runtime/internal/producerruntime)
   ```
2. 배포 직후 각 AP의 `/readyz`에서 `budget_backend_available=true`를 확인한다.
3. Holodex live batch가 켜진 환경에서는 synthetic `live_batch` registration이 생성되고, steady YouTube scraper RPM은 증가하지 않으면서 fault envelope에 fallback request units가 합산되는지 로그의 `youtube_producer_combined_budget_summary`로 확인한다.
4. YouTube scraper hard failure가 발생한 경우 Valkey source cooldown key TTL이 설정되고, `/readyz`의 `source_cooldown=true`, `affected_sources`에 해당 source가 표시되는지 확인한다.
5. source cooldown report write는 bounded timeout으로 수행되어야 하며, Valkey 지연이 poll worker를 장시간 붙잡지 않아야 한다.

## 롤백 기준

- `/readyz`에서 `budget_backend_available=false`가 지속되거나, source cooldown이 정상 TTL 이후에도 해제되지 않으면 직전 배포로 되돌린다.
- Holodex live batch registration이 누락되거나 live persistence가 채널별로 중복/누락되면 직전 배포로 되돌린다.
- YouTube scraper RPM summary가 운영 budget을 초과하거나 fault envelope warning이 정상 poll interval에서도 지속되면 poll interval을 보수적으로 늘리거나 직전 배포로 되돌린다.
- parser drift와 blocked response 분류가 섞여 source cooldown이 과도하게 발생하면 직전 scraper 분류로 되돌린다.

## 잔여 리스크

- Holodex `/users/live` batch provider는 Holodex API 실패 시 기존 Holodex service 계약에 따라 YouTube producer scraper fallback을 사용할 수 있다. 이 fallback은 YouTube scraper fault envelope와 source cooldown 경로에 반영되며, 반복 발생 시 Holodex fallback 로그와 budget warning을 함께 확인해야 한다.
- Live batch partial failure는 실패 채널을 error map으로 반환하고 성공 채널 persistence는 유지한다. v6.1부터 이 계약은 Holodex fallback 계층까지 적용된다 — fallback에서 일부 채널만 실패해도 해당 채널은 failed map으로 batch poller에 전달되어 live session이 오종료되지 않는다. 재시도 시 이미 성공한 채널과 실패 채널이 섞일 수 있으므로 duplicate-safe persistence와 `ON CONFLICT` 동작을 전제로 한다.
- legacy `GetChannelsLiveStatus` 호출자(alarm-worker 등)는 partial 성공을 그대로 받지만(failed map 무시), v6.1부터 partial 결과는 캐시되지 않으므로 Holodex 장애 + scraper 부분 실패가 지속되면 fallback scraper 호출량이 캐시 시절보다 늘 수 있다. fault envelope budget이 이를 상한으로 관리한다.
