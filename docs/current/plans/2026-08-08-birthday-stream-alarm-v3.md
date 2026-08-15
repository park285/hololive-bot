# 생일 방송 알람 v3 설계

작성: 2026-08-08
대체 대상: `2026-07-10-birthday-stream-alarm.md`의 discovery·audience 설계

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
