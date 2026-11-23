# 홀로봇 기능 검증 지도

사용자가 보거나 바꾸는 값에서 출발한다. [실행 구성](../current/PROJECT_MAP.md)과 달리 이 지도는 **어디까지 직접 확인했는가**를 설명한다. Iris Console 화면 자체는 이 저장소의 진입점이 아니다.

## 시작과 증거

저장소 루트에서 Go 1.27.4와 `go.work`의 형제 모듈, 로컬 Podman, OpenSSL, jq를 준비한다. 스크립트는 이미 떠 있는 자원을 쓰지 않고 매번 DB·Valkey·개발 인증서를 새로 만들고 저장소의 시험 프로필을 복사한다. PostgreSQL은 dbtest의 `postgres:19.0-alpine` 고정 digest, Valkey는 `localhost/iris-stack-valkey:9.1.3-zlib1.4-r1` 로컬 이미지가 있어야 한다. 이미지를 자동으로 받지 않는다. 실제 앱, H3 요청기와 dbtest 마이그레이션을 빌드하며 웹훅이나 관리 API 주소를 환경에서 물려받지 않는다.

```bash
scripts/verify/hololive-runtime.sh --help
proof=$(scripts/verify/hololive-runtime.sh run)
cat "$proof"/{identity,database,doctor-auth.status,view.json,persisted,memo-after.json,memo-persisted,exit-code,cleanup}
```

빌드·일회용 DB 구성 → 구독 수 조회, 빈 방, 무인증 거부 → LIVE 구독 등록·별도 조회·삭제 → 번역 메모 저장·낡은 수정 거부·다시 조회 순이다. 각 요청의 `.request`, `.status`, `.json`, `.exit`와 SQL 재조회 결과가 남는다. `--request`를 여러 번 주면 기본 여정 뒤 같은 인스턴스에서 순서대로 실행하고 `extra-1.*`부터 기록한다. 예상 HTTP 상태는 검사하되 추가 요청 본문은 호출자가 확인한다. 실패 시에도 마지막 줄의 `/tmp/c3-hololive-proof.*`를 연다. `cleanup`의 컨테이너 ID·망 이름은 `identity`와 대조한다. 증거에는 합성 데이터만 넣는다.

외부 통신을 허용하지 않는 전용 브리지와 컨테이너 내부 루프백·DB 소켓을 쓴다. Iris 대역은 준비 응답만 낸다. 실제 X·Gemini·YouTube·Holodex·Kakao, 기존 DB/방/키, 운영 컨테이너에 연결하지 않는다. 빈 데이터라도 마이그레이션이 넣는 멤버·메시지 기본값은 있다. 모든 페이지의 명령이 기본 실행에 포함되는 것은 아니다.

## 기능 색인

| 사용자 결과 | 진입점과 확인 범위 |
|---|---|
| [도움말과 명령 수신](help.md) | 채팅 → Iris 웹훅 → 도움말 앨범. 핸들러 경계 |
| [현재·예정 방송과 일정](streams.md) | 채팅·관리 API·공식 일정. 외부 공급자 제외 |
| [종료 방송 기록](history.md) | 방송 이력·시간·썸네일 명령. 핸들러 경계 |
| [멤버 관리와 프로필](members.md) | 관리 API 등록·재조회, 별칭·졸업·채널, 채팅 프로필 |
| [기념일 달력과 축하](calendar.md) | 월별 조회, 생일·데뷔일 알림 |
| [주간 일정표](posters.md) | 포스터 수집·검토·번역 메모·멤버별 카드. 메모는 기본 여정 |
| [방별 알람 구독](subscriptions.md) | 채팅·관리·worker API. 등록·조회·삭제는 기본 여정 |
| [알림 발송과 정산](dispatch.md) | 자동 알림·실패 조사·재대기·수동 정산. [선점 모델과 반례 상태도](../../verification/tla/AlarmDispatchClaim/COVERAGE.md#결함-경로-상태도) |
| [방 접근과 설정](rooms.md) | 가입 방·허용/차단·이름·알림 시점 |
| [X 스페이스 연결](spaces.md) | 세션 후보 등록·검증·시작 알림. 실제 쿠키 금지 |
| [방송 단축 링크](shortlinks.md) | 공개 GET/HEAD, 스크랩 방지 |
| [YouTube 관측과 정본 갱신](collection.md) | 스케줄러·내부 helper·재처리. [수집 리스 모델과 상태도](../../verification/tla/CollectionJobLease/COVERAGE.md#상태도) |
| [통계·진단과 기동 도구](operations.md) | 통계·상태·프로필·마이그레이션 CLI |
