# Hololive YouTube Scraper 안정화 구현 패키지

이 패키지는 `park285/hololive-bot`의 현재 `main` 기준으로 작성한 phase별 코드 작업 지시서입니다.

목표는 단순히 “브라우저 컨테이너를 붙이는 것”이 아니라, 다음 순서로 스크래핑 파이프라인을 안정화하는 것입니다.

1. 실패 원인을 타입으로 분류합니다.
2. 채널별/source별 adaptive backoff를 넣습니다.
3. upcoming scraper → API fallback 흐름에 partial success 관찰 정보를 남깁니다.
4. parser drift 시 raw HTML fixture를 저장합니다.
5. Chrome/browser snapshot은 진단용 저빈도 fallback으로만 제한합니다.
6. active/warm/cold channel tiering을 후속 단계로 연결합니다.

현재 코드에서 확인된 기반:

- `service_upcoming_scrape.go`: scraper 우선 실행.
- `service_upcoming_fallback.go`: 실패 채널만 YouTube API fallback.
- `fetcher.go`: `pageFetcher` 인터페이스와 `nethttp`/`goscrapy` fetcher 구조.
- `backoff_state.go`: 전역 hard/transient backoff.
- `state_manager.go`: channel별 cache state store.
- `scheduler_reschedule.go`: `RetryDelay()` 기반 next run delay 지원.

권장 적용 순서:

| Phase | 파일 | 목적 | 각 phase 단독 컴파일 기대 |
|---|---|---|---|
| 01 | `phase-01-failure-taxonomy.md` | 실패 reason 타입화, 403/429 Retry-After 보존, parser drift error 추가 | 예 |
| 02 | `phase-02-channel-health-operation-guard.md` | 채널/source별 adaptive backoff와 operation guard 추가 | 예 |
| 03 | `phase-03-upcoming-observability-and-api-fallback.md` | upcoming scrape/API fallback에 reason, source, recovery 기록 | 예 |
| 04 | `phase-04-raw-fixture-snapshot-capture.md` | parser drift HTML snapshot 저장 | 예 |
| 05 | `phase-05-config-runtime-wiring.md` | env/config/runtime wiring | 예 |
| 06 | `phase-06-extend-all-scraper-operations.md` | videos/shorts/community/channel stats까지 guard 적용 | 예 |
| 07 | `phase-07-browser-diagnostic-fetcher.md` | Chrome/browser 진단 fetcher를 낮은 QPS로 제한 | 부분 구현/옵션 |
| 08 | `phase-08-active-channel-tiering-rollout-tests.md` | active/warm/cold polling tier와 rollout/test 전략 | 설계+작업 지시 |

LLM 작업자에게 전달할 때는 phase 파일 하나씩 맡기면 됩니다. 각 파일은 다음 형식으로 구성되어 있습니다.

- 작업 의도
- 코드 레벨 의사결정
- 변경 대상 파일
- diff 수준 변경안
- 테스트
- 완료 기준

주의점:

- metric label에는 `channel_id`를 넣지 않습니다. channel별 상세는 log/state store에 남깁니다.
- 403/429는 전역 hard backoff로 유지합니다.
- parser drift/timeout/transport는 channel/source health로 관리합니다.
- browser/OCR은 운영 수집 source가 아니라 진단 source입니다.
- snapshot은 크기 제한, reason 제한, interval 제한이 반드시 필요합니다.
