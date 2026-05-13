# YouTube Community Shorts Alarm Sent History Dataset

지정한 닫힌 observation window에서 유튜브 커뮤니티/쇼츠 실제 게시 이력과 알람 발송 이력을 하나의 검증 데이터셋으로 묶어 exact-once 대조와 누락 판정을 할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` observation baseline과 같은 window 안의 sent-history row만 포함합니다.
- 실제 발송 완료 상태 판정은 `youtube_content_alarm_tracking.delivery_status = 'SENT'` 와 `alarm_sent_at IS NOT NULL` 기준입니다.
- 닫힌 observation key의 frozen baseline(`youtube_community_shorts_observation_post_baselines`)과 같은 window의 sent-history를 모두 `UTC canonical` 기준으로 정규화합니다.
- direct 대조 기본 키는 `alarm_type + channel_id + post_id` 입니다.
- 결과는 다섯 계층을 함께 제공합니다.
  - `results`: Markdown `Results` 섹션과 동일한 상단 집계입니다. 게시물 유형별(`alarm_type_comparisons`) 및 채널별(`channel_comparisons`) 대조 결과, 누락 건수 집계, `missing_alarm_zero` 여부를 포함합니다.
  - `rows`: window 안의 정규화된 sent-history row 목록입니다.
  - `verification_rows`: baseline 과 sent-history 를 대조한 per-post verdict 목록입니다.
  - `reference_rows`: `channel_id + post_id` 기준으로 baseline 쪽 게시물을 1건씩 정규화한 검증 기준 목록입니다.
  - `missing_alarm_rows`: `reference_rows` 와 finalized send-state 를 게시물 단위로 대조해 아직 성공 발송으로 닫히지 않은 누락 게시물 목록입니다.
- `verification_rows` 에는 `matched`, `unsent`, `duplicate_sent`, `unexpected_sent`, `identifier_mismatch_candidate` verdict가 들어갑니다.
- `reference_rows` 는 `unexpected_sent` 를 제외하고 baseline 쪽 게시물만 남깁니다. `identifier_mismatch_candidate` 는 `related_baseline_post_ids` 를 펼쳐 baseline post ID별 reference row로 유지합니다.
- direct post identifier가 맞지 않아도 `actual_published_at` 와 `title_hint` 가 강하게 일치하면 `identifier_mismatch_candidate` 로 남기고 자동 해소하지 않습니다.

## Execute

repo root에서 실행합니다.

```bash
go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts alarm-sent-history-dataset   -observation-runtime youtube-scraper   -observation-cutover 2026-04-10T00:00:00Z

