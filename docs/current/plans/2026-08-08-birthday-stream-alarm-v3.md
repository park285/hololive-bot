# 생일 방송 알람 v3 설계

작성: 2026-08-08
대체 대상: [`2026-07-10-birthday-stream-alarm.md`](../../history/plans/2026-07-10-birthday-stream-alarm.md)의 discovery·audience 설계

## 목적

생일 방송 발견 여부를 일반 알람 구독 유무와 분리하고, 생일 방송 알람은 해당 멤버의 같은 날짜 생일축하 메시지가 실제로 전송된 방에만 보냅니다. 기존 v2 위에 별도 poller나 예외 전송 경로를 추가하지 않고, producer target 계약과 dispatch ledger 소유권을 바로잡습니다.

## 핵심 계약

### Discovery audience

- collector LIVE/UPCOMING discovery audience는 enabled operational roster 전체입니다.
- videos, shorts, community 알림 후보는 계속 notification subscription target만 사용합니다.
- channel stats도 operational target을 사용하므로 내부 target 이름은 `OperationalChannelIDs`, target group은 `operational`로 통일합니다.
- Holodex live provider가 있으면 `live_batch` scheduler job 하나가 roster snapshot을 소유하고 실행 시 최대 40개씩 나눠 조회합니다. target refresh는 poller와 budget profile이 함께 든 immutable snapshot으로 scheduler job을 교체합니다.
- Holodex 장애 시 scraper fallback 회전 상태는 각 channel set별로 분리해, 크기가 다른 chunk를 연속 조회해도 특정 채널이 fallback 대상에서 영구 제외되지 않게 합니다.
- Holodex provider 자체가 구성되지 않은 제한 모드에서는 무제한 per-channel scraper 부하를 만들지 않도록 LIVE도 notification target으로 축소하고 `youtube_producer_live_discovery_subscription_fallback` 경고를 남깁니다. 구성된 provider의 일시 장애는 기존 bounded scraper fallback을 사용합니다.

### Delivery audience

- 생일축하 event key는 `celebration:birthday:{channelID}:{YYYY-MM-DD}`입니다.
- 생일 방송 runner는 이 event에 연결된 `alarm_dispatch_deliveries` 중 `status='sent' AND sent_at IS NOT NULL`인 방만 조회합니다.
- event key뿐 아니라 `alarm_type='BIRTHDAY'`, `category='celebration'`도 확인합니다.
- audience 조회 실패는 fail-closed입니다. 전체 방이나 일반 알람 방으로 fallback하지 않습니다.
- 조회는 event key 묶음을 한 번에 처리하며 migration 061의 `idx_alarm_dispatch_deliveries_sent_event_room` partial index를 재사용합니다. 신규 테이블이나 migration은 없습니다.

### Idempotency와 late convergence

- birthday stream event identity는 기존 `celebration:birthday_stream:{channelID}:{date}:{videoID}`를 유지합니다.
- 이미 event가 생성된 현재 세션도 이후 tick에서 다시 publish 후보에 포함합니다. 그 사이 생일축하가 성공한 방이 추가되면 같은 event와 새 room 조합만 delivery unique key로 삽입됩니다.
- 기존 방의 envelope도 다시 제출되지만 outbox event/delivery dedupe가 외부 중복 발송을 막습니다.
- 멤버·일자당 신규 stream event는 최대 3개입니다. 기존 event 재평가는 이 cap을 새 event로 소비하지 않으며, 한 tick의 publish 후보도 3개를 넘지 않습니다.

## 부하와 지연

- operational roster가 45개라면 40개 chunk 기준 primary LIVE poll 한 번에 Holodex 요청 2개입니다. 기본 2분 주기에서는 약 1 Holodex RPM입니다.
- scheduler budget은 chunk 수, roster 수만큼의 PostgreSQL write unit, YouTube scraper fallback fault envelope를 target snapshot과 함께 갱신합니다.
- 최악 감지 지연은 producer LIVE poll 주기와 birthday stream runner 주기의 합입니다. 기본값 기준 최대 약 32분이며, freshness 기본 30분 계약은 유지됩니다.
- Holodex provider 미구성 시 coverage 축소는 의도된 제한 모드입니다. 정상 운영에서 이 경고가 보이면 birthday stream full-roster coverage를 보장할 수 없습니다.

## Rollout

혼합 버전에서 구 worker는 전체 방으로 fan-out하므로 다음 중 하나를 지켜야 합니다.

1. 권장: 새 alarm-worker를 먼저 배포해 sent-birthday audience를 적용한 뒤 새 youtube-collector를 배포합니다. 중간 구간에는 구독 없는 멤버 discovery만 일시적으로 빠지고 과다 발송은 없습니다.
2. producer-first가 필요하면 먼저 `BIRTHDAY_STREAM_RUNNER_ENABLED=false`로 runner를 중지하고, producer와 worker를 모두 배포한 뒤 다시 활성화합니다.

활성화 전 확인:

- producer에 `live_batch` global job 하나가 있고 `operational` target 수가 roster 수와 일치합니다.
- `youtube_producer_live_discovery_subscription_fallback` 경고가 없습니다.
- 구독이 없는 생일 멤버의 UPCOMING/LIVE가 `youtube_live_sessions`에 나타납니다.
- 해당 날짜 birthday event의 sent delivery 방 집합과 birthday stream delivery 방 집합이 일치합니다.

## Rollback

