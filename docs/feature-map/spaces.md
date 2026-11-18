# X 스페이스 연결과 시작 알림

관리자는 연결 상태를 보고 후보 세션을 제출한다. worker가 검증·승격한 세션으로 감지한 최근 스페이스 시작만 LIVE 구독 방에 알린다.

## 하위 기능

후보의 예상 revision 확인·암호화, 검증 성공 시 승격, 인증 실패와 빈 목록 분리, 시작 제목 고정과 방별 중복 제거.

## 사용자 경로

Console → 홀로봇 → 설정 → X 스페이스 연결: `GET /api/holo/x-spaces/session`, `POST /api/holo/x-spaces/session`. API는 후보를 저장하고 `internal/service/xspaces`의 worker는 설정된 간격에 대상 계정을 확인한다. 사용자가 알림을 요청하는 별도 스페이스 채팅 명령은 없다.

## Go 핸들러 테스트로 구동

```bash
go test -p 2 -parallel 2 ./hololive/hololive-alarm-worker/internal/service/xspaces -run '^TestCollectionOutputPreservesFailureAndEmptySuccess$'
```

helper의 관측 없음과 관측 실패가 같은 결과로 표시되지 않는지 본다. [서비스 계약](../current/services/x-spaces.md)의 후보→활성→시작 알림 전체를 재현하려면 임시 암호화 키·가짜 대상·가짜 X 응답·Iris 수락 영수증이 필요하다. 기본 검증은 X 설정 없이 실행한다.

## 함정

실제 X 쿠키를 읽거나 기록하지 않는다. 후보 수락은 연결 복구가 아니며 스페이스 시작 알림이 전달됐다는 뜻도 아니다. X 세션 전이의 로컬 TLA+ 모델은 없다.
