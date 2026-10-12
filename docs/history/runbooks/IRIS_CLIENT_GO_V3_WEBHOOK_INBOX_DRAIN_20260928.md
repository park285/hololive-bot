# iris-client-go v3 전환 전 webhook inbox 드레인 (2026-09-28)

> Historical document. Do not use as the current source of truth. See `docs/current/runbooks/hololive-api.md`.
> iris-client-go v3는 #542(v6.0.0)로 도입·운영 반영되었고, 지원 중인 rollback image(v6.0.x 이상)는 모두 v3를 포함합니다.

구 SDK는 `durableAdmitter`가 저장한 `bot_webhook_inbox.payload`에 상위 `msg`·`room`과 중첩 `json.message`·`json.chat_id`를 함께 적었습니다. v3 `MessageJSON` decoder는 두 중첩 필드를 거절합니다. v3 runtime이 구 `pending`·`retry`·`processing` 행을 claim하면 `processInboxClaim`의 decode 실패 경로가 그 행을 `dead`로 종료하고 payload를 scrub하므로, 명령이 처리되지 않은 채 원본이 사라질 수 있습니다. 구 payload를 자동 변환하거나 fallback decode하지 않습니다.

v3 API image를 시작하기 전에 Iris webhook ingress와 구 API의 신규 admission을 quiesce하고, 구 API가 이미 받은 inbox를 기존 runtime으로 드레인합니다. 아래 read-only 집계에서 active 행이 **모두 0**임을 확인하고, 구 API의 재입력이 멈춘 상태에서 한 번 더 확인합니다. `legacy_shape_count`는 저장 형식의 증거이며 active 0을 대신하지 않습니다. 미완료·만료 lease가 남으면 기존 runtime에서 소유권·명령 결과를 조사하고 전환을 보류합니다. `dead`·`succeeded` 행은 `{}`로 scrub된 종단 기록이므로 이 드레인 대상이 아닙니다.

```sql
SELECT status, count(*) AS active_count,
       count(*) FILTER (
           WHERE COALESCE(payload -> 'json', payload -> 'JSON', '{}'::jsonb)
                 ?| ARRAY['message', 'chat_id']
       ) AS legacy_shape_count
FROM bot_webhook_inbox
WHERE status IN ('pending', 'retry', 'processing')
GROUP BY status ORDER BY status;
```

새 이미지의 signed webhook은 body `messageId`와 `X-Iris-Message-Id`가 일치해야 합니다. 이전 API image로 돌아가야 할 때도 ingress를 먼저 quiesce하고 v3에서 저장한 active inbox를 드레인한 뒤 해당 image의 payload 해석과 외부 부수 효과를 확인합니다. 처리 중 행을 삭제·재큐잉·임의 형식 변경해 드레인을 건너뛰지 않습니다.
