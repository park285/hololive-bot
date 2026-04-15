# Critical Review Residual Issues (2026-04-15)

상태: **CLOSED (2026-04-15)**

이 문서는 2026-04-15 기준 non-blocking residual follow-up 메모였고,
같은 날짜의 후속 작업으로 남아 있던 `R1 ~ R5` 를 모두 닫은 뒤의 종료 기록이다.

중요한 정리:

- 원본 critical review 의 직접 수정 항목 `#1 ~ #10` 은 이미 닫혀 있었다.
- 이 문서에서 남아 있던 것은 즉시 장애 항목이 아니라 후속 guardrail / observability / contract 보강 항목이었다.
- 2026-04-15 후속 패치로 이 잔여 항목도 종료됐다.

---

## 1. 종료된 잔여 항목

### R1. Holodex official schedule fallback observability

종료 내용:

- official schedule fallback 결과를 `outcome/reason` 단위로 기록하는 전용 metric 을 추가했다.
- `FetchChannel` fallback 경로에서 `structure_drift`, `parse`, `network`, `empty` 를 구분해 기록한다.
- direct `FetchAllStreams` / `ValidateStructure` 계열 소비 경로도 `official_schedule_page` operation 으로 별도 관측한다.
- reason 분류는 문자열 fallback 에만 기대지 않도록 typed wrapper 기반으로 보강했다.
- 대표 fallback 실패/빈 결과 경로를 TDD 회귀 테스트로 고정했다.

주요 범위:

- `hololive/hololive-shared/pkg/service/holodex/official_schedule_metrics.go`
- `hololive/hololive-shared/pkg/service/holodex/scraper.go`
- `hololive/hololive-shared/pkg/service/holodex/scraper_test.go`

종료 판단:

- parse failure 와 upstream structure drift 가 운영 metric 으로 분리 관측된다.
- `FetchChannel` 뿐 아니라 direct official schedule fetch 경로도 같은 metric 체계로 관측된다.
- 대표 fallback 분기 테스트가 추가됐다.

### R2. `internal/ops` markdown helper convergence

종료 내용:

- heading promotion 로직을 공용 markdown helper 로 이동했다.
- `community_shorts_alarm_sent_history_dataset_render.go` 를 helper 기반 규약으로 수렴시켰다.
- `community_shorts_continuous_observation_render.go` 도 공용 helper 스타일로 정리했다.
- 빈 dataset/scaffold 와 embedded section promotion 계약을 테스트로 고정했다.
- dataset / continuous observation markdown 전체를 golden snapshot 으로 고정해 whitespace / blank-line drift 도 잡는다.

주요 범위:

- `hololive/hololive-stream-ingester/internal/ops/report_markdown.go`
- `hololive/hololive-stream-ingester/internal/ops/report_markdown_test.go`
- `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_render.go`
- `hololive/hololive-stream-ingester/internal/ops/community_shorts_alarm_sent_history_dataset_test.go`
- `hololive/hololive-stream-ingester/internal/ops/community_shorts_continuous_observation_render.go`
- `hololive/hololive-stream-ingester/internal/ops/community_shorts_continuous_observation_report_test.go`

종료 판단:

- 주요 renderer 가 공용 helper 규약으로 수렴했다.
- 빈 섹션/헤딩 승격 contract 가 테스트로 보호된다.
- representative markdown output 이 golden 으로 고정돼 포맷 drift 가 즉시 드러난다.

### R3. summarizer prompt byte-for-byte golden freeze

종료 내용:

- weekly/monthly rendered system prompt 에 대해 byte-for-byte golden snapshot 을 추가했다.
- 기존 critical-section snapshot 검증 위에 전체 prompt freeze 검증을 겹쳐서 보호한다.
- mocked LLM JSON 을 이용한 weekly/monthly summary output golden 도 추가해 prompt 이후 텍스트 조립 결과까지 고정했다.
- golden mismatch 메시지에 `UPDATE_GOLDEN=1` 갱신 절차를 직접 안내한다.

주요 범위:

- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt_golden_test.go`
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/testdata/weekly_system_prompt.golden.txt`
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/testdata/monthly_system_prompt.golden.txt`

종료 판단:

- rendered prompt 전체가 golden 파일과 일치해야 하므로 prompt drift 가 즉시 드러난다.
- mocked summarize output golden 으로 downstream text assembly drift 도 즉시 드러난다.

### R4. runtime / tool / ops entrypoint contract drift guard

종료 내용:

- repository root 에서 모든 주요 `cmd/*/main.go` 진입점이 현재 owning helper / owning seam 에 계속 anchored 되어 있는지 검사하는 contract test 를 추가했다.
- runtime entrypoint 는 `bootstrap.Run(...)`, report CLI 는 `reportcli.Run...`, 개별 tool/ops entrypoint 는 자신이 속한 수집/초기화 seam 에 anchored 되어 있는지 검증한다.
- manifest 가 현재 repo 의 모든 `hololive/**/cmd/**/main.go` 를 빠짐없이 커버하는지 별도 테스트로 검증한다.
- 단순 substring 검색이 아니라 Go AST 기준 call-path 검사로 guard 를 강화했다.

주요 범위:

- `entrypoint_contract_test.go`
- `testdata/entrypoint_contracts.json`
- `doc.go`

종료 판단:

- runtime main 만 helper 로 공통화되어 있던 상태에서 남아 있던 “entrypoint policy drift” 우려를 stronger static contract test 로 닫았다.
- 새 command 진입점이 추가됐는데 manifest 가 빠지는 누락 리스크도 함께 닫혔다.

### R5. alarm cache rebuild observability

종료 내용:

- `hololive_alarm_cache_rebuild_total{operation,result}` metric 을 추가했다.
- `add/remove/clear` cache mutation fallback rebuild 와 `warm` 경로를 운영 metric 으로 관측한다.
- `hololive_alarm_cache_rebuild_duration_seconds{operation,result}` histogram 을 추가했다.
- `hololive_alarm_cache_rebuild_loaded{operation,resource}` gauge 로 성공한 rebuild 의 alarms / rooms / channels loaded summary 를 노출한다.
- rebuild 성공/실패 경로를 TDD 테스트로 고정했다.
- repo 안에는 Prometheus / Alertmanager / Grafana provisioning surface 가 없음을 재확인했고, 실제 alert wiring 은 external-only 로 정리했다.

주요 범위:

- `hololive/hololive-kakao-bot-go/internal/service/notification/metrics.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_persistence.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service_durability_test.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service_crud_additional_test.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/metrics_test.go`

종료 판단:

- “DB 성공 + cache rebuild 실패” 경로가 로그 외에 metric 으로 식별 가능해졌다.
- rebuild 소요 시간과 복구 규모도 metric 으로 함께 확인할 수 있다.
- monitoring stack wiring 은 repo 소유 범위 밖이라는 점이 문서 기준으로 명확히 정리됐다.

---

## 2. 검증 증거

2026-04-15에 아래 검증을 다시 실행했다.

테스트:

- `go test . -run TestCommandEntrypointsStayAnchoredToOwningHelpers -count=1`
- `go test . -run TestEntrypointContractManifestCoversAllCommandMainFiles -count=1`
- `go test ./internal/service/majorevent/summarizer -count=1`
- `go test ./pkg/service/holodex -count=1`
- `go test ./internal/service/notification -count=1`
- `go test ./internal/ops -count=1`
- `go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... -count=1`

빌드:

- `go build ./internal/service/majorevent/summarizer`
- `go build ./pkg/service/holodex`
- `go build ./internal/service/notification`
- `go build ./internal/ops ./cmd/ops/...`
- `go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...`

lint / formatting:

- `golangci-lint run ./pkg/service/holodex`
- `golangci-lint run ./internal/service/notification/...`
- `golangci-lint run ./internal/ops/...`

추가로 각 R1 / R2 / R3 / R5 는 RED → GREEN targeted test 를 먼저 거친 뒤 같은 패키지 전체 테스트로 회귀 확인했다.

---

## 3. 결론

2026-04-15 기준으로 이 문서에 남아 있던 residual follow-up `R1 ~ R5` 는 모두 종료됐다.

따라서 이 문서는 더 이상 open follow-up queue 가 아니라,
**2026-04-15 critical review 후속 정리가 완전히 마감되었음을 기록하는 종료 문서**다.
