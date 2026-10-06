# Official Schedule API Runtime Contract

## 상태와 소유권

`Holodex`는 live/upcoming/channel identity의 primary source입니다. 공식 일정 source는 `Holodex`가 실패한 경우 org upcoming을 보충하는 secondary source입니다. channel schedule(`GetChannelSchedule`)과 channel 조회(`GetChannel`, `GetChannels`)는 Holodex만 원천으로 쓰고 실패를 그대로 돌려줍니다(`DEC-20260926-hololive-source-fallbacks-retirement`).

공식 일정 source는 `GET https://schedule.hololive.tv/api/list/2`만 소비합니다. `/lives/hololive` HTML scraper, source mode, API-to-HTML fallback 및 관련 rollback branch는 지원하지 않습니다.

공식 일정 API는 다음 경로에 참여하지 않습니다.

- org/channel live truth
- `GetChannelsLiveStatus`
- live-session open/close
- alarm 전송 여부
- viewer count 및 actual start 판정

API의 `isLive` 값은 live truth로 사용하지 않습니다. 모든 유효한 API row는 `domain.StreamStatusUpcoming`으로 매핑하며 `StartActual`은 `nil`입니다.

## 요청 계약

| 항목 | 계약 |
|---|---|
| Base URL | `OFFICIAL_SCHEDULE_BASE_URL`, 기본 `https://schedule.hololive.tv` |
| Path | `/api/list/2` |
| Method | body 없는 `GET` |
| Accept | `application/json` |
| Redirect용 trailing slash | 사용하지 않음 |
| Timeout | `OFFICIAL_SCHEDULE_TIMEOUT_SECONDS`, 기본 15초 |
| Body limit | `MAX_RESPONSE_BODY_BYTES`, 기본 2MiB |
| Cache expiry | 없음. channel schedule 보충 경로와 그 Valkey 캐시를 `DEC-20260926-hololive-source-fallbacks-retirement`로 삭제했습니다. |
| Process cache | `OFFICIAL_SCHEDULE_PAGE_CACHE_TTL_SECONDS`, 기본 15초 |

Base URL은 HTTPS origin이어야 하며 userinfo, path, query, fragment를 포함할 수 없습니다. startup validation과 request construction에서 모두 fail closed합니다.

성공 응답은 다음 조건을 모두 충족해야 합니다.

1. HTTP status가 `200`입니다.
2. `Content-Type`을 `mime.ParseMediaType`으로 해석한 media type이 `application/json`입니다.
3. JSON root가 object입니다.
4. `dateGroupList`가 array입니다.
5. 각 group의 `videoList`가 array입니다.

unknown field는 허용합니다. malformed JSON은 decode 오류, root/container type drift는 schema 오류로 구분합니다.

response body는 모든 status/content-type/read 경로에서 닫습니다. 성공 body와 오류 body의 drain은 bounded입니다. body limit 초과는 `httputil.ErrResponseBodyTooLarge` 원인을 보존합니다.

## Row 처리 계약

각 `videoList` item은 독립적으로 decode합니다. 한 row의 field drift는 다른 정상 row를 버리지 않습니다.

유효 row 조건은 다음과 같습니다.

- URL scheme은 `https`입니다.
- host는 `youtube.com` 또는 `www.youtube.com`입니다.
- path는 `/watch`입니다.
- `v` query는 ASCII 영숫자, `-`, `_`로만 구성된 non-empty ID입니다.
- `datetime`은 `2006/01/02 15:04:05` 형식이며 `Asia/Tokyo`에서 parse됩니다.
- `name` 또는 `talent.name`이 non-empty입니다.

일부 invalid row는 skip하고 metric에 기록합니다. non-empty payload의 모든 row가 invalid이면 structure-drift 오류를 반환합니다. schema-valid empty `dateGroupList` 또는 empty `videoList`는 성공-empty입니다.

video identity는 검증된 `v` query입니다. output link는 `https://www.youtube.com/watch?v={id}`로 canonicalize합니다. 같은 video ID가 반복되면 첫 유효 row를 기준으로 deduplicate하고, 뒤 row의 non-empty provider title/HTTPS thumbnail만 보완합니다.

## Field mapping

| API field | `domain.Stream` |
|---|---|
| YouTube `v` | `ID` |
| canonical watch URL | `Link` |
| `title`, 비어 있으면 talent name | `Title` |
| `name`, 비어 있으면 `talent.name` | `ChannelName` |
| exact identity index 결과 | `ChannelID` |
| `datetime` in Asia/Tokyo | `StartScheduled` |
| valid HTTPS `thumbnail`, 아니면 YouTube fallback URL | `Thumbnail` |
| 고정 | `StatusUpcoming` |
| 고정 | `StartActual=nil` |

