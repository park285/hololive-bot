# Contract: karing.kakaolink

## Summary

`alarm-worker`가 Iris `/karing/content-list`로 보내는 Hololive 알림은 Kakao Developers의 list template 4종과 1:1로 맞아야 합니다. 이 문서는 template ID, 변수명, 링크 path, 검증 기준을 고정합니다.

## Contract ID

- `karing.kakaolink`

## Provider

- Service: `alarm-worker`
- Code owner: `hololive/hololive-alarm-worker/internal/egress`, `hololive/hololive-alarm-worker/internal/service/dispatchrun`
- Request type: `iris.KaringContentListRequest`

## Consumers

- Iris runtime: `/karing/content-list`, `/karing/hololive`
- Iris bridge: KakaoLinkSpec existing-chat send method `c(Long)`
- Kakao Developers app: `hololive-bot`, app ID `1369981`

## Routing

- 방 유형이 일반채팅으로 확인되고 YouTube target이 있는 broadcast/video/Shorts/community 알림만 Karing content-list를 사용합니다.
- 확인된 일반채팅의 YouTube와 Chzzk 통합 알림은 YouTube target으로 Karing을 구성합니다.
- 오픈채팅은 `BOT_MARKDOWN_REPLIES`에 따른 기존 Markdown/message 경로를 사용하고, 방 유형을 확인하지 못한 경우에는 일반 텍스트를 사용합니다.
- Twitch-only, Chzzk-only, celebration, delivery digest, YouTube milestone과 generic notification delivery는 방 유형에 맞는 기존 message 경로를 사용합니다.
- 지원되는 알림의 Karing build, admission 또는 handoff가 실패해도 일반 텍스트나 Markdown으로 fallback하지 않습니다.

## Stable Template Map

| Item count | Template ID | Title | Status |
|---:|---:|---|---|
| 1 | `133266` | `1` copy | active |
| 2 | `133223` | `2` | active |
| 3 | `133222` | `3` | active |
| 4 | `133267` | `4` copy | active |

Deprecated template IDs:

| Item count | Deprecated ID | Reason |
|---:|---:|---|
| 1 | `133220` | Existing-chat `c(Long)` send was not stable in test-room smoke. Do not select for automated sends. |
| 4 | `133218` | Existing-chat `c(Long)` send was not stable in test-room smoke. Do not select for automated sends. |

## Kakao Template Variables

1-item template `133266` must use unnumbered variables only:

| Variable | Value source |
|---|---|
| `thumbnail` | item thumbnail URL |
| `item_title` | first item title |
| `item_web_url` | first item YouTube path |
| `alarm_title` | card header |
| `item_desc` | first item description |

2-item template `133223`:

| Variable | Value source |
|---|---|
| `alarm_title` | card header |
| `web_url` | first item YouTube path |
| `mobile_web_url` | first item YouTube path |
| `item1_title`, `item2_title` | item title |
| `item1_desc`, `item2_desc` | item description |
| `item1_thumbnail`, `item2_thumbnail` | item thumbnail URL |
| `item1_web_url`, `item2_web_url` | item YouTube path |

3-item template `133222` adds `item3_title`, `item3_desc`, `item3_thumbnail`, `item3_web_url` to the 2-item contract.

4-item template `133267` adds `item4_title`, `item4_desc`, `item4_thumbnail`, `item4_web_url` to the 3-item contract.

## Display Rules

- `alarm_title` is the card header, for example `방송 5분 전 알림`, `커뮤니티 알림`, `새 영상`.
- `itemN_title` and `item_title` are trimmed for Kakao card width before send.
- `itemN_desc` and `item_desc` must be compact: `<member_name> · MM/DD HH:mm`.
- Do not append status text such as `예정`, `LIVE`, `새 영상`, or `커뮤니티` after the time in the description.
- Member display name must use the configured short Korean name when available.
- Empty optional slots must not be rendered as visible blank cards. Split requests by 1/2/3/4 items instead.

## Link Rules

Template variables carry a YouTube path, not a full URL.

Allowed path examples:

