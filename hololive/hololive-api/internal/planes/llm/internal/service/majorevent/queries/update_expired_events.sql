		UPDATE major_events
		SET status = $1,
			updated_at = NOW()
		WHERE status = $2
		  AND COALESCE(event_end_date, event_start_date, (pub_date AT TIME ZONE 'Asia/Seoul')::date)
		      < (statement_timestamp() AT TIME ZONE 'Asia/Seoul')::date
