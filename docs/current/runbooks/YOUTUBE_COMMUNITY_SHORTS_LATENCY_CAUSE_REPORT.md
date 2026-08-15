# YouTube Community Shorts Latency Cause Report

최근 period 기반 조회로 실제 게시 시각 SLA 집계와 2분 초과 게시물 목록, 내부 원인 집계 결과를 함께 확인할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- recent query는 각 period의 `[period_start_at, period_end_at)` 범위를 사용합니다.
- 기간 판정 시작점은 `youtube_content_alarm_tracking.actual_published_at` 이고, 실제 게시 시각이 비어 있으면 `detected_at` 로 대체합니다.
- 2분 초과 여부는 저장된 `alarm_latency_exceeded = true` 기준입니다.
- 원인 분류는 저장된 `delay_source`, `internal_delay_cause`, `latency_classification_status` 와 텔레메트리 재구성값을 함께 사용합니다.
- 지연 집계와 원인 분류 모두 대상 범위를 community/shorts 게시물에만 한정합니다.

## Execute

standalone producer ops CLI는 Task 9에서 모듈과 함께 제거됐다. `youtube-collector`에는 `cmd/ops` 리포트 바이너리가 없다.

기간별 지연 집계와 2분 초과 후보는 `youtube_content_alarm_tracking`의 `alarm_latency_exceeded`, `delay_source`, `internal_delay_cause`, `latency_classification_status`와 `youtube_notification_delivery_telemetry`로 확인한다.

## Readout

- `mode = recent_window`: rolling period 조회입니다. top-level `window` 는 가장 이른 period 시작 시각부터 마지막 period 종료 시각까지의 fetch 범위입니다.
- `latency summary`: 기간별 전체 게시물 수, 발송 완료 수, 대기 수, 평균/p95/최대 지연, 2분 초과 건수를 보여줍니다.
- top-level `verification.internal_cause_rule`, `verification.non_internal_cause_rule`, `verification.excluded_external_rule`, `verification.insufficient_evidence_rule` 는 내부/비내부 판정과 외부 시스템 지연 제외 규칙에 실제로 사용한 문자열을 그대로 노출합니다.
- top-level `verification.evidence_field_catalog` 는 row 판정에 사용할 수 있는 안정된 근거 필드 목록입니다. 현재 목록은 `delay_source`, `internal_delay_cause`, `alarm_latency_millis`, `publish_to_detect_millis`, `internal_latency_millis`, `queue_wait_millis`, `retry_accumulation_millis`, `job_failure_detected`, `latency_classification.status`, `latency_classification.evidence` 입니다.
- `cause summary`: 2분 초과 건을 먼저 `internal_system_cause_posts` 와 `non_internal_system_cause_posts` 로 나눕니다. `delay_source = internal_delivery|mixed` 이거나 `internal_delay_cause != none` 이면 내부 원인으로 판정하고, 그 외 초과 건은 비내부 원인으로 집계합니다. 같은 요약에 `excluded_external_delay_posts` 를 별도로 노출해 `delay_source = external_collection` 건수를 실패 집계와 분리된 참고 정보로 바로 읽을 수 있게 합니다.
- 같은 `cause summary` 에서 `external_collection`, `internal_delivery`, `mixed`, `no_dominant_source` source bucket 과 `queue_wait`, `retry_accumulation`, `job_failure`, `unclassified_internal_cause` 내부 세부 원인 bucket 을 함께 봅니다. `external_collection_source_posts` 는 source bucket 유지용이고, `excluded_external_delay_posts` 와 같은 게시물 수를 가리키지만 의미는 운영 참고용 제외 건수입니다.
- `insufficient_evidence_posts`: 2분 초과는 확인됐지만 저장된 원인 분류 근거가 부족한 건수입니다. 내부 원인 근거가 없으면 `non_internal_system_cause_posts` 에 포함됩니다.
- detail table의 `observed_at` 은 기간 경계 판정에 사용된 시각입니다.
- detail table의 `internal_cause_judgment` 와 `internal_cause_basis` 는 각 초과 게시물이 왜 내부/비내부로 판정됐는지 바로 추적할 수 있게 남깁니다.
- detail table의 `cause_evidence_fields` 와 JSON row의 `cause_evidence.fields` 는 해당 게시물 판정에 실제로 참조한 필드 이름만 남깁니다. `cause_evidence.selected_classification_keys` 및 `cause_classification_evidence` 의 `[selected]` 표시는 저장된 classification evidence 중 어떤 항목이 선택 근거였는지 보여줍니다.
- detail table의 `cause_classification_status` 와 `cause_classification_evidence` 는 저장된 latency classification 근거를 그대로 보여줍니다.
