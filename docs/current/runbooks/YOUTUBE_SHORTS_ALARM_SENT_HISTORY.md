# YouTube Shorts Alarm Sent History

지정한 닫힌 observation window에서 실제 발송 완료 상태로 확정된 유튜브 쇼츠 알람 게시물 목록을 검증 원본으로 수집할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `NEW_SHORT` observation baseline에 포함된 게시물만 포함합니다.
- 실제 발송 완료 상태 판정은 `youtube_content_alarm_tracking.delivery_status = 'SENT'` 와 `alarm_sent_at IS NOT NULL` 기준입니다.
- 동일 observation key의 frozen baseline(`youtube_community_shorts_observation_post_baselines`)을 기준 집합으로 사용하므로, 관찰 종료 후 늦게 감지된 게시물은 결과에 섞이지 않습니다.
- 결과는 게시물 단위 1행이며, `post_id` 와 canonical `alarm_sent_at` 을 함께 보여 줍니다.
- 같은 observation window 안에서 baseline 전체와 sent-history 전체를 비교한 `comparison` summary를 함께 포함합니다.
- direct post identifier가 맞지 않아도 `actual_published_at` 와 `title_hint` 같은 보조 정보가 강하게 일치하면 `identifier mismatch candidate` 로 묶고 `pending_review` 상태로 남깁니다.

## Execute

repo root에서 실행합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts shorts-alarm-sent-history \
  -observation-runtime youtube-scraper \
  -observation-cutover 2026-04-10T00:00:00Z

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts shorts-alarm-sent-history \
  -observation-runtime youtube-scraper \
  -observation-cutover 2026-04-10T00:00:00Z \
  -format json
```

- `-observation-runtime`, `-observation-cutover` 는 반드시 함께 줘야 합니다.
- `-format json` 은 자동 검증 파이프라인 후처리용입니다.

## Readout

- `post_id`: observation baseline에서 고정된 canonical shorts post identifier입니다.
- `content_id`: sent 상태 tracking row의 content identifier입니다. raw ID와 canonical ID 차이를 역추적할 때 사용합니다.
- `actual_published_at`: sent 상태 tracking row에 저장된 실제 게시 시각입니다. SLA 시작점 검증에 사용합니다.
- `detected_at`: sent 상태 tracking row의 내부 최초 감지 시각입니다. 외부 수집 지연 해석에 사용합니다.
- `alarm_sent_at`: canonical 발송 완료 시각입니다. 같은 게시물의 실제 발송 완료 이력 판정은 이 값을 기준으로 읽습니다.
- `comparison`: frozen baseline 대비 matched / unsent / duplicate / unexpected / identifier mismatch candidate 집계입니다.
- `comparison.verdict_rows`: 각 비교 항목의 `verdict`, `reason`, baseline/sent 개수, 관련 post ID를 한 줄씩 기록한 조회용 결과입니다. 자동 후처리나 운영 판독에서 항목별 판정 사유를 직접 읽을 때 사용합니다.
- `identifier_mismatch_candidates`: direct ID가 맞지 않지만 `actual_published_at` 와 `title_hint` 로 묶인 baseline/sent row 묶음입니다. 자동 해소하지 않고 `pending_review` 상태로 남겨 반자동 검토에 사용합니다.

## Fallback SQL

compose 운영 기준 Postgres는 `localhost:5433` 입니다.

- 아래 SQL은 direct ID로 연결된 sent row 목록만 재현합니다. `identifier_mismatch_candidates` 재구성은 `actual_published_at + title_hint` 보강이 필요하므로 command 출력 또는 JSON 결과를 기준으로 읽습니다.

```bash
set -a
source "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/env}"
set +a

PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
SELECT
    base.post_id,
    track.content_id,
    track.channel_id,
    track.actual_published_at,
    track.detected_at,
    track.alarm_sent_at
FROM youtube_community_shorts_observation_post_baselines AS base
INNER JOIN youtube_content_alarm_tracking AS track
    ON track.kind = base.kind
   AND track.canonical_content_id = base.post_id
WHERE base.runtime_name = 'youtube-scraper'
  AND base.bigbang_cutover_at = '2026-04-10T00:00:00Z'
  AND base.kind = 'NEW_SHORT'
  AND track.delivery_status = 'SENT'
  AND track.alarm_sent_at IS NOT NULL
ORDER BY track.alarm_sent_at ASC, base.post_id ASC;
SQL
```
