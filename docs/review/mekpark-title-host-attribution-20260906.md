# mekPark 제목 기반 방송자 표시 검증

2026-09-06, Go 1.27.1 linux/amd64의 로컬 작업 트리에서 검증했다.
통제 문서는 [구현 계획](../current/plans/2026-09-06-mekpark-title-host-attribution.md)과
`DEC-20260906-hololive-mekpark-title-host-attribution`이다.

## 구현과 수용 기준

T01·AC01: `hololive/hololive-shared/pkg/domain/mekparkhost`에 공유 순수 판별기를 추가했다.
채널 ID로 유닛을 정하고 성명·개인 태그·시리즈·명시적 시점/파트/이름 쌍으로 진행자와
게스트를 구분한다. 기존 규칙 초안의 같은 유닛 게스트 2건을 수정했으며 신규 같은 상황
1건을 같은 기준으로 검증했다. 규칙은 embedded `rules.json` 하나가 소유한다.
`golang.org/x/text v0.41.0`은 기존 indirect 항목을 direct로 옮겼으며 버전·go.sum은 유지했다.

T02·AC02: 공유 outbox 텍스트 표시, alarm-worker의 라이브/방송 전 텍스트 및 카카오링크,
영상/쇼츠 카카오링크, bot의 라이브/예정 목록·채널 일정·방송 이력·알림 표시를 연결했다.
실제 seed template 렌더링과 카카오링크 content item 검증에서 `유닛 B · 미라`와
`아크로라 · 사야나 (게스트: 히나미)`를 확인했다. 영상 묶음은 채널 이름을 유지하고
항목마다 미라·네온·개인 미상을 각각 표시했다. 긴 제목은 축약 전에 분류한다.

AC03: 다른 채널의 동일 인물 태그, 커뮤니티·구독자 달성 알림, Twitch/Chzzk 단독 방송,
개인 미상 제목의 회귀를 확인했다. 표시 과정에서 원본 Stream·Channel·payload가 바뀌지
않는 것도 확인했다. 채널별 구독·수집·DB 스키마·공개 payload 구조는 수정하지 않았다.

## 데이터 근거

`hololive-osaka`의 `holo-postgres`/`hololive`를 다음 guard로 조회했다.

```text
sudo docker exec -e PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=10000' \
  holo-postgres psql -U postgres_admin -d hololive --no-psqlrc -v ON_ERROR_STOP=1 -At ...
SHOW transaction_read_only → on
```

두 채널에 한정한 집계와 최대 300행의 제목 조회만 수행했다. `youtube_live_sessions`와
`youtube_videos`가 같은 video_id를 가지면 세션을 우선하여 195개 고유 영상을 확보했다.
기존 149개 표본에 신규 46개를 추가했으며 최신 세션 상태를 반영했다. 영상에는 세션 상태가
없으므로 `status: null`을 유지했다.

| 출처 | 제목 수 | 진행자 표시 | 개인 식별 보류 |
|---|---:|---:|---:|
| UNIT B 방송 세션 | 59 | 52 | 7 |
| ACHRORA 방송 세션 | 81 | 77 | 4 |
| 세션 외 영상·쇼츠 | 55 | 46 | 9 |
| 합계 | 195 | 175 | 20 |

V01: 195개 제목의 진행자/게스트 기대값과 실제 판별 결과가 모두 일치했다. 추가 회귀에는
여섯 멤버, 전각/반각·공백·제로폭 문자, 시리즈 게스트, 이름 쌍, 게임명 `ミラクル`,
일반 본문의 이름 일부, 알 수 없는 채널이 포함된다. 방송 세션 129/140은 92.1%의
판별 범위이며 실제 출연자 정확도가 아니다. 세션에는 데모·커버 영상도 포함된다.

## 실행한 검증

T03·V01·V02: 아래 명령에서 다섯 패키지가 모두 `ok`로 완료되었다. `TEST_DATABASE_URL`을
제거하여 DB 테스트는 testcontainers의 임시 PostgreSQL과 격리된 테스트 DB를 사용했다.

```bash
env -u TEST_DATABASE_URL go test -race -p 2 \
  ./hololive/hololive-shared/pkg/domain/mekparkhost \
  ./hololive/hololive-shared/pkg/service/youtube/outbox/format \
  ./hololive/hololive-alarm-worker/internal/service/dispatchrun \
  ./hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter \
  ./hololive/hololive-api/internal/planes/bot/internal/command/handlers
```

V03: 같은 다섯 패키지 대상으로 `go build`가 exit 0으로 완료되었으며,
`golangci-lint run -c .golangci.yml`은 `0 issues.`를 반환했다.
`GOMEMLIMIT=10GiB go vet -p 2 -vettool=/home/kapu/go/bin/nilaway`도 exit 0이었다.
`git diff --check`와 `bash ../tools/checks/check-decision-catalog.sh check --submodules`가
통과했다. 초기 lint의 구조체 복사·불필요한 초기 대입·공백 지적은 수정했으며 억제를 추가하지 않았다.
최종 diff에서 작업 범위를 확인했고 동시에 진행 중인 admin-dashboard 변경은 수정하지 않았다.

## 남는 범위

이번 구현은 제목 규칙 방식이다. 표본은 제목 근거를 검토한 회귀 기대값이며 실제 영상을
독립 검수한 학습 정답 자료가 아니다. 모델 학습과 별도의 정확도 검증은 수행하지 않았다.
일반 본문의 이름 일부만 있거나 인물 표시가 없는 제목은 개인 식별을 보류한다.
현재 DB 제목은 최초 알림 이후 바뀌었을 수 있으므로 알림 시점 성능을 이 표본만으로
추정하지 않는다. 카카오톡 실제 발송이나 운영 배포·재시작·DB 쓰기는 수행하지 않았다.
Fallback delta: none.
