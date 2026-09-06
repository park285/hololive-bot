# UNIT B 멤버별 구독과 fail-open 검증

2026-09-06 사용자 요구에 따라 채팅방에서 UNIT B의 라이라·미라·네온을 각각 구독하도록
구현했다. 구독 주체는 채팅방이며 카카오톡 사용자별 구독을 추가한 것이 아니다.
실행 계획은 [PLN-20260906-unit-b-member-subscriptions](../current/plans/2026-09-06-unit-b-member-subscriptions.md),
결정은 `DEC-20260906-hololive-unit-b-member-subscriptions`이다.

## 확인된 동작

T01·AC01·V01: 실제 YouTube 채널 ID는 유지하고 `alarms.host_id`로 구독 대상을 구분한다.
같은 방에 전체 채널 구독과 세 멤버 구독을 함께 저장할 수 있다. 빈 `host_id`는 전체 채널이고,
멤버별 알림 종류와 해지는 서로 덮어쓰지 않는다. DB 제약도 UNIT B의 세 멤버만 허용한다.

T02·AC02·V02: 기존 알람 명령의 멤버 이름에 `미라`, `네온`, `라이라`를 사용할 수 있다.
추가·해지·목록, HTTP client/handler의 `host_id` 전달, 중복 추가, 다른 멤버의 다음 방송 제외를
검증했다. 한 멤버를 해지한 뒤 다른 멤버의 알림 종류와 방/채널 cache registry가 유지된다.
DB를 기준으로 캐시를 재구축한 뒤에도 같은 대상이 복원된다. 해지 저장 후 구독 재조회가
실패한 사례에서는 오류를 반환하면서 DB 기준 재구축으로 남은 멤버 구독을 보존했다.

T03·AC03·V03: 기존 `mekparkhost` 판별 결과와 알림 종류로 다음 대상을 계산한다.

| 제목의 진행자 | 포함되는 방 |
|---|---|
| 미라 | 미라 구독 방 + UNIT B 전체 구독 방 |
| 미라와 네온 | 두 멤버의 구독 방 합집합 + 전체 구독 방 |
| 진행자 미상 또는 빈 제목 | 해당 알림 종류를 구독한 모든 UNIT B 방 |
| 외부 유닛 인물만 식별됨 | UNIT B 진행자는 미상으로 처리함 |

한 방이 여러 구독에 해당해도 같은 이벤트의 수신 방에는 한 번만 들어간다. 다른 채널이나
비구독 방으로 대상을 넓히지 않는다. 예정/일정 변경/live-catchup과 YouTube outbox가 같은
선정 함수를 사용한다. outbox는 채널·종류가 같아도 진행자 집합이 다르면 대상 조회를 분리한다.
수신 누락 점검도 현재 방송 제목에 맞는 구독 방을 사용한다.

JSON 손상이나 구독 DB 오류는 진행자 미상으로 바꾸지 않는다. outbox의 대상 조회 실패는
기존 fanout 실패·재시도 경로에 남고, 체커는 오류를 반환한다. 알려진 진행자가 없는 정상 제목에
대해서만 fail-open을 적용한다. 기존 ML 실험의 낮은 점수를 자동 확정에 사용하지 않는다.

## 실제 검증

T04·AC04·V04: 다음 검사를 로컬에서 수행했다.

- 새 기능의 도메인·실제 PostgreSQL·캐시·HTTP·명령·worker 회귀 테스트가 통과했다.
- 영향 패키지 12개의 전체 `go test -race -p 2`가 통과했다. 대상은 shared domain/mekparkhost,
  alarm, alarmservice, bot alarm/handlers/bootstrap/runtime, worker checking/scheduler/youtubedispatch,
  hololive-dbtest다.
- 같은 12개 패키지의 `golangci-lint run -c .golangci.yml`은 지적 0건이고,
  `GOMEMLIMIT=10GiB go vet -p 2 -vettool=/home/kapu/go/bin/nilaway`가 통과했다.
- `go build ./hololive/hololive-api/... ./hololive/hololive-alarm-worker/... ./hololive/hololive-shared/...`가 통과했다.
- `check-migration-manifest.sh`가 통과했고, `SCHEMA_SNAPSHOT_UPDATE=1 go test -run '^TestSchemaSnapshotGolden$' ./hololive/hololive-dbtest`
  실행 후 전체 dbtest race suite에서도 snapshot을 다시 확인했다.
- migration 194–196을 두 번 재적용한 뒤 전체 채널과 멤버의 알림 종류가 보존되는 실제 DB 검사를 통과했다.
- 격리한 PostgreSQL/Valkey에서 `INTEGRATION_TEST=true`로 `TestDispatcher_ProcessOnce_Success`의
  race 검사를 실행해 실제 fanout부터 테스트 sender의 발송 완료까지 확인했다.
- 계약 지도와 decision catalog 검사, `git diff --check`가 통과했다.

중간에 멤버 검색의 nullable 결과를 새 구독 대상으로 옮길 때의 nil 처리를 보완했다.
공통 DB suite에서는 별도 작업의 migration 192가 추가한 pending-end 외래키에 기존 retention
fixture가 맞지 않았다. 필요한 부모 테스트 행을 추가했으며 기존 retention assertion과 운영 코드는
유지했다. 같은 workspace의 dispatcher 생성자 변경에서는 저장소가 없는 기존 구성을 보존하면서
선택적인 전이 저장소 생성 여부를 호출부에서 처리했고, 테스트 helper 형식을 맞췄다.
다른 세션의 admin/sourceobservation 변경과 Git ref는 보존했다.

## 운영 적용 조건

로컬 코드·SQL·검증만 완료했다. 운영 DB 적용, 발송, 재시작, 배포, commit/push는 하지 않았다.

1. 같은 변경을 포함한 `hololive-api`와 `hololive-alarm-worker`를 함께 준비한다. API의 admin-plane
   compatibility provider도 같은 shared handler를 사용하므로 두 provider 모두 갱신 대상이다.
2. 승인된 전환 창에서 기존 구독 writer를 중단한 뒤 공식 migration runner로 194 → 195 → 196을 적용한다.
   194는 컬럼/제약, 195는 동시 unique index 생성, 196은 인덱스 유효성 확인과 기존 유일성 제약 교체다.
3. 새 worker의 구독 선정과 HTTP provider가 준비된 후 새 bot에서 멤버별 구독을 허용한다.
   오래된 provider/worker와 섞어 멤버별 등록을 받지 않는다.

196 적용 후 기존 바이너리의 `(room_id, channel_id)` upsert는 호환되지 않는다. 복구할 때
멤버별 구독을 삭제하거나 전체 채널 구독으로 합치지 않고, 새 스키마와 저장된 `host_id`를 보존하는
수정본을 사용해야 한다. ACHRORA 멤버별 구독과 ML 기반 자동 확정은 이 변경 범위에 포함하지 않았다.

사용 방법은 [mekparkhost README](../../hololive/hololive-shared/pkg/domain/mekparkhost/README.md),
HTTP 계약은 [alarm contract](../current/contracts/alarm.md)에 있다.
