# YouTube Community Shorts Continuous Observation Report

`youtube-scraper` 빅뱅 배포 완료 직후부터 24시간 동안 같은 observation key로 운영 채널 전체 baseline, 채널 요약, 게시물 exact-once 상태, delivery audit 로그, 지연 요약/원인 보고서를 끊김 없이 파일로 누적할 때 사용하는 운영 절차입니다.

## Scope

- 대상 알람은 `COMMUNITY_POST`, `NEW_SHORT` 만 포함합니다.
- 시작 기준은 `youtube_community_shorts_observation_windows.deployment_completed_at` 이며, 같은 row의 `observation_started_at` 부터 `observation_ended_at` 까지를 24시간 관찰 창으로 사용합니다.
- observation query는 `runtime_name + bigbang_cutover_at` 한 쌍으로만 식별합니다.
- 관찰 창이 열려 있으면 리포트는 `[observation_started_at, min(now, observation_ended_at))` 누적 구간을 사용합니다.
- 관찰 창이 닫히면 같은 command가 finalized baseline 기준 최종 24시간 집합을 다시 수집해 마지막 snapshot을 남깁니다.
- 출력은 커뮤니티/쇼츠 전용 데이터만 사용하며 다른 알람 유형이나 UI는 변경하지 않습니다.

## Execute

one-shot snapshot:

```bash
go run ./hololive/hololive-stream-ingester/cmd/youtube-community-shorts-continuous-observation-report \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT>
```

continuous 24h snapshot loop:

```bash
go run ./hololive/hololive-stream-ingester/cmd/youtube-community-shorts-continuous-observation-report \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT> \
  -watch
```

JSON output with an explicit directory:

```bash
go run ./hololive/hololive-stream-ingester/cmd/youtube-community-shorts-continuous-observation-report \
  -observation-runtime youtube-scraper \
  -observation-cutover <CUTOVER_AT> \
  -watch \
  -format json \
  -output-dir artifacts/youtube-community-shorts-continuous-observation/manual-run
```

주요 옵션:

- `-watch`: 첫 snapshot을 즉시 생성한 뒤 관찰 창이 finalized 될 때까지 계속 수집합니다.
- `-output-dir`: snapshot 파일을 쓸 디렉터리입니다. `-watch` 에서 비워 두면 `artifacts/youtube-community-shorts-continuous-observation/<runtime>-<cutover>` 를 사용합니다.
- `-delivery-log-limit`: snapshot마다 포함할 delivery log row 최대 개수입니다.
- `-wait-timeout`: 빅뱅 배포 직후 observation window row가 아직 보이지 않을 때 얼마나 기다릴지 정합니다.

## Cadence

- 첫 snapshot은 command 시작 직후 즉시 생성합니다.
- 배포 후 첫 1시간 동안은 `5분` 간격으로 다시 수집합니다.
- 배포 후 1시간부터 관찰 창 종료 시점까지는 `15분` 간격으로 다시 수집합니다.
- 종료 시각 직전에는 남은 시간을 기준으로 sleep을 줄여 관찰 창 종료 시점에 final snapshot이 남도록 맞춥니다.
- 종료 이후 최초 재수집에서 observation window가 finalized 되면 마지막 snapshot을 남기고 종료합니다.

## Output Files

- `latest.md` 또는 `latest.json`: 가장 최근 snapshot입니다.
- `snapshot-YYYYMMDD-HHMMSS.<ext>`: 각 수집 시점의 immutable snapshot입니다.
- top-level metadata에는 `observation status`, `deployment completed at`, `observed until`, `target channels`, `app version` 이 함께 기록됩니다.
- top-level `24h closeout` 섹션에는 전체 운영 채널 합산 기준 `internal_system_cause_posts`, 전체 `over_2m_posts`, `excluded_external_collection_posts`, 그리고 `missing_alarm_posts` 와 누락 0건 여부가 함께 명시됩니다.

## Included Sections

- target baseline: 운영 채널 전체 대상 channel/route baseline
- channel summary: 현재까지 관찰된 채널별 감지/성공/미발송 요약
- send counts: 게시물별 exact-once, 누락, 중복, 내부/외부 지연 상태와 `actual_published_at`, `alarm_sent_at`, `delay_seconds` 상세
- alarm sent-history dataset: finalized snapshot에서만 포함되는 exact-once 대조 결과와 `missing_alarm_rows` 상세
- delivery logs: delivery audit success/failure/retry 로그
- latency period report: 최근 `15m`, `1h`, `24h` rolling 요약
- latency cause report: 같은 observation key 기준 2분 초과 상세 및 원인 집계

## Interpretation

- `observation status = active`: 관찰 창이 아직 열려 있으며 snapshot은 누적 관찰치입니다.
- `observation status = finalized`: 관찰 창이 닫혔고 finalized baseline 기준 최종 24시간 집합이 기록된 상태입니다.
- finalized snapshot에서는 `24h closeout` 이 `scope = all_operational_channels` 로 집계되고, SLA 합격선은 `internal over-2m closeout: status = pass` 와 `internal_system_cause_posts = 0` 입니다. `excluded_external_collection_posts` 는 관측용 기록이며 pass/fail 평가에서는 제외됩니다.
- finalized snapshot에서는 같은 섹션에 `missing alarm closeout: status = pass` 와 `missing_alarm_posts = 0` 이 함께 기록돼 운영 채널 전체 기준 누락 0건 여부를 파일만으로 닫을 수 있습니다.
- `observed until` 은 현재 snapshot이 실제로 포함한 observation end입니다. active 상태에서는 `min(now, observation_ended_at)` 이고, finalized 상태에서는 `observation_ended_at` 와 같습니다.
- `excluded_external_collection_posts` 는 `latency cause report` 에서 `delay_source = external_collection` 으로 분류된 2분 초과 게시물 수입니다. 이 값은 지연 기록용이며 closeout pass/fail을 뒤집지 않습니다.
- `target baseline` 은 게시물이 아직 없는 채널도 포함하므로 운영 채널 전체 대상 여부를 확인할 때 기준이 됩니다.

## Related Runbooks

- `YOUTUBE_COMMUNITY_SHORTS_POST_DEPLOY_24H_VERIFICATION.md`
- `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`
- `YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md`
- `YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md`
- `YOUTUBE_COMMUNITY_SHORTS_OPERATIONS_DASHBOARD.md`