| Content type | Path format |
|---|---|
| Video | `watch?v=<video_id>` |
| Live | `live/<video_id>` or `watch?v=<video_id>` |
| Shorts | `shorts/<video_id>` |
| Community post | `post/<post_id>` |

Kakao Developers link settings must prepend the same YouTube web origin for mobile and PC list item links:

```text
https://www.youtube.com/${item_web_url}
https://www.youtube.com/${itemN_web_url}
```

Do not use `https://youtu.be/${item_web_url}` for the 1-item template because it cannot represent `watch?v=...`, `shorts/...`, `live/...`, and `post/...` with one shared variable contract.

## Send Semantics

- Live send must use Iris `/karing/content-list` for generated content-list requests.
- `/karing/send` is allowed only for controlled smoke tests or already-materialized template args.
- Iris bridge must use KakaoLinkSpec existing-chat method `c(Long)`.
- Do not reintroduce direct DB injection, Web Picker send, or `b`/direct fallback for this contract.
- Live send admission은 `202 Accepted`와 `success=true`, `delivery=queued`, non-empty `requestId`를 모두 요구합니다.
- `202 Accepted`는 reply queue 접수 증거일 뿐 성공 증거가 아닙니다. Caller는 exact `requestId`로 `/reply-status/{requestId}`를 조회하고 `handoff_completed`를 관측한 뒤에만 delivery를 성공 처리합니다.
- `queued`, `preparing`, `prepared`, `sending`은 확인 중입니다. `failed`는 확정 실패이고 `outcome_unknown`, 알 수 없는 상태, request ID 불일치, 빈 응답과 확인 deadline 소진은 결과 불명확입니다.
- `handoff_completed`는 Kakao handoff를 확인하지만 상대방의 읽음이나 Kakao client의 최종 template 표시를 보장하지 않습니다.

## Retry Policy

- Iris가 admission 전에 반환한 bounded `429`/`502`/`503`과 요청이 client를 떠나지 않은 dial/DNS 실패는 owning queue 정책이 재시도할 수 있습니다. Alarm dispatch의 ambiguous transport 재시도는 동일 `clientRequestId`를 재생산할 수 있는 기존 단건/persisted send-unit 경계로 제한합니다.
- Iris 내부의 queue/handoff retry는 Iris가 소유합니다. Caller는 accepted request를 다시 post하지 않고 status만 조회합니다.
- Do not fall back to duplicate plain text after a Karing failure.
- `outcome_unknown` 또는 status 확인 실패는 caller retry 대상이 아닙니다. YouTube outbox는 `SENDING`을 보존하고 alarm dispatch는 quarantine하여 같은 알림을 다시 post하지 않습니다.
- `failed`는 owning lifecycle의 known failure로 기록하며 임의의 새 `clientRequestId`로 재발급하지 않습니다.

## Smoke Test Policy

Smoke tests must explicitly set the target room. Do not rely on Iris runtime default receiver fields.

Keep concrete Kakao `receiver_room_id` values and room names in private operational runbooks or environment-specific smoke configuration. Committed contract docs may show only placeholders:

```text
receiver_room_id=<explicit private smoke room id>
receiver_name=<explicit private smoke room name>
```

Before changing template IDs, variables, or link settings:

1. Run `/karing/content-list` dry-run for 1, 2, 3, and 4 items.
2. Run live smoke only against an explicit test room.
3. Confirm live send returns `HTTP 202` with a non-empty `requestId`.
4. Poll `/reply-status/{requestId}` until `handoff_completed`; `failed`, `outcome_unknown` or deadline exhaustion fails the smoke.
5. Confirm bridge log contains `text send kakaolink spec invoked method=c` and `kakaolink commit verified`.
6. Confirm KakaoTalk `chat_logs` has exactly one new row in the target room.
7. Manually check mobile and PC list item taps open the intended YouTube URL.

## Compatibility Rule

Any change to template ID, Kakao Developers variable name, link prefix, item splitting, or bridge send method is a contract change. Update this file, `docs/current/CONTRACT_MAP.md`, `docs/current/CONTRACT_MANIFEST.txt`, and the Iris Karing API documentation in the same change.
