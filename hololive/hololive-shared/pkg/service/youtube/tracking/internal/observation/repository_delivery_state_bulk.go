package observation

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const bulkApplyAlarmSentMarksSQL = `
WITH input AS (
	SELECT *
	FROM unnest(
		$1::text[],
		$2::text[],
		$3::text[],
		$4::timestamptz[],
		$5::timestamptz[]
	) AS t(kind, content_id, canonical_content_id, alarm_sent_at, authorized_at)
), deduped_input AS (
	SELECT DISTINCT ON (kind, canonical_content_id)
		kind,
		content_id,
		canonical_content_id,
		alarm_sent_at,
		authorized_at
	FROM input
	WHERE kind <> ''
	  AND canonical_content_id <> ''
	  AND alarm_sent_at IS NOT NULL
	ORDER BY kind, canonical_content_id, alarm_sent_at ASC
), tracking_updated AS (
	UPDATE youtube_content_alarm_tracking AS t
	SET alarm_sent_at = i.alarm_sent_at,
	    alarm_latency_millis = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT)
	    END,
	    alarm_latency_exceeded = CASE
	        WHEN t.actual_published_at IS NULL THEN NULL
	        WHEN CAST(ROUND(EXTRACT(EPOCH FROM (i.alarm_sent_at - t.actual_published_at)) * 1000) AS BIGINT) > 120000 THEN TRUE
	        ELSE FALSE
	    END,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE t.kind = i.kind
	  AND t.canonical_content_id = i.canonical_content_id
	  AND (t.alarm_sent_at IS NULL OR t.alarm_sent_at > i.alarm_sent_at)
	RETURNING
		t.kind,
		t.canonical_content_id AS post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		t.alarm_sent_at
), claimed_state_finalized AS (
	UPDATE youtube_community_shorts_alarm_states AS s
	SET authorized_at = NULL,
	    alarm_sent_at = i.alarm_sent_at,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND s.post_id = i.canonical_content_id
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.authorized_at = i.authorized_at
	  AND s.alarm_sent_at IS NULL
	RETURNING s.kind, s.post_id
), authorization_mismatches AS (
	SELECT s.kind, s.post_id
	FROM deduped_input AS i
	JOIN youtube_community_shorts_alarm_states AS s
	  ON s.kind = i.kind
	 AND s.post_id = i.canonical_content_id
	WHERE i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND i.authorized_at IS NOT NULL
	  AND s.alarm_sent_at IS NULL
	  AND NOT EXISTS (
		SELECT 1
		FROM claimed_state_finalized AS f
		WHERE f.kind = s.kind
		  AND f.post_id = s.post_id
	  )
), existing_state_updated AS (
	UPDATE youtube_community_shorts_alarm_states AS s
	SET authorized_at = NULL,
	    alarm_sent_at = CASE
	        WHEN s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at THEN i.alarm_sent_at
	        ELSE s.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    updated_at = $6
	FROM deduped_input AS i
	WHERE s.kind = i.kind
	  AND s.post_id = i.canonical_content_id
	  AND i.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND NOT EXISTS (
		SELECT 1
		FROM claimed_state_finalized AS f
		WHERE f.kind = s.kind
		  AND f.post_id = s.post_id
	  )
	  AND NOT EXISTS (
		SELECT 1
		FROM authorization_mismatches AS m
		WHERE m.kind = s.kind
		  AND m.post_id = s.post_id
	  )
	  AND (s.alarm_sent_at IS NULL OR s.alarm_sent_at > i.alarm_sent_at OR s.authorized_at IS NOT NULL)
	RETURNING s.kind, s.post_id
), missing_state_inserted AS (
	INSERT INTO youtube_community_shorts_alarm_states (
		kind,
		post_id,
		content_id,
		channel_id,
		actual_published_at,
		detected_at,
		authorized_at,
		alarm_sent_at,
		delivery_status,
		created_at,
		updated_at
	)
	SELECT
		t.kind,
		t.post_id,
		t.content_id,
		t.channel_id,
		t.actual_published_at,
		t.detected_at,
		NULL,
		t.alarm_sent_at,
		'SENT',
		$6,
		$6
	FROM tracking_updated AS t
	WHERE t.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	  AND NOT EXISTS (
		SELECT 1
		FROM youtube_community_shorts_alarm_states AS s
		WHERE s.kind = t.kind
		  AND s.post_id = t.post_id
	  )
	ON CONFLICT (kind, post_id) DO UPDATE
	SET alarm_sent_at = CASE
	        WHEN youtube_community_shorts_alarm_states.alarm_sent_at IS NULL
	          OR youtube_community_shorts_alarm_states.alarm_sent_at > EXCLUDED.alarm_sent_at
	        THEN EXCLUDED.alarm_sent_at
	        ELSE youtube_community_shorts_alarm_states.alarm_sent_at
	    END,
	    delivery_status = 'SENT',
	    authorized_at = NULL,
	    updated_at = $6
	RETURNING kind, post_id
)
SELECT
	(SELECT COUNT(*) FROM tracking_updated) AS tracking_updated_count,
	(SELECT COUNT(*) FROM claimed_state_finalized) AS claimed_state_finalized_count,
	(SELECT COUNT(*) FROM authorization_mismatches) AS authorization_mismatch_count,
	(SELECT COUNT(*) FROM existing_state_updated) AS existing_state_updated_count,
	(SELECT COUNT(*) FROM missing_state_inserted) AS missing_state_inserted_count
`

