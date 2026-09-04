# Contract: shortlink.youtube

## Summary

확인된 일반채팅의 지원 알림은 Karing을 사용하고, 오픈채팅과 미확인 방의 기존 grouped message path는 YouTube 원본 URL 대신 짧은 고정 목적지 URL을 사용할 수 있습니다. `hololive-api` bot plane은 정상 사용자를 YouTube로 리다이렉트하고 KakaoTalk 링크 스크랩 요청은 리다이렉트 전에 거부하여 자동 섬네일 수집을 차단합니다.

## Contract ID

- `shortlink.youtube`

## Provider

- Service: `hololive-api` bot plane
- Route owner: `hololive/hololive-api/internal/planes/bot/internal/app/http`
- Contract package: `hololive/hololive-shared/pkg/contracts/shortlink`

## Consumers

- 일반 브라우저와 KakaoTalk 인앱 브라우저
- KakaoTalk 링크 스크랩 서버
- `alarm-worker`의 grouped message renderer

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

`ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`인 경우 두 개 이상의 message-path alarm notification 그룹에서 YouTube URL을 해당 origin의 `/l/<videoID>`로 바꿉니다. 확인된 일반채팅의 Karing-compatible 알림은 이 renderer를 거치지 않고, 오픈채팅과 미확인 방은 기존 grouped message 경로에서 short link를 사용할 수 있습니다.

- 활성 값은 `https://short.holoshi.com`이며 trailing slash 하나는 같은 origin으로 정규화합니다.
- 빈 값은 기능을 비활성화하고 기존 YouTube 원본 URL을 유지합니다.
- 단일 알림, Twitch-only, Chzzk-only, celebration과 YouTube outbox source renderer는 기존 링크를 유지합니다.
- Integrated 알림은 message path에서 YouTube 부분만 단축하고 Chzzk 보조 링크를 유지합니다.
- Room-specific Karing과 short link는 서로 다른 전송 경로에 적용되므로 같은 runtime 설정에서 공존합니다.

## Deployment

Provider-first 순서를 지켜 아래 단계를 앞 단계가 검증된 뒤에만 진행합니다.

1. `hololive-api` short-link listener(`127.0.0.1:30101`)를 먼저 활성화합니다.
2. 중앙 `admin-dashboard-ingress`의 source-restricted `/l/*` listener(`100.100.1.8:30192`)를 활성화합니다.
3. Seoul Nginx의 `http` context에 `deploy/nginx/holoshi-public-shortlink.conf`를 적용해 전용 `short.holoshi.com` TLS/HTTP3 endpoint의 `/l/*`만 `30192`로 전달합니다.
4. 중앙 호스트에서 `scripts/deploy/shortlink-smoke.sh`를 실행해 listener, central ingress, public ingress의 `302`/`403`/`404` 계약을 모두 검증합니다.
5. 모든 provider 검증이 통과한 뒤에만 `hololive-alarm-worker` runtime file에 `ALARM_SHORT_LINK_BASE_URL=https://short.holoshi.com`을 설정하고 consumer를 재기동합니다.

`stack-secrets` 마스터 갱신과 `tools/sync-host.sh <host> --apply`, Nginx 적용과 reload는 별도 approval-gated 운영 작업입니다. smoke에는 secret이 필요하지 않습니다.

## Tests

- Route constants: `hololive/hololive-shared/pkg/contracts/shortlink/routes_test.go`
- Origin and video ID validation: `hololive/hololive-shared/pkg/service/shortlink/youtube_test.go`
- Redirect and scraper rejection: `hololive/hololive-api/internal/planes/bot/internal/app/http/shortlink_handler_test.go`
- Alarm URL selection and startup validation: `hololive/hololive-alarm-worker/internal/service/dispatchrun/alarm_dispatch_*shortlink*_test.go`, `hololive/hololive-alarm-worker/internal/app/workerapp/build_egress_shortlink_test.go`
- Deployed three-hop contract: `scripts/deploy/shortlink-smoke.sh`

## Compatibility

`ALARM_SHORT_LINK_BASE_URL`의 기본값은 빈 값이므로 기존 배포는 동작이 바뀌지 않습니다. Rollback은 consumer의 `ALARM_SHORT_LINK_BASE_URL`을 먼저 비우고 재기동하는 것으로 시작합니다. 이미 발송된 short link의 호환성을 위해 listener와 중앙·Seoul ingress 두 계층은 무기한 유지하며, 별도의 미래 compatibility deprecation이 명시적으로 승인되기 전에는 제거하지 않습니다. Route path, accepted ID alphabet, redirect target, scraper token 또는 status code를 바꾸면 이 문서와 contract map/manifest를 함께 갱신합니다.
