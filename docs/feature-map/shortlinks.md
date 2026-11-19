# 알림의 YouTube 단축 링크 열기

알림에서 짧은 주소를 열면 해당 영상의 watch URL을 가리킨다. Kakao 미리보기 봇은 차단한다.

## 하위 기능

유효한 영상 ID 검증, 사용자 이동, 스크랩 거부, 검색 색인과 캐시 억제.

## 사용자 경로

`GET /l/:videoID`, `HEAD /l/:videoID` 두 경로를 bot 서버와 선택적 별도 short-link listener가 제공한다. `internal/planes/bot/internal/app/http/shortlink_handler.go`는 `kakaotalk-scrap/` User-Agent를 구분한다.

## hololive-runtime.sh로 구동

```bash
p=$(scripts/verify/hololive-runtime.sh run --request GET /l/abcdefghijk 302 '' --request HEAD /l/x 404 '')
cat "$p"/{extra-1.status,extra-2.status}
```

`Location`의 영상 ID가 `abcdefghijk`인지 본다. 드라이버는 리디렉션을 따라가지 않아 영상 서버를 접속하지 않는다.

## 함정

브라우저가 마지막에 보여 주는 200은 링크 자체의 302가 아니다. 스크랩 차단은 `go test -p 2 -parallel 2 ./hololive/hololive-api/internal/planes/bot/internal/app/http -run '^TestYouTubeShortLinkBlocks'`에서 별도로 확인한다. 이 결과가 외부 ingress 배포를 확인해 주지는 않는다.