`collaboTalents`는 owner 판정에 사용하지 않습니다. talent icon URL도 현재 domain에 저장하지 않습니다.

공식 collector는 `collaboTalents[].name`만 guest evidence로 보존합니다. 빈 배열은 솔로 일정으로 해석하며, 호스트 자신의 이름은 제외합니다. 이 값은 `youtube_schedule_items.collabo_talent_names`에 저장되고, 기존 호스트 live/upcoming Kakao 알림에 표시용 `콜라보:` 한 줄로만 붙습니다. collab 멤버 구독자에게 알림을 팬아웃하지 않습니다. Holodex mentions는 사용하지 않습니다.

## Channel identity

공식 API는 stable channel ID를 제공하지 않습니다. runtime은 member data가 소유하는 다음 값을 normalized exact key로 인덱싱합니다.

- `Name`
- `NameJa`
- `NameKo`
- `ShortKoreanName`
- `Aliases.Ko`
- `Aliases.Ja`

하나의 normalized key가 정확히 하나의 distinct ChannelID를 가리킬 때만 매핑합니다. 같은 ChannelID로 수렴하는 중복 alias는 하나로 인정합니다. 서로 다른 ChannelID가 충돌하거나 key가 없으면 `ChannelID`를 비워 둡니다.

unmapped stream은 org upcoming 결과에 유지합니다. partial/contains matching은 사용하지 않습니다. 식별 색인을 만들 멤버 데이터를 적재하지 못하면 빈 색인으로 두지 않고 scraper service 생성(기동)이 실패합니다(`DEC-20260926-hololive-source-fallbacks-retirement`).

## Fallback semantics

### Org upcoming

```text
cache -> Holodex /live
      -> 모든 대상 org가 실패했을 때만 Official Schedule API
```

Holodex success-empty는 authoritative empty이며 공식 API를 호출하지 않습니다. API fallback 결과에는 caller의 `hours` window와 기존 list limit을 적용합니다.

모든 대상 org가 성공한 결과만 캐시합니다. `org=all`에서 일부 org만 성공하면 성공한 org의 stream과 함께 `PartialStreamsError`(실패 org 목록 포함)를 반환하고 캐시하지 않습니다. Stream HTTP API는 이 오류를 500으로 응답하고 부분 목록을 내보내지 않습니다(부분 응답 필드는 별도 결정 전 추가하지 않음). 공식 API fallback이 stream을 하나도 찾지 못하면 빈 성공을 만들지 않고 primary 오류를 반환하며 캐시하지 않습니다. 호출자 context 취소로 끝난 primary는 fallback·재시도·캐시 없이 취소 오류를 반환합니다.

workspace 예외 형식에 맞춰 다음을 함께 지킵니다(2026-10-02 보강).

| 항목 | 내용 |
|---|---|
| Trigger | `org=hololive` upcoming 조회에서 primary가 stream을 하나도 얻지 못했고 실패한 org가 있음 |
| 한도 | 요청당 공식 일정 조회 한 번이며(origin fetch는 아래 cache·singleflight를 공유) 재시도하지 않습니다. caller의 `hours` window와 기존 list limit을 적용합니다. |
| 종단 | 공식 API 오류나 빈 결과이면 primary 오류를 반환하고 캐시하지 않습니다. |
| Telemetry | `hololive_fallback_primary_total{service="holodex"}`, `hololive_fallback_execution_total{service="holodex",trigger="on_empty_primary_with_error",outcome}`(`skipped`, `error`, `hit`, `miss`), `hololive_holodex_official_schedule_fallback_total`과 Warn 로그 |
| Owner | `hololive-shared/pkg/service/holodex/provider`(실행)와 `internal/service/holodex/provider/htmlscraper`(공식 API 조회) |
| 검토 조건 | `outcome="hit"`이 90일 동안 0이면 이 fallback을 지우고 Holodex 실패를 그대로 오류로 반환합니다. Holodex upcoming이 공식 일정 API를 대체할 수 없게 되면(Holodex 퇴역 등) 공식 API를 primary로 올리고 이 예외를 지웁니다. |

이 fallback만 쓰던 범용 실행기 `internal/service/fallback`은 2026-10-02 provider 안으로 합쳤습니다. 위 metric의 이름과 label 구성은 그대로입니다.

### Channel schedule

```text
channel cache -> Holodex /live?channel_id=...
              -> 실패는 그대로 오류(보충·캐시 없음)
```

