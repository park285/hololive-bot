# Task 003. bot command flow

## 목표

bot command flow의 책임을 재정리하고 command 실행 로그를 단일 책임 지점으로 옮긴다.

## 수정 파일

- `hololive/hololive-kakao-bot-go/internal/bot/log_events.go`
- `hololive/hololive-kakao-bot-go/internal/bot/log_attrs.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_ingress.go`
- `hololive/hololive-kakao-bot-go/internal/bot/command_router.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_message_handler.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_message_async.go`

## 금지

- raw user message 로그 금지
- `payload`, `msg`, `raw` 원문 로그 금지
- command 실패 중복 로그 금지

## 완료 기준

- command received: message_len/message_sha256_8만 남음
- command execution failed: `CommandRouter.Execute`에서 한 번만 남음
- error response failed는 handler에서 별도 이벤트로 남음
