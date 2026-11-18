# 지난 방송과 방송 시간 조회

종료된 방송을 조건으로 찾고 멤버의 방송 시간을 합산하거나 썸네일을 받는다.

## 하위 기능

최근 기록·종류·토픽·멤버·기간·개수 필터, 실제 시작/종료 근거를 이용한 시간 표시, 썸네일 원본 조회가 있다.

## 사용자 경로

- `!방송이력 미코 기간:7` (`방송기록`, `종료방송`, `이전방송`, `history`, `broadcast_history`); `전체` 및 `종류`, `토픽`, `기간`, `개수`, `멤버` 필터. 별칭은 `message_parser_broadcast.go`의 `broadcastHistoryFilterKinds`에 있다.
- `!방송시간 미코 7일` 또는 `!broadcast_time 미코 7일`.
- `!방송썸네일 abcdefghijk` 또는 `!방송이력 썸네일 abcdefghijk`. 독립 명령은 `썸네일`, `썸네일다운`, `썸네일다운로드`, `thumbnail`, `thumbnail_download`, `thumbnaildownload`, `broadcast_thumbnail`; 이력 하위 동사는 `썸네일`, `thumbnail`, `thumb`, `다운로드`, `download`. 영상 ID 또는 YouTube URL을 받는다.

## Go 핸들러 테스트로 구동

```bash
go test -p 2 -parallel 2 ./hololive/hololive-api/internal/planes/bot/internal/command/handlers -run '^Test(BroadcastHistoryCommand(MemberAliasAndTypeQuery|ShowsTimeEvidence)|BroadcastThumbnailCommandExecute)$'
```

준비된 종료 세션·미리보기 입력을 대상으로 조건과 출력이 대응하는지 본다. 서명된 채팅·실제 이미지 파일 전송은 이 검사가 아니다.

## 함정

옛 기록의 저장 시각은 방송 종료 근거가 아니다. 제한된 탐색이 잘린 결과를 전수 결과로 해석하지 않는다. 실제 썸네일 호스트를 접속하거나 임의 영상의 결과를 재현 가능한 기준으로 쓰지 않는다.
