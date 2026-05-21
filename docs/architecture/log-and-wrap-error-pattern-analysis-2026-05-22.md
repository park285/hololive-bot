# 2026-05-22 — logAndWrapError 패턴 분석 (Phase 2.B.4 진입 전)

본 문서는 `hololive-shared` 의 `logger.Error + fmt.Errorf` wrap 패턴 빈도, 인접 매칭, slog schema 영향을 분석해 `LogAndWrapError` helper 도입의 안전성과 마이그레이션 우선순위를 평가한다. 본 문서는 decision 만, helper 구현은 별도 task.

## 1. 패턴 빈도 측정

측정 대상은 `hololive/hololive-shared/` 아래 Go 파일이다.

- `grep -rn "logger\.Error\b" --include="*.go" hololive/hololive-shared/ | wc -l` — `90`
- `grep -rnE "fmt\.Errorf\(.*: %w" --include="*.go" hololive/hololive-shared/ | wc -l` — `880`
- `grep -B 0 -A 2 "logger\.Error" --include="*.go" -r hololive/hololive-shared/ | grep -B 2 "fmt\.Errorf.*: %w" | wc -l` — `47`

세 번째 명령의 `47` 은 패턴 개수가 아니라 `grep -B/-A` 가 출력한 컨텍스트 라인 수다. 실제 인접 패턴 수는 아래 awk 로 다시 계산했다.

```bash
awk '
FNR==1 { for (i in buf) delete buf[i]; for (i in filebuf) delete filebuf[i] }
{ buf[FNR]=$0; filebuf[FNR]=FILENAME }
/logger\.Error/ { loggerLines[++n]=FILENAME ":" FNR; loggerText[n]=$0; pending[n]=FNR; pendingFile[n]=FILENAME }
/return[[:space:]]+fmt\.Errorf\(.*: %w/ {
  for (i=1; i<=n; i++) if (pendingFile[i]==FILENAME && FNR>pending[i] && FNR<=pending[i]+2 && !seen[i]) {
    seen[i]=1; count++
    print loggerLines[i] ":" loggerText[i]
    print FILENAME ":" FNR ":" $0
    print "--"
  }
}
END { print "COUNT=" count > "/dev/stderr" }
' $(rg --files hololive/hololive-shared -g '*.go')
```

결과: `COUNT=4`. 따라서 master plan 의 `1378회` 는 더 넓은 error logging / wrap 후보 집계로 보고, Phase 2.B.4 의 즉시 자동 마이그레이션 후보는 4곳으로 제한한다.

## 2. 패턴 sample

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:99` / `:101`

```go
as.logger.Error("Failed to add alarm", slog.Any("error", opErr))
return 0, fmt.Errorf("rebuild add cache from repository: %w", opErr)
```

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:160` / `:162`

```go
as.logger.Error("Failed to persist alarm before cache write", slog.Any("error", err))
return fmt.Errorf("persist alarm before cache write: %w", err)
```

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:270` / `:272`

```go
as.logger.Error("Failed to persist alarm removal before cache write", slog.Any("error", err))
return fmt.Errorf("delete alarm before cache removal: %w", err)
```

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:280` / `:282`