type bulkAlarmSentMarkInputs struct {
	kinds               []string
	contentIDs          []string
	canonicalContentIDs []string
	alarmSentAts        []time.Time
	authorizedAts       []pgtype.Timestamptz
}

func newBulkAlarmSentMarkInputs(marks []AlarmSentMark) (bulkAlarmSentMarkInputs, error) {
	inputs := bulkAlarmSentMarkInputs{
		kinds:               make([]string, 0, len(marks)),
		contentIDs:          make([]string, 0, len(marks)),
		canonicalContentIDs: make([]string, 0, len(marks)),
		alarmSentAts:        make([]time.Time, 0, len(marks)),
		authorizedAts:       make([]pgtype.Timestamptz, 0, len(marks)),
	}

	for i, mark := range marks {
		if err := appendBulkAlarmSentMarkInput(&inputs, i, mark); err != nil {
			return bulkAlarmSentMarkInputs{}, err
		}
	}

	return inputs, nil
}

func appendBulkAlarmSentMarkInput(inputs *bulkAlarmSentMarkInputs, index int, mark AlarmSentMark) error {
	if mark.AlarmSentAt.IsZero() {
		return fmt.Errorf("bulk mark alarm sent: alarm sent at is empty at index %d", index)
	}
	canonicalContentID := canonicalTrackingIdentity(mark.Kind, mark.ContentID)
	if strings.TrimSpace(canonicalContentID) == "" {
		return fmt.Errorf("bulk mark alarm sent: canonical content id is empty at index %d", index)
	}

	inputs.kinds = append(inputs.kinds, string(mark.Kind))
	inputs.contentIDs = append(inputs.contentIDs, mark.ContentID)
	inputs.canonicalContentIDs = append(inputs.canonicalContentIDs, canonicalContentID)
	inputs.alarmSentAts = append(inputs.alarmSentAts, mark.AlarmSentAt)
	inputs.authorizedAts = append(inputs.authorizedAts, alarmSentAuthorizedAtValue(mark.AuthorizedAt))

	return nil
}

func alarmSentAuthorizedAtValue(authorizedAt *time.Time) pgtype.Timestamptz {
	if authorizedAt == nil {
		return pgtype.Timestamptz{}
	}

	return pgtype.Timestamptz{Time: *authorizedAt, Valid: true}
}
