-- 같은 트랜잭션의 앞 문장(delivery_repository_aggregate_lock.sql)이 부모를 잠근 뒤 실행되므로 clock은 부모 잠금
-- 이후의 DB 시계다. 새로 기록하는 sent_at·terminal_at은 이 clock 하나로 정한다.
-- $1 outbox id 배열, $2 PENDING, $3 SENDING, $4 FAILED, $5 QUARANTINED, $6 SENT, $7 실패 사유.
WITH clock AS MATERIALIZED (
    SELECT clock_timestamp() AS at
)
UPDATE youtube_notification_outbox o
		SET status = agg.next_status,
		    locked_at = NULL,
		    sent_at = CASE WHEN agg.next_status = $6::text AND o.sent_at IS NULL THEN (SELECT at FROM clock) ELSE o.sent_at END,
		    terminal_at = CASE
		        WHEN agg.next_status NOT IN ($4::text, $6::text) THEN NULL
		        WHEN o.status IS DISTINCT FROM agg.next_status OR o.terminal_at IS NULL THEN (SELECT at FROM clock)
		        ELSE o.terminal_at
		    END,
		    error = CASE WHEN agg.next_status = $4::text THEN $7::text ELSE '' END
		FROM (
		    SELECT ids.id,
		        CASE
		            WHEN COUNT(d.status) FILTER (WHERE d.status IN ($2::text, $3::text)) > 0 THEN $2::text
		            WHEN COUNT(d.status) FILTER (WHERE d.status IN ($4::text, $5::text)) > 0 THEN $4::text
		            WHEN COUNT(d.status) FILTER (WHERE d.status = $6::text) > 0 THEN $6::text
		            ELSE $2::text
		        END AS next_status
		    FROM unnest($1::bigint[]) AS ids(id)
		    LEFT JOIN youtube_notification_delivery d ON d.outbox_id = ids.id
		    GROUP BY ids.id
		) agg
		WHERE o.id = agg.id
		  AND (o.status IS DISTINCT FROM agg.next_status
		       OR (agg.next_status = $6::text AND o.sent_at IS NULL)
		       OR (agg.next_status IN ($4::text, $6::text) AND o.terminal_at IS NULL))