```go
as.logger.Error("Failed to persist alarm type update before cache write", slog.Any("error", err))
return fmt.Errorf("persist alarm type update before cache removal: %w", err)
```

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:367` / `:369`

```go
as.logger.Error("Failed to persist room alarm clear before cache write", slog.Any("error", err))
return fmt.Errorf("delete room alarms before cache clear: %w", err)
```

위 sample 중 awk 의 1-2 line 인접 조건에 매칭된 것은 `:160/:162`, `:270/:272`, `:280/:282`, `:367/:369` 네 곳이다. `:99/:101` 은 동일한 형태지만 `if as.logger != nil` 블록 때문에 텍스트상 2줄 초과 후보로 분류된다.

## 3. LogAndWrapError 시그니처 검토

cross-cutting helper doc 의 권장 시그니처는 다음 형태다.

```go
func LogAndWrapError(ctx context.Context, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error
```

권장 내부 동작은 `ErrorAttrs(err)` 를 선행 merge 하고 shared logging wrapper 를 통해 기록한 뒤 wrap error 를 반환하는 형태다.

```go
func LogAndWrapError(ctx context.Context, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error {
    if err == nil {
        return nil
    }
    errorAttrs := ErrorAttrs(err)
    merged := append(errorAttrs, attrs...)
    Error(ctx, logger, op+".error", op+": "+err.Error(), merged...)
    return fmt.Errorf("%s: %w", op, err)
}
```

검토 결과:

- `ErrorAttrs(err)` schema 는 `error_type`, `error_message`, 선택적 `error_code`, 선택적 `retryable` 이다. 기존 호출부의 대표 key 는 `slog.Any("error", err)` 와 `slog.String("error", err.Error())` 이므로 helper 로 즉시 교체하면 `error` key 가 사라지고 새 key 가 추가된다.
- `attrs ...slog.Attr` 는 shared-go logging package 의 `Error(ctx, logger, event, message, attrs...)` 와 맞는다. 기존 `logger.Error(..., ...any)` 호출을 바로 넘길 수는 없지만 schema 검증성은 높다.
- event 이름 정책은 후속 구현 전에 하나로 고정해야 한다. 이번 task 템플릿은 `op+".error"` 를 제안하지만 `docs/architecture/cross-cutting-helpers-2026-05-21.md` 는 `op+".failed"` 를 적고 있다. 기존 `hololive-shared` 의 직접 `logger.Error` 호출은 대부분 structured `event` attr 을 쓰지 않으므로, 첫 PR 에서는 기존 message/key 의존 호출부를 제외하고 새 event suffix 를 명시적으로 문서화해야 한다.
- ctx 전달은 adjacent 4곳 모두 함수 인자로 `ctx context.Context` 가 있어 helper 적용 가능하다. 다만 `pkg/service/activity/logger.go` 처럼 ctx 가 없는 logger 호출도 존재하므로 ctx 없는 호출부는 Phase 2.B.4 초기 대상에서 제외하거나 명시적으로 `context.Background()` 정책을 정해야 한다.

## 4. slog 컨벤션 침해 회피 검토

기존 logger 호출의 error attr 형식은 섞여 있다.

- `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go:160` — `slog.Any("error", err)`
- `hololive/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatcher_claim.go:246` — `slog.Any("error", err)`
- `hololive/hololive-shared/pkg/service/delivery/dispatcher.go:144` — `slog.String("error", err.Error())`
- `hololive/hololive-shared/pkg/service/delivery/dispatcher.go:246` — `slog.String("error", err.Error())`
- `hololive/hololive-shared/pkg/server/internal/httpserver/response.go:48` — `slog.Any("error", err)` 를 `[]any` 로 변환해 `logger.Error` 에 전달
- `hololive/hololive-shared/pkg/service/holodex/internal/holodexprovider/api_client.go:323` / `:324` — `slog.Any("error", err)` 와 manual `slog.String("error_type", errorType)` 병행

검색 결과 `slog.Any("err", ...)` / `slog.String("err", ...)` 형태는 `hololive-shared` 에서 확인되지 않았다. manual `error_code` / `retryable` logger attr 도 Go 코드에서는 확인되지 않았고, `error_code` 는 docs/history 와 DB field 이름(`last_error_code`) 쪽에 주로 나타났다.

마이그레이션 영향:

- helper 가 자동 생성하는 `error_type` / `error_message` 는 기존 `error` key 와 병존할 수 있으나, 기존 호출부의 `error` attr 를 제거하면 alert/grep 영향이 생길 수 있다.
- `api_client.go:324` 처럼 이미 manual `error_type` 을 넣는 호출부는 helper 적용 시 key 중복이 생긴다.
- `logger.Error` 의 message 문자열 자체를 grep 하는 운영 규칙이 있으면 `op+": "+err.Error()` 또는 `op+" failed"` 로 바뀌는 순간 깨질 수 있다.

따라서 Phase 2.B.4 초기 구현은 helper 만 신설하거나, 소수 호출부에 한해 기존 `"error"` key 를 유지하는 compatibility attr 를 명시적으로 넘기는 방식이 안전하다.

## 5. 마이그레이션 전략

| 옵션 | 장점 | 위험 |
| --- | --- | --- |
| (a) 전체 마이그레이션 | 중복 제거 효과가 크고 schema 를 빠르게 통일한다. | 90개 `logger.Error`, 880개 wrap 후보 중 실제 의미가 다른 호출부를 한 PR 에 섞게 된다. `"error"` key, message text, ctx 유무, manual `error_type` 중복 위험이 크다. |
| (b) 점진적 마이그레이션 | 패키지별로 log shape 를 검증하며 전환할 수 있다. adjacent 4곳처럼 ctx 와 wrap 형태가 명확한 대상부터 시작 가능하다. | helper 신설 후 한동안 old/new schema 가 공존한다. |
| (c) opt-in | helper 만 만들고 새 코드부터 적용하므로 alert/grep 영향이 가장 작다. | 기존 중복 제거 효과가 작고 Phase 2.B.4 의 cleanup 목표가 지연된다. |

결정: (b) 점진적 마이그레이션을 권장한다. 단, 첫 구현 PR 은 (c)에 가깝게 helper 와 단위 테스트를 먼저 두고, 같은 PR 또는 다음 PR 에서 adjacent 4곳 중 1곳만 시범 적용한다.

## 6. log key 변경 위험 평가

다음 명령으로 alert/grep rule 후보를 검색했다.

```bash
grep -rn "error_type\|error_message\|error_code\|retryable" --include="*.yaml" --include="*.json" --include="*.md" .
```

원 명령은 `admin-dashboard/frontend/node_modules/typescript/lib/*/diagnosticMessages.generated.json` 의 `error_messages` 번역 키를 다수 포함한다. repo 산출물만 보기 위해 `node_modules` 와 `.git` 을 제외해 다시 확인했다.

```bash
grep -rn "error_type\|error_message\|error_code\|retryable" --include="*.yaml" --include="*.json" --include="*.md" --exclude-dir=node_modules --exclude-dir=.git .
```

결과 요약:

- active docs 에서는 `docs/current/ERROR_CONTRACT.md` 의 HTTP wire error contract 와 `docs/architecture/cross-cutting-helpers-2026-05-21.md` 의 helper schema 언급이 확인된다.
- `docs/history/plan-kits/hololive-bot-integrated-refactor-v3/docs/03-logging-contract.md` 는 `error_type`, `error_message`, `error_code`, `retryable` 를 과거 logging contract 로 나열한다.
- `docs/history/plan-kits/holobot-valkey-plan/*` 의 `last_error_code` 는 DB field / SQL 예시이며 slog key 와 직접 충돌하지 않는다.
- `*.yaml` 에서 해당 key 를 쓰는 alert rule 은 확인되지 않았다.

위 검색만으로 외부 운영 grep rule 부재를 보장할 수는 없다. repository 내부 기준 충돌 위험은 낮지만, 기존 호출부의 `"error"` key 제거와 log message 변경 위험은 남아 있으므로 log key 변경 위험은 **medium** 으로 평가한다.

## 7. 본거지 결정

본거지는 `shared-go/pkg/logging` 유지가 유효하다.

근거:

- `shared-go/pkg/logging/attrs.go` 에 `ErrorAttrs(err)` 가 이미 있고, schema 는 `error_type`, `error_message`, `error_code`, `retryable` 로 고정돼 있다.
- `shared-go/pkg/logging/log.go` 에 `Error(ctx, logger, event, message, attrs...)` 가 있어 ctx-aware structured logging wrapper 와 맞다.
- `hololive/hololive-shared/go.mod` 는 `github.com/park285/llm-kakao-bots/shared-go v0.1.2` 와 local replace `../../shared-go` 를 이미 가진다.

`hololive/hololive-shared/pkg/logschema` 는 YouTube domain log schema 를 다루는 호출부에는 보조 layer 로 쓸 수 있지만, cross-runtime helper 의 본거지로는 좁다.

## 8. 마이그레이션 step list

1. `shared-go/pkg/logging` 에 `LogAndWrapError` 를 신설하고 nil err, nil logger, attr 보존, `ErrorAttrs(err)` 포함, `%w` wrapping 을 단위 테스트로 고정한다.
2. event suffix 를 `op+".error"` 또는 `op+".failed"` 중 하나로 결정하고 `docs/architecture/cross-cutting-helpers-2026-05-21.md` 와 helper godoc 을 맞춘다. message text 의존 경로는 첫 적용에서 제외한다.
3. `hololive/hololive-shared/pkg/service/notification/internal/alarmservice/alarm_service_mutation.go` 의 adjacent 4곳 중 1곳만 시범 마이그레이션하고 기존 `"error"` key 유지 여부를 테스트 또는 log snapshot 으로 확인한다.
4. 같은 파일의 나머지 adjacent 후보로 확장한다. `:99/:101` 처럼 nil logger guard 때문에 2줄 초과로 잡히는 동일 패턴은 별도 검토한다.
5. `slog.String("error", err.Error())` 를 쓰는 `pkg/service/delivery/dispatcher.go` 같은 호출부는 alert/grep 확인 후 package 단위 PR 로 분리한다.
6. manual `error_type` 이 있는 `holodexprovider/api_client.go` 는 `ErrorAttrs` 와 key 중복 방지 방식을 먼저 정한 뒤 적용한다.
7. ctx 가 없는 호출부는 helper 적용 대상에서 제외하거나 wrapper 호출 시 `context.Background()` 허용 기준을 별도 문서화한다.
8. `hololive-shared` 전체로 확장할 때는 package별로 `logger.Error` count 와 `fmt.Errorf(...: %w)` count 를 다시 측정하고, 변경 전후 log key diff 를 검증한다.

## 9. 결론

`LogAndWrapError` 의 시그니처와 본거지 결정은 유효하다. 다만 실제 인접 자동 마이그레이션 후보는 `COUNT=4` 로 작고, 기존 `hololive-shared` 호출부는 `"error"` key 와 message text 에 의존할 가능성이 있다. Phase 2.B.4 는 helper 신설 + 테스트 후 점진적 마이그레이션으로 진행하고, 첫 적용은 `alarm_service_mutation.go` 의 adjacent 후보 1곳으로 제한하는 것이 안전하다.

log key 변경 위험은 repository 내부 기준 **medium** 이다. `*.yaml` alert rule 충돌은 확인되지 않았지만, 외부 grep rule 과 기존 `"error"` key 의존 가능성 때문에 전체 마이그레이션은 권장하지 않는다. event suffix 는 `op+".error"` 와 `op+".failed"` 중 하나로 구현 전 결정해야 한다.
