# Contract: karing.kakaolink

## Summary

`alarm-worker`가 Iris `/karing/content-list`로 보내는 Hololive 알림은 Kakao Developers의 list template 4종과 1:1로 맞아야 합니다. 이 문서는 template ID, 변수명, 링크 path, 검증 기준을 고정합니다.

## Contract ID

- `karing.kakaolink`

## Provider

- Service: `alarm-worker`
- Code owner: `hololive/hololive-alarm-worker/internal/app/internal/workerapp`
- Request type: `iris.KaringContentListRequest`

## Consumers

- Iris runtime: `/karing/content-list`, `/karing/hololive`
- Iris bridge: KakaoLinkSpec existing-chat send method `c(Long)`
- Kakao Developers app: `hololive-bot`, app ID `1369981`

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
- A send is successful only when Iris returns `200` and bridge logs show `kakaolink commit verified`.
- `200` confirms KakaoTalk chat log commit. It does not guarantee the recipient has read the message.

## Retry Policy

- Bridge-level retry for missing KakaoLink commit is allowed and expected.
- Caller/outbox retry is allowed for bounded `502`/`503` failures.
- Do not fall back to duplicate plain text after a Karing failure.
- Failed deliveries remain retryable through the owning queue/outbox policy.

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
3. Confirm `HTTP 200`.
4. Confirm bridge log contains `text send kakaolink spec invoked method=c`.
5. Confirm bridge log contains `kakaolink commit verified`.
6. Confirm KakaoTalk `chat_logs` has a new row in the target room.
7. Manually check mobile and PC list item taps open the intended YouTube URL.

## Compatibility Rule

Any change to template ID, Kakao Developers variable name, link prefix, item splitting, or bridge send method is a contract change. Update this file, `docs/current/CONTRACT_MAP.md`, `docs/current/CONTRACT_MANIFEST.txt`, and the Iris Karing API documentation in the same change.