Holodex 실패를 YouTube channel source나 공식 API로 보충하던 경로, 그 보충 결과를 5분(`CacheTTL.ChannelSchedule`) 다시 캐시하던 provider 경로, htmlscraper의 1분 보충 TTL은 `DEC-20260926-hololive-source-fallbacks-retirement`(PLN-20260926-stack-audit-refactoring T19)로 함께 삭제했습니다. 성공한 Holodex 결과만 `CacheTTL.ChannelSchedule` 동안 캐시합니다. T18(2026-09-26) 30일 로그에서 이 경로의 fallback 경고는 0건이었습니다.

### Channel

`GetChannel`은 Holodex `/channels/{id}` 하나만 원천으로 쓰고, 실패를 YouTube scraper로 만든 부분 Channel(구독자 수·아바타만 채운 값)로 보충하거나 캐시하지 않습니다. `GetChannels`는 목록 API(`/channels?org=Hololive`) 실패를 개별 `/channels/{id}` 조회로 보충하지 않고 `get channels batch list` 오류로 돌려줍니다.

### Live

Holodex live primary가 실패하면 source failure를 반환합니다. 공식 일정의 upcoming-only 결과로 success-empty를 만들지 않습니다. `GetChannelsLiveStatus`의 YouTube live-status scraper fallback은 `DEC-20260926-hololive-live-status-scraper-fallback-removal`로 삭제했으며, alarm-worker는 이 source failure를 받으면 persisted live session으로 판단합니다.

## Cache와 동시성

공식 API origin fetch는 process-local short TTL cache와 `singleflight.DoChan`으로 deduplicate합니다. shared fetch는 caller cancellation과 분리하되 configured timeout으로 bounded합니다. 각 waiter는 자신의 context cancellation을 즉시 관찰합니다.

cache에 저장하거나 caller에게 반환할 때 `domain.Stream`과 pointer field를 clone하여 caller mutation이 다른 요청으로 전파되지 않게 합니다.

## 관측성

기존 metric 이름과 label set은 유지합니다. 공식 일정 보충 결과(org upcoming, `official_schedule_page`)는 이 metric 하나로만 기록합니다. `operation="channel_schedule"` 시계열은 channel schedule 보충 경로 삭제로 더 생기지 않습니다.

```text
hololive_holodex_official_schedule_fallback_total{operation,outcome,reason}
```

`hololive_fallback_primary_total{service="youtube",operation="channel_schedule",outcome}`도 같은 이유로 더 생기지 않습니다. 호출자 취소로 끝난 primary는 실패가 아니라 `outcome="canceled"`입니다. `outcome="canceled"`는 호출자 context 취소만 뜻합니다. 호출자와 분리된 자체 단계 예산이 끝나 못 마친 target(예: `channel_statistics`의 scraper 단계 timeout)은 원천 실패이므로 `failed`/`partial`로 기록합니다.

API source 관측 metric은 다음과 같습니다.

```text
hololive_official_schedule_requests_total{source="api",outcome,reason}
hololive_official_schedule_request_duration_seconds{source="api",outcome}
hololive_official_schedule_response_bytes{source="api"}
hololive_official_schedule_rows_total{source="api",result}
hololive_official_schedule_last_success_timestamp_seconds{source="api"}
```

`reason`과 `result`는 bounded enum입니다. URL, title, talent name, ChannelID 및 payload를 metric label이나 log에 넣지 않습니다.

## 실패 분류

요청 및 decode 오류는 다음 bounded reason으로 구분합니다.

- `request`
- `context`
- `transport`
- `status`
- `content_type`
- `decode`
- `schema`
- `oversize`
- `unknown`

API-only contract이므로 status/schema/decode/oversize 실패 후 HTML을 호출하지 않습니다. synchronous retry도 이 source layer에 추가하지 않습니다.

## 검증

unit/race 검증은 외부 endpoint 없이 다음 계약을 고정합니다.

- exact method/path/header
- JSON media type과 core schema
- unknown field 허용
- partial invalid row 보존
- all-invalid structure drift
- canonical YouTube identity와 dedup
- full-year JST timestamp
- `isLive` non-authority
- exact/ambiguous channel identity
- body close와 2MiB bound
- cache clone과 TTL
- concurrent request singleflight
- leader cancellation과 waiter 독립성
- Holodex live path에서 official request 0회
- Holodex success-empty에서 official request 0회
- channel fallback 순서

live integration test는 `integration` build tag로만 실행하며, 결과가 non-empty라고 가정하지 않습니다.
