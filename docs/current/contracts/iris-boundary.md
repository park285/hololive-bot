# Contract: iris-boundary

## Summary

Iris / Redroid is an external KakaoTalk automation boundary used for webhook ingress and outbound message delivery.

## Contract ID

- `iris.webhook`

## Provider

- Service: Iris / Redroid instance
- Module: external
- Runtime: external boundary, not an in-repo Go runtime

## Consumers

- Service: `bot`
- Service: `alarm-worker`
- Usage: Kakao webhook ingress/reply and alarm dispatch send. Proactive alarm egress is owned by `alarm-worker`; exclusivity comes from PostgreSQL `FOR UPDATE SKIP LOCKED` row claims on the dispatch/delivery/YouTube outboxes plus the single Compose alarm-worker instance.
- `hololive-shared/pkg/service/delivery.RuntimeIrisClient` and `pkg/providers/iris.ProvideIrisClient` are low-level Iris client construction helpers, not proactive notification sender ownership. The proactive sender and generic notification delivery dispatcher live in `hololive-alarm-worker/internal/egress`. CI gates forbid producer runtimes from using these helpers for proactive egress.

## Transport

- External HTTP/H3 boundary

## Endpoint / Event / Queue

| Field | Value |
|---|---|
| Path/Event/Queue | 검토 필요; configured through `IRIS_BASE_URL`, `IRIS_BASE_URL_FILE`, Iris client APIs |
| Method | 검토 필요 |
| Version | external boundary; no in-repo version field |
| Contract package | none; Iris client dependency and runtime env |

## Request

```go
// External Iris client request shapes are outside hololive-shared contracts.
// Do not invent payload fields in this document.
```

새 Markdown 응답은 `iris-client-go v3.3.0`의 `SendRichText`로 `/reply`에 `type:"richtext"`와 원문을 보냅니다.
서버는 RichText 지원 Iris와 카카오톡 26.9.1 대응 bridge가 필요합니다. 기존 `BOT_MARKDOWN_REPLIES`와
오픈채팅 판정은 유지하고, 일반 텍스트 및 다른 메시지 유형은 전환하지 않습니다.
SDK의 기존 `SendMarkdown`·`Sender`·`BotClient` 계약은 변경하지 않으며 필요한 소비자 인터페이스만 확장합니다.

기본 템플릿 42개는 migration `347_richtext_templates.sql`에서 RichText 표현으로 전환합니다.
이전 표준 본문과 일치하는 전역 기본값만 갱신하며 사용자 지정 기본값·채널 override와 기존 발송 요청은 유지합니다.
운영 활성화는 API와 알람 워커 양쪽의 `BOT_MARKDOWN_REPLIES=true`를 확인해야 완료됩니다. 미설정 기본값은 `false`입니다.
템플릿 `mdsafe`와 직접 조합하는 동적 필드는 `kakaotext.MarkdownLiteral`로 한 번만 보호합니다.
LLM 최종 검토·캐시의 평문은 formatter 진입에서 보호하며 정적 서식·전체보기 패딩·저장 요청에는 재적용하지 않습니다.
카카오 표를 생성하지 않고 제목·대상·상태 강조, 항목 순서와 독립 URL을 사용합니다. 제목 문법은 굵은 서식이지 큰 글자가 아니며 링크 문법도 숨은 URL을 만들지 않습니다.

## Response

```go
// External Iris client response shapes are outside hololive-shared contracts.
```

## Error codes

| Code | HTTP status | Meaning | Consumer behavior |
|---|---:|---|---|
| transport error | n/a | Iris unreachable or H3 trust failure | retry/diagnose Iris runtime and certs |
| auth error | 검토 필요 | token mismatch or missing token | verify runtime secret files/env |
| send failure | 검토 필요 | Kakao send failed | dispatcher retry/DLQ policy applies for alarm dispatch |

## Timeout and retry policy

- `alarm-worker` owns proactive alarm send retry/DLQ behavior after Iris send failures.
- `bot` webhook/reply timeout behavior is runtime-specific and 검토 필요.
- H3 certificate/trust changes must follow Iris certificate runbooks.

## Compatibility policy

- Do not treat Iris as an internal contract package.
- Env, auth, cert, transport, and route changes require affected runtime runbook updates.
- External boundary changes should keep rollback guidance in `runbooks/rollback.md`.
- bot outbox와 alarm/notification/YouTube 원장은 새 요청에 `richtext`를 저장합니다. 기존 `markdown`·`text` 요청은
  저장된 본문·타입·thread·clientRequestId로만 재생합니다. 이는 실패 fallback이 아니며 각 원장이 기존 요청의
  drain·재시도 종료를 확인한 뒤에만 구형 읽기 경로를 제거할 수 있습니다.
- writer 배포 전에 migration `345_alarm_richtext_immutable_request.sql`과 `346_youtube_richtext_immutable_request.sql`을
  적용합니다. 기존 행은 갱신하지 않습니다. 새 RichText 요청 저장 후에는 해당 kind/route를 모르는 구형 writer로
  롤백하지 않고 forward-fix합니다. 로컬 검증 완료와 운영 적용 여부는 별개입니다.

## Tests

- Bot router/transport tests: 검토 필요
- Alarm-worker Iris egress sender tests: `hololive/hololive-alarm-worker/internal/egress`

## Known gaps

- Exact Iris endpoint shapes are intentionally not invented here.
- Auth header/signing details are covered by Iris-specific operational skills/runbooks, not this current docs workpack.
