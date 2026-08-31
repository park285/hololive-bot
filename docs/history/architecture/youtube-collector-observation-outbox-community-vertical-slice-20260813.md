# YouTube Collector / Source Observation Outbox: Community Vertical Slice (historical)

> **중간 구현 기록:** 이 문서는 2026-08-13 Community-only WIP 계약을 설명한다. 최종 목표와 구현 순서는 `youtube-three-provider-convergence-contract-v2-20260814.md`가 대체한다. `legacy/shadow/authoritative`, 중앙 singleton collector, `youtube-producer` consumer는 retired historical vocabulary이며 production rollout 근거로 사용하지 않는다.

## 상태

이 문서는 YouTube Collector / Source Observation Outbox 전환의 첫 구현 단위인 Community end-to-end vertical slice의 저장소 기준 계약을 고정한다.

현재 변경의 범위는 다음과 같다.

1. Community domain 처리 로직을 외부 수집과 분리한다.
2. PostgreSQL에 source checkpoint와 source observation outbox를 추가한다. 스키마는 migration `144`가 소유하며 기본 fence는 `legacy`다.
3. Community 수집은 독립 프로세스 `youtube-collector`가 소유한다. Canonical persist와 observation consume는 `youtube-producer`가 소유한다. Handoff는 같은 PostgreSQL `source_observation_outbox`다. 두 번째 큐를 만들지 않는다.
4. Collector는 매 poll마다 `CommunityPayloadV1`과 envelope를 만들고, fence가 유효하면 항상 `Repository.Publish`한다. Collector는 `youtube_community_posts`, watermark, tracking, `youtube_notification_outbox`를 쓰지 않는다.
5. Producer consumer는 producer lifecycle과 함께 기동한다. `legacy`와 `authoritative`에서 Claim → Finalize하고 canonical persist+notify를 한 트랜잭션에서 한다. `shadow`는 operable mode가 아니다. `TransitionAuthority(shadow)`는 거절하고, DB에 shadow fence가 있으면 consume은 fail-closed다. Producer community poller는 YouTube `GetCommunityPosts`를 하지 않고 production scheduler에 등록되지 않는다.
6. `Repository.TransitionAuthority`가 operable mode(`legacy`, `authoritative`)를 바꾸고 generation을 단조성 있게 올린다. dual-writer cutover가 생기기 전에는 `shadow`를 쓰지 않는다.

Community authoritative 전환 자체와 운영 migration 적용은 이 변경에 포함하지 않는다. 기본 authority는 `legacy`다. live/shorts/videos/stats poller는 이 vertical slice에서 `youtube-producer`에 남는다. Community 정본 fetch는 collector-owned YouTube.js (`youtubei.js`) helper이며, Go collector가 Publish 소유권을 유지한다. production fence를 실제로 뒤집었다는 뜻이 아니다.

## 프로세스 경계

| Runtime | Binary | Compose | DB role | Owns |
|---|---|---|---|---|
| `youtube-collector` | `hololive/hololive-youtube-collector/cmd/runtime/youtube-collector` | `youtube-collector` | `hololive_scraper` | YouTube.js community fetch, normalize, `Repository.Publish` |
| `youtube-producer` | `hololive/hololive-youtube-collector/cmd/runtime/youtube-producer` | `youtube-producer` / AP overlays | `hololive_runtime` | Claim/Finalize, canonical persist, live/shorts/videos/stats, photo sync |

`youtube-collector`는 `github.com/kapu/hololive-youtube-collector` Go 모듈이다. producer 모듈의 extra binary가 아니다.

Collector는 중앙 싱글톤이다. AP a/b/c/d collector 인스턴스를 만들지 않는다. systemd unit 템플릿은 `scripts/deploy/lib/hololive-youtube-collector.service`에 있으며 이 변경은 해당 unit을 설치하거나 enable하지 않는다.

## 핵심 invariant

