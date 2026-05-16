# YouTube Community/Shorts Continuous Observation Report

- generated at: `2026-04-15T12:34:56Z`
- observation runtime: `youtube-scraper`, cutover: `2026-04-15T00:00:00Z`
- observation status: `finalized`
- observation window: `2026-04-15T00:02:00Z` -> `2026-04-16T00:02:00Z`
- deployment completed at: `2026-04-15T00:02:00Z`, observed until: `2026-04-16T00:02:00Z`
- target channels: `1`, app version: `2.1.0`
## 24h Closeout

- scope: `all_operational_channels`, target_channels=`1`, observed_posts=`1`, period_label=`observation_window`
- internal over-2m closeout: status=`pass`, internal_system_cause_posts=`0`, over_2m_posts=`0`, non_internal_system_cause_posts=`0`, excluded_external_collection_posts=`0`, rule=`internal_system_cause_posts == 0`
- closeout statement: Finalized observation recorded internal_system_cause_posts=0.
- missing alarm closeout: status=`pass`, reference_posts=`1`, send_state_posts=`1`, missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`, rule=`missing_alarm_posts == 0`
- missing alarm statement: Finalized observation recorded missing_alarm_posts=0.
- state consistency closeout: status=`pass`, reference_posts=`1`, send_state_posts=`1`, duplicate_sent_posts=`0`, missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`, rule=`duplicate_sent_posts == 0 && missing_alarm_posts == 0`
- state consistency statement: Finalized observation recorded duplicate_sent_posts=0 and missing_alarm_posts=0.
## Target Baseline

- generated at: `2026-04-15T12:34:56Z`
- final delivery owner: `alarm-worker`, big-bang enabled: `true`
- runtime target channels: `1`, channel rows: `1`

| channel_id | owner | community_enabled | community_rooms | community_mode | shorts_enabled | shorts_rooms | shorts_mode |
| --- | --- | --- | ---: | --- | --- | ---: | --- |
| `UC_TEST` | `Member One` | `true` | 2 | `new_only` | `false` | 0 | `disabled` |

## YouTube Community/Shorts Channel Delivery Summary

- generated at: `2026-04-15T12:34:56Z`
- window: `2026-04-15T00:02:00Z` -> `2026-04-16T00:02:00Z`
- summary: channels=`0`, detected_posts=`0`, alarm_sent_posts=`0`, success_posts=`0`, failed_posts=`0`, detected_unsent_posts=`0`, community_detected_posts=`0`, shorts_detected_posts=`0`

최근 윈도우에 해당하는 community/shorts 감지 채널이 없습니다.


## YouTube Community/Shorts Post Send Counts Report

- generated at: `2026-04-15T12:34:56Z`
- mode: `observation_window`
- window: `2026-04-15T00:02:00Z` -> `2026-04-16T00:02:00Z`
- observation runtime: `youtube-scraper`, cutover: `2026-04-15T00:00:00Z`
- summary: posts=`0`, successful_posts=`0`, zero_success_posts=`0`, duplicate_success_posts=`0`, failed_attempt_posts=`0`, outbox_missing_posts=`0`, external_collection_source_posts=`0`, internal_delivery_source_posts=`0`, mixed_delay_source_posts=`0`, queue_wait_cause_posts=`0`, retry_accumulation_cause_posts=`0`, job_failure_cause_posts=`0`
- duplicate alarm verdict: status=``, duplicate_posts=`0`, rule=``

조회된 community/shorts 게시물이 없습니다.


## YouTube Community/Shorts Alarm Sent History Dataset

- generated at: `2026-04-15T12:34:56Z`
- observation runtime: `youtube-scraper`, cutover: `2026-04-15T00:00:00Z`
- window: `2026-04-15T00:00:00Z` -> `2026-04-16T00:00:00Z`
- summary: collected_rows=`1`, duplicates_removed=`0`, sent_posts=`1`, community_posts=`1`, shorts_posts=`0`, baseline_posts=`1`, matched_posts=`1`, unsent_posts=`0`, duplicate_sent_posts=`0`, unexpected_sent_posts=`0`, identifier_mismatch_candidates=`0`, verification_rows=`1`, reference_rows=`1`, send_state_posts=`1`, missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`, earliest_alarm_sent_at=`2026-04-15T02:01:30Z`, latest_alarm_sent_at=`2026-04-15T02:01:30Z`
### Results

- missing alarm aggregation: missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`
- omission closeout: 누락 0건입니다.
#### By Alarm Type


| alarm_type | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `COMMUNITY` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
#### By Channel


| channel_id | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `UC_TEST` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
### Missing Alarm Rows

누락 알람 게시물이 없습니다.
### Verification Rows


| verdict | reason | alarm_type | channel_id | post_key | post_id | content_id | baseline_count | sent_count | actual_published_at | detected_at | alarm_sent_at | match_published_at | match_title_hint | match_basis | review_status | related_baseline_post_ids | related_sent_post_ids |
| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `matched` | `canonical_identifier_matched` | `COMMUNITY` | `UC_TEST` | `COMMUNITY/UC_TEST/community:test-post` | `community:test-post` | `test-post` | 1 | 1 | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `2026-04-15T02:01:30Z` | `(none)` | `` | `` | `` | `` | `` |
### Normalized Verification Reference Rows


| alarm_type | channel_id | channel_post_key | post_id | actual_published_at | detected_at | verification_verdict | verification_reason | sent_count | review_status | related_sent_post_ids |
| --- | --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |
| `COMMUNITY` | `UC_TEST` | `UC_TEST/community:test-post` | `community:test-post` | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `matched` | `canonical_identifier_matched` | 1 | `` | `` |
### Normalized Sent History Rows


| alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `COMMUNITY` | `UC_TEST` | `COMMUNITY/UC_TEST/community:test-post` | `community:test-post` | `test-post` | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `2026-04-15T02:01:30Z` |


## YouTube Community/Shorts Delivery Logs Report

- generated at: `2026-04-15T12:34:56Z`
- mode: `observation_window`
- observation runtime: `(none)`, cutover: `(none)`
- summary: logs=`0`, success_logs=`0`, failure_logs=`0`, unique_posts=`0`, unique_rooms=`0`, limit=`0`, truncated=`false`

조회된 community/shorts 발송 로그가 없습니다.


## YouTube Community/Shorts Latency Period Report

- generated at: `2026-04-15T12:34:56Z`
- periods: `0`

조회된 community/shorts 지연 기간 집계가 없습니다.


## YouTube Community/Shorts Latency Cause Report

- generated at: `2026-04-15T12:34:56Z`
- mode: `observation_window`
- window: `2026-04-15T00:02:00Z` -> `2026-04-16T00:02:00Z`
- observation runtime: `youtube-scraper`, cutover: `2026-04-15T00:00:00Z`
- observed at basis: `(none)`
- threshold millis: `0`
- internal cause rule: ``
- non internal rule: ``
- excluded external rule: ``
- insufficient evidence rule: ``
- cause evidence fields: ``
- periods: `0`

조회된 community/shorts 지연 원인 리포트가 없습니다.

