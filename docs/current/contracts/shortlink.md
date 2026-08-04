# Contract: shortlink.youtube

## Summary

여러 방송을 하나의 일반 텍스트 알림으로 묶을 때 YouTube 원본 URL 대신 짧은 고정 목적지 URL을 사용합니다. `hololive-api` bot plane은 정상 사용자를 YouTube로 리다이렉트하고 KakaoTalk 링크 스크랩 요청은 리다이렉트 전에 거부하여 자동 섬네일 수집을 차단합니다.

## Contract ID

- `shortlink.youtube`

## Provider

- Service: `hololive-api` bot plane
- Route owner: `hololive/hololive-api/internal/planes/bot/internal/app/http`
- Contract package: `hololive/hololive-shared/pkg/contracts/shortlink`

## Consumers

- 일반 브라우저와 KakaoTalk 인앱 브라우저
- KakaoTalk 링크 스크랩 서버
- `alarm-worker`의 다중 방송 일반 텍스트 알림 renderer

## Route

| Field | Value |
|---|---|
| Methods | `GET`, `HEAD` |
| Path | `/l/:videoID` |
| Success | `302 Found` to `https://youtube.com/watch?v=<videoID>` |
| Kakao scraper | `403 Forbidden`, no `Location` header |
| Invalid ID | `404 Not Found` |

`videoID`는 길이 11의 ASCII `A-Z`, `a-z`, `0-9`, `_`, `-`만 허용합니다. 임의 URL을 입력받지 않으므로 open redirect가 아닙니다.

## Scraper Blocking

`User-Agent`에 대소문자 구분 없이 `kakaotalk-scrap/` 토큰이 있으면 `GET`과 `HEAD`를 모두 `403`으로 거부합니다. `KAKAOTALK` 토큰만 있는 일반 인앱 브라우저는 차단하지 않습니다.

모든 응답은 다음 정책을 가집니다.

- `Cache-Control: no-store, max-age=0`
- `Pragma: no-cache`
- `Referrer-Policy: no-referrer`
- `Vary: User-Agent`
- `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet, noimageindex`

## Alarm Rendering

`ALARM_SHORT_LINK_BASE_URL`이 설정된 경우에만 두 개 이상의 일반 alarm notification 그룹에서 YouTube URL을 `<origin>/l/<videoID>`로 바꿉니다.

- 값은 path, query, fragment, user info가 없는 `https` origin이어야 합니다.
- 빈 값은 기능을 비활성화하고 기존 YouTube 원본 URL을 유지합니다.
- 단일 방송 알림, Twitch-only, Chzzk-only, celebration, YouTube outbox 경로는 기존 링크를 유지합니다.
- integrated 알림은 YouTube 부분만 단축하고 기존 Chzzk 보조 링크를 유지합니다.
- `ALARM_DISPATCH_KARING_ENABLED=true`와 동시에 활성화할 수 없습니다. Karing list template의 명시적 thumbnail 계약과 충돌하므로 alarm-worker가 기동 시 fail closed합니다.

## Deployment

`ALARM_SHORT_LINK_BASE_URL`은 `hololive-alarm-worker`의 `/run/hololive-bot/alarm-worker.env`에 주입합니다. OpenBao KV/template 갱신과 Agent 재기동은 별도 approval-gated 운영 작업이며 이 repository PR이 live secret/config를 변경하지는 않습니다. 해당 origin의 외부 HTTPS ingress는 `/l/*`를 `hololive-api` bot plane으로 전달해야 합니다. 저장소의 기본 Compose 포트는 loopback 전용이므로 public ingress 설정은 별도 운영 경계에서 소유합니다.

## Tests

- Route constants: `hololive/hololive-shared/pkg/contracts/shortlink/routes_test.go`
- Origin and video ID validation: `hololive/hololive-shared/pkg/service/shortlink/youtube_test.go`
- Redirect and scraper rejection: `hololive/hololive-api/internal/planes/bot/internal/app/http/shortlink_handler_test.go`
- Alarm URL selection and startup validation: `hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_*shortlink*_test.go`, `hololive/hololive-alarm-worker/internal/app/workerapp/build_egress_shortlink_test.go`

## Compatibility

`ALARM_SHORT_LINK_BASE_URL`의 기본값은 빈 값이므로 기존 배포는 동작이 바뀌지 않습니다. Route path, accepted ID alphabet, redirect target, scraper token 또는 status code를 바꾸면 이 문서와 contract map/manifest를 함께 갱신합니다.