- Collector 역할은 fetch + normalize + envelope 생성 + Publish다. `youtube_community_posts`, `youtube_content_watermarks`, `youtube_content_alarm_tracking`, `youtube_notification_outbox`를 수정하지 않는다.
- Producer는 Community YouTube fetch를 하지 않는다. canonical persist와 notification outbox writer는 producer consumer다. fence별 writer는 하나다: `legacy`와 `authoritative`는 collector Publish + consumer persist+notify다. `shadow`는 dual-writer가 생길 때까지 거절한다.
- Collector의 checkpoint와 observation insert는 동일한 PostgreSQL transaction이다.
- Producer의 canonical persistence와 observation processed 전이는 동일한 PostgreSQL transaction이다. writer는 `NewBatchCanonicalWriter` → `PersistCommunityPostsTx`이며 nested `PersistCommunityPosts`가 아니다.
- 동일 observation은 `source_kind`, `source_key`, `observation_key`, `schema_version`, `generation` 조합으로 멱등 처리한다. generation이 바뀌면 같은 `observation_key`도 새 row다.
- payload 해시는 post 내용만 포함한다. `CollectedAt`/`ObservedAt`은 해시 밖이며 수집 시각은 envelope `observed_at`과 checkpoint `collected_at`만 소유한다.
- `observation_key`는 수집 window의 안정 identity다. Community에서는 content SHA-256이다. poll timestamp를 쓰지 않는다.
- checkpoint는 collector continuity다. 내용과 generation이 불변하면 observation insert는 건너뛰고 checkpoint success metadata(`collected_at`, `last_success_at`, `collection_latency_ms`)만 갱신한다.
- claim은 current generation만 bounded batch와 lease로 집어 간다. stale/exhausted는 같은 `LIMIT` candidate 집합 안에서만 `DEAD_LETTER`로 보내고, lease 만료 observation만 재수행할 수 있다.
- 알 수 없는 schema version, 잘못된 payload, stale authority generation은 canonical write 전에 fail closed한다.
- `LoadAuthority` 실패와 unsupported mode는 fail-closed다. Collector는 persist도 publish도 하지 않고, consumer는 claim/finalize를 하지 않는다.
- `shadow` mode는 operable이 아니다. parity-only consume는 알림을 떨어뜨리므로 `TransitionAuthority`가 거절한다.
- authority mode가 `authoritative`인 경우에도 generation fence가 일치하지 않으면 처리하지 않는다.
- Community watermark는 Collector checkpoint와 별도이다. Collector checkpoint는 source fetch 연속성을, Producer watermark는 domain freshness를 소유한다.
- Fence 전환은 `Repository.TransitionAuthority`다. 잘못된 mode는 거부하고 generation은 반드시 증가한다. 기존 admin/HTTP control plane이 없으므로 public HTTP API를 추가하지 않는다.
- Collector JobRunGuard identity는 `community_collect`이다. Producer는 Community poller를 등록하지 않으며 consumer identity는 `youtube-community-processor`다. Collector는 global ingestion lease를 잡지 않는다.
- Fallback delta: none.

## Observation envelope

Community observation의 envelope는 다음 필드를 가진다.

- `id`: PostgreSQL에서 생성한 observation 식별자
- `source_kind`: `youtube_community`
- `source_key`: YouTube channel ID
- `observation_key`: 수집 window의 안정 identity. Community payload의 content SHA-256이다. poll timestamp를 쓰지 않는다.
- `schema_version`: 현재 `1`
- `generation`: authority fence generation. identity에 포함된다
- `observed_at`: source 수집 시각
- `completeness`: Community에서는 `COMPLETE_WINDOW` 또는 `PARTIAL_WINDOW`
- `continuity`: `CONTIGUOUS` 또는 `GAP_UNRESOLVED`
- `payload`: 정규화된 Community post 배열. 수집 시각은 포함하지 않는다
- `payload_sha256`: canonical JSON payload의 SHA-256

Community payload는 channel ID와 post 배열만 포함한다. 각 post는 upstream post ID, channel ID, author metadata, content text, published text/time, like/comment count, image metadata, attached video ID를 가진다. Producer가 canonical content ID를 계산하므로 Collector는 notification identity를 생성하지 않는다.

## PostgreSQL 소유 경계

### source_collection_checkpoints

Collector만 갱신한다. source별 마지막 성공 수집 identity, continuity 상태, collection 시각, generation을 보존한다.

### source_observation_outbox

Collector(`hololive_scraper`)는 `SELECT, INSERT`만 가진다. `ON CONFLICT DO NOTHING` insert는 UPDATE 권한이 필요 없다. Producer(`hololive_runtime`)가 claim/processed/DLQ 상태를 갱신한다. payload는 immutable이며 retry 상태만 갱신한다. Collector는 outbox를 UPDATE하지 않는다.

### source_observation_consumer_offsets

consumer별 마지막 완료 observation과 lag 관측값을 보존한다. correctness는 개별 observation 상태가 소유하며 offset은 운영 관측 및 wakeup 최적화에 사용한다. Finalize의 offset upsert는 producer(`hololive_runtime`)만 수행한다. scraper는 이 테이블 DML을 갖지 않는다.

### source_authority_fences

source kind별 `legacy`, `shadow`, `authoritative` mode와 monotonic generation을 보존한다. Collector와 Producer는 읽은 generation을 write predicate에 포함한다. mode 변경은 `TransitionAuthority`가 generation을 올린 뒤에만 성립한다.

## Community 처리 단계

