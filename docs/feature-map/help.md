# 채팅에서 도움말 열기

방에서 명령어 사용법을 이미지 앨범으로 받는다.

## 하위 기능

명령 접두어 인식, 방 접근 검사, 웹훅 수신 중복 제거, 명령 실행과 답장 outbox, 도움말 카드 렌더링과 전송 결과 분류가 관여한다.

## 사용자 경로

허용된 일회용 방에서 `!도움말`을 보낸다. `!도움`, `!help`, `!명령어`, `!commands`도 같다. 수신기는 bot 평면의 `POST /webhook/iris`이다. `internal/planes/bot/internal/adapter/messaging`의 파서는 설정한 접두어만 받으며, 미등록 명령은 도움말과 다르다.

## Go 핸들러 테스트로 구동

```bash
go test -p 2 -parallel 2 ./hololive/hololive-api/internal/planes/bot/internal/command/handlers -run '^TestHelpCommand_Execute_(GoldenPathSendsImageAlbum|AlbumOutcomeUnknownSuppressesTextFallback)$'
```

앨범 전송 성공과 수락 불명 때의 추가 텍스트 억제를 별도로 본다. 실제 엔드투엔드는 서명된 Iris v3 웹훅, 허용된 채팅 ID와 응답을 기록하는 Iris reply 대역이 전제다. 런타임 드라이버의 `/ready` 대역에는 reply 기능이 없다.

## 함정

웹훅 HTTP 수락은 작업 등록이지 방 도착이 아니다. 텍스트 대체가 허용된 실패와 결과 불명을 혼동하면 사용자가 두 번 받을 수 있다. 이 여정의 inbox/outbox 전체를 검증한 로컬 TLA+ 모델은 없다.
