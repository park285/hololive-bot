# Member News v4 구현 완료 보고서

**Date**: 2026-02-17  
**Status**: 완료

## 1) 기준 문서(SSOT)

- `docs/local/member-news-v4-execution-handoff.md`

본 보고서는 위 문서를 기준으로 Step 1~6 구현 완료 상태를 정리합니다.

---

## 2) 구현 완료 범위

### Step 1. Domain / Parser / Router

- `internal/domain/command.go`
- `internal/adapter/message.go`
- `internal/bot/bot.go`
- `internal/bot/command_router.go`
- `internal/bot/command_normalizer.go` (신규)

적용 내용:
- `CommandMemberNews`, `CommandMemberNewsSubscription` 추가
- `!뉴스 [이번주|이번달]`, `!뉴스알림 켜기|끄기|상태` 파싱 추가
- 뉴스 계열 파서를 `major_event` 파서보다 먼저 처리
- `news_subscription_* -> news_subscription(action)` normalize 적용

---

### Step 2. 신규 서비스 패키지

- `internal/service/membernews/repository.go`
- `internal/service/membernews/repository_pgx.go` (신규)
- `internal/service/membernews/service.go`
- `internal/service/membernews/scheduler.go`
- `internal/service/membernews/summarizer.go`
- `internal/service/membernews/filter.go`
- `internal/service/membernews/source_validator.go`

적용 내용:
- Postgres 구독 CRUD + Valkey write-through/warmup
- alarms 기반 room 멤버 조회
- 기간 필터(weekly/monthly), 멤버 매칭, 카테고리 분류, 정렬
- source_url 필수 + x allowlist + community corroboration 검증
- LLM(JSON schema) 요약 + deterministic fallback
- 주간 자동발송 스케줄러(월요일 09:00 KST, execution lock/room claim)

---

### Step 3. Command

- `internal/command/member_news.go`
- `internal/command/member_news_subscription.go`
- `internal/command/command.go`

적용 내용:
- 뉴스 조회/구독 제어 커맨드 추가
- `Dependencies`에 membernews service 주입 및 호출 경로 연결

---

### Step 4. Formatter / Template

- `internal/adapter/formatter_member_news.go`
- `internal/adapter/messages.go`
- `internal/domain/notification_template.go`
- `internal/domain/template_sample_data.go`

적용 내용:
- MemberNews formatter 메서드 추가
- 템플릿 키/샘플 데이터/메시지 상수 추가
- 커맨드 하드코딩 메시지 제거(Formatter 경유)

---

### Step 5. App Wiring

- `internal/bot/deps.go`
- `internal/app/providers.go`
- `internal/app/bootstrap.go`
- `internal/app/runtime.go`

적용 내용:
- membernews provider/DI 연결
- allowlist 경로 해석 및 validator/summarizer 초기화
- runtime에 membernews scheduler start/stop 연결

---

### Step 6. Migration / Config / Template

- `scripts/migrations/030_add_member_news_subscriptions.sql`
- `scripts/migrations/031_seed_member_news_templates.sql`
- `configs/hololive_official_x_accounts.json`
- `docs/templates/member_news_*.tmpl`

적용 내용:
- 구독 테이블 및 인덱스 추가
- CMD_MEMBER_NEWS_* 템플릿 시드 추가
- 공식 X allowlist 파일 추가

---

## 3) 리뷰 반영 추가 수정

리뷰 과정에서 아래 항목을 추가 보완했습니다.

- Scheduler lock/claim 실패 처리 강화(fail-open 제거)
- execution lock 해제 로직 보강
- YouTube source 분류 로직 개선(`channel/@handle/user/c`, `watch/shorts/live/youtu.be`)
- Parser 테스트 보강(`!뉴스알림 끄기`, `!뉴스알림 상태`)
- Command deps nil/error wrapping 테스트 추가
- Repository 동작 검증 테스트 추가(idempotent/unsubscribe/list order)

---

## 4) 테스트 및 품질 게이트 결과

실행 명령:

```bash
make -C hololive-kakao-bot-go fmt
make -C hololive-kakao-bot-go lint
make -C hololive-kakao-bot-go test
```

결과:
- fmt: 성공
- lint: 성공 (0 issues)
- test: 성공 (전체 패키지 통과)

---

## 5) 준수 사항

- generated 파일(`pb/*`, `*.pb.go`, `*_grpc.pb.go`, `*.pb*.go`) 미수정
- 기존 코드 컨벤션(context 전달, error wrapping, structured logging) 준수
- 템플릿 하드코딩 지양(Formatter/TemplateKey 경유)

