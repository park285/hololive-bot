-- timeline 조회 필터: $2는 대상 outbox id 배열이다.
o.id = ANY($2::bigint[])