```
Collector (youtube-collector, community_collect)
  YouTube.js fetch + normalize
  ALWAYS build CommunityPayloadV1 + Envelope (content-only hash, stable observation_key)
  NEVER write canonical community tables
  Publish(checkpoint + outbox) when fence mode is valid, including legacy
  invalid/unreadable fence: fail closed, no persist no publish

Producer persist poller (youtube-producer)
  community poller is not registered
  CommunityPoller is persist-test helper only; production persist is CommunityObservationConsumer

Producer consumer (youtube-producer, CommunityObservationConsumer)
  ALWAYS a first-class runtime loop started with youtube-producer lifecycle
  legacy: Claim → PersistCommunityPostsTx + Finalize in ONE tx
  authoritative: Claim → PersistCommunityPostsTx + Finalize in ONE tx → AfterCommit latency
  shadow: not operable. TransitionAuthority rejects it. SQL-seeded shadow fence is fail-closed (no claim, no persist, no PROCESSED)
```

1. Collector는 notification target set에서 channel을 선택하고 source별 fixed interval과 deterministic spread를 적용한다.
2. bounded worker가 collector-owned YouTube.js helper로 community source를 수집한다. HTML `GetCommunityPosts`는 live path가 아니다.
3. 수집 결과를 정규화하고 canonical JSON hash를 계산한다. collection latency는 fetch duration이며 `MaxCollectionLatency`로 cap한다.
4. live fence를 읽는다. 실패하면 publish하지 않는다.
5. 유효한 fence에서 checkpoint update와 outbox insert를 하나의 transaction으로 commit한다. 직전 checkpoint와 동일한 observation key, payload hash, generation이면 observation insert는 건너뛰고 checkpoint success metadata만 갱신한다. generation만 바뀌어도 새 observation row를 발행한다.
6. Producer는 Community YouTube fetch를 하지 않는다.
7. Producer consumer는 pending observation을 bounded batch로 claim한다. quarantine은 잠근 candidate 집합 안에서만 stale/exhausted를 `DEAD_LETTER`로 보낸다. 루프 자체는 항상 살아 있다. `legacy`와 `authoritative`에서 canonical persist+notify를 한 트랜잭션에서 한다.
8. Community processor가 기존 persist helper와 동일한 canonical artifacts를 계산한다. keywords는 collector와 같은 정규화 목록이다.
9. `shadow`는 dual-writer cutover가 생기기 전까지 operable이 아니다. `TransitionAuthority(shadow)`는 거절하고, DB에 shadow fence가 있으면 consume은 fail-closed다. persist를 건너뛰는 parity-only 경로는 알림을 떨어뜨리므로 두지 않는다.
10. authoritative mode에서는 authority generation을 재검증한 뒤 canonical persistence와 observation processed 전이를 하나의 transaction으로 commit한다. Collector가 이미 notification을 쓰지 않았으므로 double-notify가 없다.
11. retry budget을 소진하거나 payload/schema가 영구적으로 잘못된 observation은 DLQ 상태로 전환한다.

## 검증 기준

- Collector Publish가 canonical community/outbox/watermark/tracking row를 만들지 않음을 검증한다.
- Producer consumer가 legacy와 authoritative에서 canonical persist 함을 검증한다.
- `shadow` fence는 claim/persist/PROCESSED 없이 fail-closed임을 검증한다.
- `communitycollector` production source가 persist helper를 import하지 않음을 검증한다.
- checkpoint update와 observation insert가 원자적임을 transaction rollback test로 검증한다.
- duplicate observation identity가 추가 row를 만들지 않음을 검증한다.
- claim lease가 중복 active processing을 허용하지 않음을 검증한다.
- unsupported schema, malformed payload, stale generation이 canonical write 전에 거부됨을 검증한다.
- `TransitionAuthority`가 invalid mode와 `shadow`를 거부하고 operable mode에서 generation을 증가시킴을 검증한다.
- fence를 legacy → authoritative로 바꾸면 collector Publish와 consumer persist+notify가 유지된다. `shadow`는 거절된다.
- producer community poller가 `GetCommunityPosts`를 호출하지 않음을 검증한다.
- collector live path가 HTML `GetCommunityPosts`를 정본 fetch로 쓰지 않고 YouTube.js helper를 쓰는지 검증한다.

## 롤아웃 상태

이 중간 계약의 rollout 순서는 폐기되었다. migration `144`가 production에 적용되지 않았으므로 authority mode를 배포하거나 전환하지 않고, merge 전에 3-provider observation 계약으로 직접 재작성한다. production migration과 배포는 최종 구현 검증 뒤 별도 승인 대상으로 남는다.

Shorts, Holodex Live, 일반 영상·통계·Photo Sync는 이 authority 계약을 재사용하지 않는다. `youtube-three-provider-convergence-contract-v2-20260814.md`의 provider/observation-kind envelope와 source-neutral reconciliation을 따른다.