- audience 변경 이후 구 alarm-worker로 rollback하기 전에는 반드시 `BIRTHDAY_STREAM_RUNNER_ENABLED=false`로 중지합니다. 그렇지 않으면 full-roster discovery와 구 all-room fan-out이 결합됩니다.
- producer만 rollback하면 일반 알림 계약은 유지되지만 구독 없는 멤버의 birthday stream discovery coverage가 다시 사라집니다.
- 신규 schema가 없으므로 DB rollback은 필요하지 않습니다. 이미 생성된 outbox delivery는 기존 consumer가 정상적으로 drain합니다.

## 검증 범위

- producer: operational/notification target 분리, single batch registration, 40개 chunk, runtime snapshot·budget replacement, provider 미구성 제한 모드, multi-chunk fallback 공정성
- worker: exact birthday audience, 다른 멤버 audience 격리, empty/failure fail-closed, known event republish, late sent room, 신규 event cap
- PostgreSQL: 실제 dispatch event/delivery ledger에서 sent room만 반환하는 integration test

## v9.2.2: 제목 판정과 현지 생일 창 (2026-10-13)

위 설계의 "생일 당일 본인 채널 방송은 전부 유효"와 KST 하루 창은 v9.2.2(#627)에서 바뀌었다. 현재 계약은 `docs/current/contracts/alarm.md`가 소유한다.

- **바뀐 계약:** 판정 창(KST 생일 전날 21:00 ~ 다음 날 17:00) 안에서 제목에 생일 키워드가 있는 와꾸만 생일 방송이다. 멤버·일자 상한 3건과 제목 변경 뒤 재평가는 저장 제목이 생일로 판정되는 기발행 event만 세며, 저장 제목이 생일과 무관한 기발행 event는 다시 보내지 않는다. 다른 KST 날짜 방송은 `01/02 15:04`로 표시한다.
- **이유:** 2026-10-13 크레이지 올리(ID) 생일 00:27에 카운트다운과 함께 hololive Dreams 스폰서 방송·SARUSHIMA VLOG 최초공개가 생일 방송으로 나가 상한 3건을 모두 썼다. 9.2.0이 계속 돌았다면 그날의 실제 생일 방송 알림은 막혔다. KST 하루 창은 ID·EN 멤버의 현지 생일 저녁 방송도 놓쳤다.
- **리뷰:** 서브에이전트 적대적 리뷰 2건(정합성, 운영·계약)에서 blocker·major는 없었다. 반영한 minor는 잘못 발행된 event의 재발송 차단, 키워드 보강(`バースディ`·`たんじょうび`·`誕生会`), 카운트다운 서술 축소, event key 표기 정정, runbook 진단·롤백 보강, 상한 경계·DB prefix·앞쪽 창 표시 테스트다.
- **게시:** #627 `1875c7083`. CI 11개 통과, 병합 tree는 검증한 브랜치와 같다. annotated tag `v9.2.2`와 GitHub Release(`publish-release.sh v9.2.2 v9.2.1`)를 게시했다. 같은 릴리스에 #625(문서)와 #626(native AP logrotate)이 포함된다.
- **배포:**
  - `hololive-alarm-worker:release-1875c708391a-r1-arm64`(`sha256:1f14ebcb…`, arm64, 9.2.2)를 원격에 load해 같은 ID·revision을 확인했다.
  - 9.2.0 worker를 `hololive-alarm-worker:rollback-20261012T172106Z`로 보존하고 VERSION 2개를 `deploy-backups/pre-v9.2.2-20261012T172106Z`(tar·sha256)에 백업했다. 원격 tree에서 바뀌는 파일은 이 2개뿐이며 소유자·모드(664·644)를 유지했다.
  - `change_started_at=2026-10-12T17:21:13Z`에 `up -d --no-build --no-deps hololive-alarm-worker`로 교체했고 health gate를 6초에 통과했다. migration 없음.
- **수용 증거:** worker healthy, 재시작 0회, 시작 로그 `version=9.2.2`, 실행 compose 파일 4개 동일, 교체 뒤 로그는 모두 INFO(ERROR·panic 0)이며 다른 컨테이너는 재시작되지 않았다. 첫 tick에서 올리의 `【Ollie's Ramen Birthday Party - VRCHAT】`(21:00 KST)를 새 생일 방송으로 발행해 생일축하를 받은 3개 방에 모두 전송했고, 카운트다운은 재평가 중복으로 끝났으며 Dreams·VLOG는 다시 나가지 않았다(read-only guard 조회).
- **현재 rollback:** `rollback-20261012T172106Z`를 `prod`로 되돌리고 백업 tar로 VERSION 2개를 복원한 뒤 `--no-build --no-deps hololive-alarm-worker`로 다시 띄운다. 제목 판정과 확장 창이 사라지고, 저장 제목이 생일과 무관한 event가 다시 상한을 차지한다.
- **남은 공백:** 키워드 없는 생일 방송은 알리지 않는다. 같은 `channel_id`를 쓰는 멤버의 생일이 인접하면 확장 창이 겹쳐 같은 방송이 두 멤버 생일로 나갈 수 있다(저장소 데이터상 UNIT B는 멤버 한 행이라 해당 없음, 운영 DB 중복 여부는 미확인). tick 실행 시간만큼 다음 날 17시 경계 직전 세션을 놓칠 수 있다(기존 자정 경계와 같은 성질).