go run ./hololive/hololive-stream-ingester/cmd/ops/youtube-community-shorts alarm-sent-history-dataset   -observation-runtime youtube-scraper   -observation-cutover 2026-04-10T00:00:00Z   -format json
```

- `-observation-runtime`, `-observation-cutover` 는 반드시 함께 줘야 합니다.
- `-format json` 은 자동 검증 파이프라인 후처리용입니다. JSON 결과의 `results` 필드에는 Markdown `Results` 섹션과 같은 채널별/게시물 유형별 대조 집계와 누락 0건 여부가 들어갑니다.

## Readout

- `rows[].alarm_type`, `rows[].channel_id`, `rows[].post_id`: direct 대조용 canonical post key의 구성 요소입니다.
- `rows[].post_key`: `alarm_type + channel_id + post_id` 를 한 문자열로 정규화한 값입니다.
- `rows[].content_id`: raw tracking row의 원본 콘텐츠 식별자입니다. canonical ID와 raw ID 차이를 역추적할 때 사용합니다.
- `rows[].actual_published_at`: 실제 유튜브 게시 시각입니다. SLA 시작점 검증에 사용합니다.
- `rows[].detected_at`: 내부 최초 감지 시각입니다. 외부 수집 지연 해석에 사용합니다.
- `rows[].alarm_sent_at`: canonical 발송 완료 시각입니다. 같은 게시물의 실제 발송 완료 이력 판정은 이 값을 기준으로 읽습니다.
- `summary.baseline_post_count`: frozen baseline 기준 기대 게시물 수입니다.
- `summary.sent_post_count`: sent-history 기준 게시물 수입니다.
- `summary.matched_post_count`, `summary.unsent_post_count`, `summary.duplicate_sent_post_count`, `summary.unexpected_sent_post_count`: direct canonical key 기준 1차 판정 결과입니다.
- `summary.identifier_mismatch_candidate_count`: direct ID는 다르지만 `actual_published_at` 와 `title_hint` 보강으로 검토 후보가 된 row 수입니다.
- `summary.reference_row_count`: `channel_id + post_id` 기준으로 정규화된 baseline-side 검증 기준 row 수입니다.
- `summary.send_state_post_count`: 같은 observation key에서 finalized send-state 로 읽힌 게시물 수입니다.
- `summary.missing_alarm_post_count`: `reference_rows` 기준으로 성공 발송 증적이 닫히지 않은 누락 게시물 총건수입니다.
- `summary.missing_send_state_post_count`, `summary.attempted_missing_post_count`, `summary.not_sent_missing_post_count`: 누락 건수를 send-state 부재, 시도만 있음, 미전송 상태로 나눈 세부 건수입니다.
- `results.missing_alarm_evaluated`: finalized send-state 와의 누락 대조가 끝났는지 보여 줍니다. 운영 closeout 용도에서는 이 값이 `true` 여야 합니다.
- `results.missing_alarm_zero`: 누락 건수가 0건이면 `true` 입니다. Markdown 출력에서는 `Results` 섹션에 `누락 0건입니다.` 로 함께 적힙니다.
- `results.alarm_type_comparisons[]`: `COMMUNITY`, `SHORTS` 유형별로 `baseline_posts`, `sent_posts`, `matched_posts`, `unsent_posts`, `duplicate_sent_posts`, `unexpected_sent_posts`, `identifier_mismatch_candidates`, `missing_alarm_posts` 를 묶은 대조 집계입니다.
- `results.channel_comparisons[]`: 채널별로 같은 집계를 다시 펼친 결과입니다. 운영자가 누락 0건 여부를 채널 단위로 대조할 때 사용합니다.
- `verification_rows[].post_key`: direct canonical key가 있는 경우 운영 대조에 바로 쓰는 키입니다.
- `verification_rows[].verdict`: `matched`, `unsent`, `duplicate_sent`, `unexpected_sent`, `identifier_mismatch_candidate` 중 하나입니다.
- `verification_rows[].baseline_count`, `verification_rows[].sent_count`: 같은 canonical key 또는 review candidate 묶음에 들어간 baseline/sent row 수입니다.
- `verification_rows[].related_baseline_post_ids`, `verification_rows[].related_sent_post_ids`: review candidate 또는 이상 row를 재추적할 때 쓰는 post ID 목록입니다.
- `reference_rows[].channel_post_key`: `channel_id + post_id` 를 한 문자열로 정규화한 기준 키입니다. 운영 채널 전체 게시물 기준 목록을 그대로 읽을 때 사용합니다.
- `reference_rows[].verification_verdict`: 해당 baseline 게시물이 `matched`, `unsent`, `duplicate_sent`, `identifier_mismatch_candidate` 중 어떤 상태인지 보여 줍니다.
- `reference_rows[].related_sent_post_ids`: review candidate 또는 baseline-side reference row가 어떤 sent post ID와 연결됐는지 보조로 보여 줍니다.
- `missing_alarm_rows[].missing_reason`: `send_state_missing`, `attempted_without_success`, `not_sent` 중 어떤 이유로 누락으로 남았는지 보여 줍니다.
- `missing_alarm_rows[].post_key`: `alarm_type + channel_id + post_id` canonical key입니다.
- `missing_alarm_rows[].send_state`, `missing_alarm_rows[].state_detected_at`, `missing_alarm_rows[].state_alarm_sent_at`: 누락 게시물에 대응되는 finalized send-state 증적이 있으면 함께 보여 줍니다.
- `comparison`: JSON 후처리에서 세부 분류를 그대로 읽고 싶을 때 사용하는 원본 비교 결과입니다.

## Fallback SQL

compose 운영 기준 Postgres는 `localhost:5433` 입니다.

- 아래 SQL은 `rows` 에 들어가는 sent-history 부분만 재현합니다.
- `verification_rows` 의 `unsent` 와 `identifier_mismatch_candidate` 판정은 frozen baseline 비교와 metadata 보강이 필요하므로 command 출력 또는 JSON 결과를 기준으로 읽습니다.

```bash
set -a
source .env
set +a

PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
SELECT *
FROM (
    SELECT
        'COMMUNITY' AS alarm_type,
        track.channel_id,
        CONCAT('COMMUNITY', '|', track.channel_id, '|', track.canonical_content_id) AS post_key,
        track.canonical_content_id AS post_id,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at
    FROM youtube_content_alarm_tracking AS track
    WHERE track.kind = 'COMMUNITY_POST'
      AND track.delivery_status = 'SENT'
      AND track.alarm_sent_at IS NOT NULL
      AND COALESCE(track.actual_published_at, track.detected_at) >= '2026-04-10T00:00:00Z'
      AND COALESCE(track.actual_published_at, track.detected_at) < '2026-04-11T00:00:00Z'
      AND track.detected_at < '2026-04-11T00:00:00Z'

    UNION ALL

    SELECT
        'SHORTS' AS alarm_type,
        track.channel_id,
        CONCAT('SHORTS', '|', track.channel_id, '|', track.canonical_content_id) AS post_key,
        track.canonical_content_id AS post_id,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at
    FROM youtube_content_alarm_tracking AS track
    WHERE track.kind = 'NEW_SHORT'
      AND track.delivery_status = 'SENT'
      AND track.alarm_sent_at IS NOT NULL
      AND COALESCE(track.actual_published_at, track.detected_at) >= '2026-04-10T00:00:00Z'
      AND COALESCE(track.actual_published_at, track.detected_at) < '2026-04-11T00:00:00Z'
      AND track.detected_at < '2026-04-11T00:00:00Z'
) AS sent_history
ORDER BY alarm_sent_at ASC, channel_id ASC, alarm_type ASC, post_id ASC;
SQL
```
