# LLM 작업자용 프롬프트

아래 지시를 그대로 사용해 phase별로 PR을 만들 수 있습니다.

## 공통 지시

당신은 `park285/hololive-bot` repository의 Go 코드 작업자입니다. 아래 phase 파일 하나만 적용하세요. 다른 phase를 미리 구현하지 마세요. 각 phase가 끝날 때 다음을 수행하세요.

1. 변경 파일 목록을 요약합니다.
2. `go test` 결과를 첨부합니다.
3. compile error가 있으면 phase 파일의 의도를 유지하면서 최소 수정합니다.
4. metric label에 `channel_id`를 넣지 않습니다.
5. browser/OCR을 기본 scraping path로 넣지 않습니다.
6. 403/429는 전역 hard backoff를 유지합니다.
7. parser drift/timeout/transport는 channel/source health로 분리합니다.

## Phase별 작업 순서

1. `phase-01-failure-taxonomy.md`
2. `phase-02-channel-health-operation-guard.md`
3. `phase-03-upcoming-observability-and-api-fallback.md`
4. `phase-04-raw-fixture-snapshot-capture.md`
5. `phase-05-config-runtime-wiring.md`
6. `phase-06-extend-all-scraper-operations.md`
7. `phase-07-browser-diagnostic-fetcher.md`
8. `phase-08-active-channel-tiering-rollout-tests.md`

## PR 제목 예시

- `youtube scraper: classify failures and parser drift`
- `youtube scraper: add channel source health backoff`
- `youtube upcoming: observe scraper/API fallback recovery`
- `youtube scraper: capture parser drift HTML snapshots`
- `youtube scraper: wire snapshot and health config`
- `youtube scraper: extend operation guards`
- `youtube scraper: add browser diagnostic snapshot path`
- `stream ingester: add active channel polling tiers`

## 리뷰 체크리스트

- `go test ./hololive/hololive-shared/pkg/service/youtube/scraper`
- `go test ./hololive/hololive-shared/pkg/service/youtube`
- `go test ./hololive/hololive-shared/pkg/service/youtube/poller`
- `go test ./hololive/hololive-stream-ingester/internal/runtime`
- Prometheus label cardinality 확인
- snapshot default OFF 확인
- browser default path 비활성 확인
- API fallback quota 계산 유지 확인
