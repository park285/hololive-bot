# Legacy Lint Cleanup Record (2026-04-15)

상태: **CLOSED**

범위:

- `hololive-kakao-bot-go/internal/service/acl/...`
- `hololive-kakao-bot-go/internal/service/twitch/...`
- `hololive-shared/pkg/providers/...`
- `hololive-shared/pkg/service/youtube/...`

원칙:

- `//nolint` suppress 주석 추가 없음
- `.golangci.yml` 예외 추가 없음
- 실제 리팩터링/삭제/테스트 분해로 해소

검증:

- `golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...` → `0 issues`
- `golangci-lint run ./pkg/providers/... ./pkg/service/youtube/...` → `0 issues`
- `go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...` → PASS
- `go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... -count=1` → PASS
- 관련 package test/build green
- suppress 주석 / config exclusion 추가 없음

이 기준은 2026-04-15 세션에서 모두 충족되었고, 본 문서는 종료 기록으로 보존한다.
