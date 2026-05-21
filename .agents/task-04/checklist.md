# Task 04 — pkg/logging/log.go + pkg/logging/id.go 단위 테스트

각 `- [x]` flip 시 해당 verification 명령의 핵심 출력 요지를 `> evidence: …` 로 동일 라인 아래 첨부.

## log.go (Debug/Info/Warn/Error/Log/logMessage)
- [x] `shared-go/pkg/logging/log_test.go` 생성
  > evidence: `test -f shared-go/pkg/logging/log_test.go && go test -count=1 ./shared-go/pkg/logging/...` → logging 패키지 PASS
- [x] `Debug` / `Info` / `Warn` / `Error` 4개 wrapper 가 적절한 level 로 `Log` 위임하는지 확인 (각 level 별 1 케이스)
  > evidence: `go test -coverprofile=/tmp/shared-go-logging-cover.out -count=1 ./shared-go/pkg/logging/... && go tool cover -func=/tmp/shared-go-logging-cover.out` → Debug/Info/Warn/Error 각 100.0%
- [x] `Log` nil logger 호출 시 panic 없이 noop
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestLogWithNilLoggerNoops` 포함 PASS
- [x] `Log` nil ctx 호출 시 `context.Background` 폴백
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestLogWithNilContextFallsBackToBackground` 포함 PASS
- [x] `Log` 의 `logger.Enabled(ctx, level)==false` 분기 (테스트용 핸들러로 disable)
  > evidence: `go test -coverprofile=/tmp/shared-go-logging-cover.out -count=1 ./shared-go/pkg/logging/... && go tool cover -func=/tmp/shared-go-logging-cover.out` → `Log` 100.0%
- [x] `Log` 가 빈 event 면 Event attr 생략, 비공백 event 면 Event attr 추가
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestLogAddsEventAttrOnlyWhenPresent` 포함 PASS
- [x] `Log` 가 `ContextAttrs(ctx)` 를 attrs 와 함께 merge 하는지 확인 (예: WithJobID 적용 후)
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestLogMergesContextAttrsAndAttrs` 포함 PASS
- [x] `logMessage` 폴백: message 우선 → event → "log"
  > evidence: `go test -coverprofile=/tmp/shared-go-logging-cover.out -count=1 ./shared-go/pkg/logging/... && go tool cover -func=/tmp/shared-go-logging-cover.out` → `logMessage` 100.0%

## id.go (NewID + sanitizeIDPrefix + helpers)
- [x] `shared-go/pkg/logging/id_test.go` 생성
  > evidence: `test -f shared-go/pkg/logging/id_test.go && go test -count=1 ./shared-go/pkg/logging/...` → logging 패키지 PASS
- [x] `NewID` 정상 prefix → `<prefix>_<unixMillis>_<hex>` 포맷 (regex 검증)
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestNewIDFormat` 포함 PASS
- [x] `NewID` 결과의 prefix 가 sanitize 후 값과 일치
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestNewIDUsesSanitizedPrefix` 포함 PASS
- [x] `sanitizeIDPrefix` 대문자 입력 → 소문자
  > evidence: `go test -coverprofile=/tmp/shared-go-logging-cover.out -count=1 ./shared-go/pkg/logging/... && go tool cover -func=/tmp/shared-go-logging-cover.out` → `sanitizeIDPrefix` 100.0%
- [x] `sanitizeIDPrefix` 공백/특수문자 → `_` 변환 또는 제거 (alphanumeric / -/_/.  만 유지)
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestSanitizeIDPrefix` 포함 PASS
- [x] `sanitizeIDPrefix` 빈 입력 → `"id"`
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestSanitizeIDPrefix` 포함 PASS
- [x] `sanitizeIDPrefix` 전부 비허용 문자 → `"id"`
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestSanitizeIDPrefix` 포함 PASS
- [x] `sanitizeIDPrefix` 앞뒤 `_` trim 동작
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → `TestSanitizeIDPrefix` 포함 PASS
- [x] `isIDPrefixAlphaNumeric` / `isIDPrefixSeparator` 분기 (선택, NewID + sanitize 테스트로 간접 커버 가능)
  > evidence: `go test -coverprofile=/tmp/shared-go-logging-cover.out -count=1 ./shared-go/pkg/logging/... && go tool cover -func=/tmp/shared-go-logging-cover.out` → `isIDPrefixAlphaNumeric`/`isIDPrefixSeparator` 각 100.0%

## Validation
- [x] `go build ./shared-go/...` 통과
  > evidence: `go build ./shared-go/...` → exit 0
- [x] `go test ./shared-go/pkg/logging/...` 통과
  > evidence: `go test -count=1 ./shared-go/pkg/logging/...` → ok github.com/park285/llm-kakao-bots/shared-go/pkg/logging 0.006s
- [x] `go test -cover ./shared-go/pkg/logging/...` 커버리지 출력 (log.go / id.go 70% 이상 권장)
  > evidence: `go test -cover -count=1 ./shared-go/pkg/logging/...` → coverage: 81.4% of statements
- [x] `./build-all.sh --no-bump` 통과
  > evidence: `./build-all.sh --no-bump` → `[LOCAL CI] Passed`, Docker images built, `[DONE] Build complete!`
- [x] staticcheck (있다면) 통과 — nil context 직접 전달 금지 (SA1012)
  > evidence: `./build-all.sh --no-bump` → `[LOCAL CI] staticcheck` 단계 후 `[LOCAL CI] Passed`

## Commit
- [x] 단일 commit: `test(shared-go/logging): log.go + id.go 단위 테스트 보강`
  > evidence: `git log -1 --pretty=%s` → `test(shared-go/logging): log.go + id.go 단위 테스트 보강`
- [x] Footer 에 `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`
  > evidence: `git log -1 --pretty=%B` → footer contains `Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>`
- [x] Local commit 만 — push 금지, PR 금지
  > evidence: `git status --short --branch` → local branch `refactor/shared-go-2026-05-21`, no push/PR command executed
