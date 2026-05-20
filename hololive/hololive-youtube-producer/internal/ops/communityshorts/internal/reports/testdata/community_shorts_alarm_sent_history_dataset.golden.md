# YouTube Community/Shorts Alarm Sent History Dataset

- generated at: `2026-04-15T12:34:56Z`
- observation runtime: `youtube-producer`, cutover: `2026-04-15T00:00:00Z`
- window: `2026-04-15T00:00:00Z` -> `2026-04-16T00:00:00Z`
- summary: collected_rows=`1`, duplicates_removed=`0`, sent_posts=`1`, community_posts=`1`, shorts_posts=`0`, baseline_posts=`1`, matched_posts=`1`, unsent_posts=`0`, duplicate_sent_posts=`0`, unexpected_sent_posts=`0`, identifier_mismatch_candidates=`0`, verification_rows=`1`, reference_rows=`1`, send_state_posts=`1`, missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`, earliest_alarm_sent_at=`2026-04-15T02:01:30Z`, latest_alarm_sent_at=`2026-04-15T02:01:30Z`
## Results

- missing alarm aggregation: missing_alarm_posts=`0`, missing_send_state_posts=`0`, attempted_missing_posts=`0`, not_sent_missing_posts=`0`
- omission closeout: 누락 0건입니다.
### By Alarm Type


| alarm_type | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `COMMUNITY` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
### By Channel


| channel_id | baseline_posts | sent_posts | matched_posts | unsent_posts | duplicate_sent_posts | unexpected_sent_posts | identifier_mismatch_candidates | missing_alarm_posts |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `UC_TEST` | 1 | 1 | 1 | 0 | 0 | 0 | 0 | 0 |
## Missing Alarm Rows

누락 알람 게시물이 없습니다.
## Verification Rows


| verdict | reason | alarm_type | channel_id | post_key | post_id | content_id | baseline_count | sent_count | actual_published_at | detected_at | alarm_sent_at | match_published_at | match_title_hint | match_basis | review_status | related_baseline_post_ids | related_sent_post_ids |
| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `matched` | `canonical_identifier_matched` | `COMMUNITY` | `UC_TEST` | `COMMUNITY/UC_TEST/community:test-post` | `community:test-post` | `test-post` | 1 | 1 | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `2026-04-15T02:01:30Z` | `(none)` | `` | `` | `` | `` | `` |
## Normalized Verification Reference Rows


| alarm_type | channel_id | channel_post_key | post_id | actual_published_at | detected_at | verification_verdict | verification_reason | sent_count | review_status | related_sent_post_ids |
| --- | --- | --- | --- | --- | --- | --- | --- | ---: | --- | --- |
| `COMMUNITY` | `UC_TEST` | `UC_TEST/community:test-post` | `community:test-post` | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `matched` | `canonical_identifier_matched` | 1 | `` | `` |
## Normalized Sent History Rows


| alarm_type | channel_id | post_key | post_id | content_id | actual_published_at | detected_at | alarm_sent_at |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `COMMUNITY` | `UC_TEST` | `COMMUNITY/UC_TEST/community:test-post` | `community:test-post` | `test-post` | `2026-04-15T02:00:00Z` | `2026-04-15T02:00:30Z` | `2026-04-15T02:01:30Z` |
