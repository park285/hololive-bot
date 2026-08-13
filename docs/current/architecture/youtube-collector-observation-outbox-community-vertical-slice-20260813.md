# YouTube Collector / Source Observation Outbox: Community Vertical Slice

## 상태

이 문서는 YouTube Collector / Source Observation Outbox 전환의 첫 구현 단위인 Community end-to-end vertical slice의 저장소 기준 계약을 고정한다.

현재 변경의 범위는 다음과 같다.

1. Community domain 처리 로직을 외부 수집과 분리한다.
2. PostgreSQL에 source checkpoint와 source observation outbox를 추가한다.
3. Collector가 checkpoint 갱신과 observation 발행을 하나의 transaction으로 기록할 수 있게 한다.
4. Producer가 외부 API를 호출하지 않고 observation을 shadow consume할 수 있게 한다.
5. legacy와 shadow 결과의 parity를 측정하고 authority fence가 허용할 때만 authoritative write를 수행한다.

Community authoritative 전환 자체와 운영 migration 적용은 이 변경에 포함하지 않는다. 기본 authority는 legacy이며 shadow consumer는 canonical table과 notification outbox를 변경하지 않는다.

## 핵심 invariant

- Collector는 `youtube_community_posts`, `youtube_content_watermarks`, `youtube_content_alarm_tracking`, `youtube_notification_outbox`를 직접 수정하지 않는다.
- Collector의 checkpoint와 observation insert는 동일한 PostgreSQL transaction이다.
- Producer의 canonical persistence와 observation processed 전이는 동일한 PostgreSQL transaction이다.
- 동일 observation은 `source_kind`, `source_key`, `observation_key`, `schema_version` 조합으로 멱등 처리한다.
- claim은 bounded batch와 lease를 사용하며, lease 만료 observation만 재수행할 수 있다.
- 알 수 없는 schema version, 잘못된 payload, stale authority generation은 canonical write 전에 fail closed한다.
- shadow mode는 비교 결과만 기록하고 canonical state와 notification outbox를 변경하지 않는다.
- authority mode가 `authoritative`인 경우에도 generation fence가 일치하지 않으면 처리하지 않는다.
- Community watermark는 Collector checkpoint와 별도이다. Collector checkpoint는 source fetch 연속성을, Producer watermark는 domain freshness를 소유한다.

## Observation envelope

Community observation의 envelope는 다음 필드를 가진다.

- `id`: PostgreSQL에서 생성한 observation 식별자
- `source_kind`: `youtube_community`
- `source_key`: YouTube channel ID
- `observation_key`: source가 제공한 최신 collection identity
- `schema_version`: 현재 `1`
- `observed_at`: source 수집 시각
- `completeness`: Community에서는 `COMPLETE_WINDOW` 또는 `PARTIAL_WINDOW`
- `continuity`: `CONTIGUOUS` 또는 `GAP_UNRESOLVED`
- `payload`: 정규화된 Community post 배열과 collection metadata
- `payload_sha256`: canonical JSON payload의 SHA-256

Community payload는 upstream post ID, channel ID, author metadata, content text, published text/time, like/comment count, image metadata, attached video ID를 포함한다. Producer가 canonical content ID를 계산하므로 Collector는 notification identity를 생성하지 않는다.

## PostgreSQL 소유 경계

### source_collection_checkpoints

Collector만 갱신한다. source별 마지막 성공 수집 identity, continuity 상태, collection 시각, generation을 보존한다.

### source_observation_outbox

Collector가 insert하고 Producer가 claim/processed/DLQ 상태를 갱신한다. payload는 immutable이며 retry 상태만 갱신한다.

### source_observation_consumer_offsets

consumer별 마지막 완료 observation과 lag 관측값을 보존한다. correctness는 개별 observation 상태가 소유하며 offset은 운영 관측 및 wakeup 최적화에 사용한다.

### source_authority_fences

source kind별 `legacy`, `shadow`, `authoritative` mode와 monotonic generation을 보존한다. Collector와 Producer는 읽은 generation을 write predicate에 포함한다.

## Community 처리 단계

1. Collector는 target set에서 channel을 선택하고 source별 fixed interval과 deterministic spread를 적용한다.
2. bounded worker가 YouTube source를 수집한다.
3. 수집 결과를 정규화하고 canonical JSON hash를 계산한다.
4. 직전 checkpoint와 동일한 observation identity/hash이면 observation을 발행하지 않고 checkpoint success metadata만 갱신한다.
5. 변화가 있으면 checkpoint update와 outbox insert를 하나의 transaction으로 commit한다.
6. Producer shadow consumer는 pending observation을 bounded batch로 claim한다.
7. Community processor가 legacy processor와 동일한 canonical artifacts를 계산한다.
8. shadow mode에서는 persisted legacy artifacts와 parity를 비교하고 mismatch reason을 기록한다.
9. authoritative mode에서는 authority generation을 재검증한 뒤 canonical persistence와 observation processed 전이를 하나의 transaction으로 commit한다.
10. retry budget을 소진하거나 payload/schema가 영구적으로 잘못된 observation은 DLQ 상태로 전환한다.

## 검증 기준

- migration 전체 재생과 schema snapshot이 통과한다.
- checkpoint update와 observation insert가 원자적임을 transaction rollback test로 검증한다.
- duplicate observation identity가 추가 row를 만들지 않음을 검증한다.
- claim lease가 중복 active processing을 허용하지 않음을 검증한다.
- lease 만료 후 recovery가 가능함을 검증한다.
- unsupported schema, malformed payload, stale generation이 canonical write 전에 거부됨을 검증한다.
- shadow mode가 canonical tables와 notification outbox를 수정하지 않음을 검증한다.
- authoritative mode의 domain write와 processed 전이가 함께 commit 또는 rollback됨을 검증한다.
- Community legacy와 observation processor가 동일 input에 대해 동일 canonical post, tracking, notification, watermark를 계산함을 검증한다.

## 롤아웃

1. migration을 적용하되 authority는 `legacy`로 유지한다.
2. Collector를 single-active/standby로 기동하고 outbox lag와 error를 관측한다.
3. Producer shadow consumer를 활성화하고 parity mismatch가 없는지 확인한다.
4. Community authority generation을 증가시키면서 `authoritative`로 전환한다.
5. rollback 시 generation을 다시 증가시키고 mode를 `legacy`로 되돌린다.

Shorts, Holodex Live, 일반 영상·통계·Photo Sync는 Community vertical slice가 검증된 뒤 같은 envelope와 authority 계약을 재사용하되 kind별 completeness 정책은 별도로 유지한다.
