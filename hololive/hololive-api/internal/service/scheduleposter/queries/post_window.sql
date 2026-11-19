d.posted_at > p.posted_at AND d.posted_at >= (p.week_start::timestamp AT TIME ZONE 'Asia/Tokyo')
    AND d.posted_at < ((p.week_end + 1)::timestamp AT TIME ZONE 'Asia/Tokyo')
