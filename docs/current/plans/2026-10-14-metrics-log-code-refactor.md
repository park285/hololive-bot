# 메트릭·로그·코드 분석 기반 리팩토링 (2026-10-14)

운영 Prometheus(kapu `obs-prometheus`, 최근 7일)와 `logs/remote/*`(2026-10-14 08:54 동기화), 코드 정적 분석(golangci-lint 2.14 기준 0건, dupl 120 토큰, deadcode)을 함께 읽고 찾은 병목·품질·컨벤션·재활용 문제를 정리하고, 코드만으로 끝낼 수 있는 항목을 이 branch(`refactor/metrics-log-code-20261014`, worktree `/home/kapu/work/refactor-20261014/hololive-bot`)에서 고친다. 측정과 분석은 `main` `7f2fe0cbf`(v9.3.1)에서 했고, 변경은 `main` `9feb5b267`(v9.3.2) 위에 커밋한다. v9.3.2(#635)는 이 변경과 겹치는 파일이 없다.

이전 계획([DB hot path](2026-10-05-db-hotpath-optimization.md), [DB 리뷰 후속](2026-10-08-db-review-followup.md), [SQL 최적화·호환 fadeout](2026-10-12-sql-optimization-and-compat-fadeout.md))이 끝낸 항목은 다시 다루지 않는다. 새 재시도·대체 경로를 만들지 않고, 재등장 방지 grep 가드나 이름 검사 테스트도 추가하지 않는다.

## 측정 결과

### 자원 (Prometheus, 7일)

| 런타임 | CPU 평균(1h) | CPU 최대(5m) | RSS 최대 | goroutine 최대 |
| --- | ---: | ---: | ---: | ---: |
| hololive-api (30091) | 0.018 core | 0.039 core | 308 MiB | 245 |
| alarm-worker (30097) | 0.004 core | 0.005 core | 67 MiB | 114 |
| collector c (30096) | 0.006 core | 0.008 core | 49 MiB | 46 |
| collector a/b/d | 0.007~0.008 core | 0.023 core | 50 MiB | 50 |

프로세스 자원은 병목이 아니다. hololive DB는 commit 85/s, 활성 시간 0.09 s/s, 캐시 적중 99%, deadlock 0이다. 병목은 용량이 아니라 "쓸데없는 왕복"이다.

### DB 왕복 낭비 (Prometheus, 7일)

| 항목 | 측정값 | 근거 |
| --- | ---: | --- |
| bot durable 유휴 claim | inbox 479/min + outbox 479/min ≈ 16/s | `bot_durable_claim_idle_total`. `bot_webhook_inbox`·`bot_reply_outbox`·`bot_webhook_heads` seq scan 각 8~10/s와 일치. DB commit 85/s의 약 19% |
| collector lease 빈 획득 | acquired 1.40M, not_acquired 0.70M (33%) | `youtube_collection_lease_acquire_total`. 빈 획득도 guard 잠금·target 검증·INSERT ON CONFLICT·UPDATE를 모두 실행 |
| `youtube_collection_job_leases` | idx_scan 202/s, seq_scan 16/s | 4대의 1초 주기 후보 탐색 |
| `observation_contract_generations` | seq_scan 13/s | collector publish마다 계약 세대 조회 |

### 큐·지연 (Prometheus, 7일)

- API YouTube plane: consume 1.54M 성공, retry 0, retention 오류 0. pending 큐 평균 2~5, p99 7~32, 최대 67. `work_queue_utilization_ratio`는 최대 1(consumer worker 1개), 가장 오래된 관측 최대 72초.
- alarm-worker: backlog 0, 빈 poll 12/min, Holodex live 상태 조회 실패 13건/주(외부 `nil response`·backoff 취소).
- 발행 p95: holodex 0.41 s, youtubejs 0.24 s, official 0.22 s.

### 로그 (2026-08-16 ~ 10-14)

| 런타임 | 핵심 신호 |
| --- | --- |
| collector a/b/d | "YouTube collection job failed" 각 6.2k~6.9k건. 분해: `parser_drift`(helper "upcoming live session has no machine-readable schedule") 2.0k~2.7k건(9/9~9/11 집중, 이후 산발), `candidate_load_failed`(DB dial·deadline, 9/24·9/30·10/8 장애일 집중) 2.4k~2.6k건, **`collection_internal_invariant` 335~445건 중 대부분이 Holodex/official `http2: timeout awaiting response headers`**(수송 계층 타임아웃을 INTERNAL 불변식 위반으로 기록), YouTube 429로 helper가 죽으면 런타임이 종료·재시작(19~21회) |
| hololive-api | 재시작 18회 중 17회가 배포. 10-08 19:29 `acquire DB slot: context deadline exceeded`(retention·consume·retry 동시, migration 271~278 적용 시점). WARN은 otel export timeout 16건뿐 |
| alarm-worker | ERROR 13건 모두 Holodex 외부 실패. 오류 문구가 `nil response: Get ...`로 시작해 원인을 가린다 |

### 코드 (정적 분석)

- golangci-lint(저장소 설정, 2.14.0) 0건. deadcode: 운영 경로 도달 불가 104개 중 테스트 helper 패키지 제외 34개는 테스트에서만 쓰는 운영 함수(전부 `-test` 포함 시 도달), 완전 미사용은 `subscription/repository_test.go`의 mock 메서드 2개뿐.
- dupl(120 토큰) 11쌍: retention 설정 loader 4개 함수, `content_consumer.go` video/shorts evidence, `repository_celebration.go`/`repository_scan.go`의 동일 scan, tracking/observation의 batch normalize 2벌, cache `SAdd`/`SRem`, alarm 명령 add/remove, majorevent weekly/monthly 실행, canonicalwrite short/community published_at 검증, 그리고 spec 리터럴만 다른 3쌍(member field update, channel photo/profile consumer).
- 컨벤션: `fmt.Errorf("failed to …")` 92곳(37파일)이 `action: context: %w` 규칙과 다르다. slog 메시지는 문장형 413 / 소문자 구 235 / shared-go의 snake_case가 섞여 있다(문서화된 규칙 없음). 메트릭 `service` 라벨이 `hololive-kakao-bot-go`로 남아 있다.
- 재활용 미흡: `hololive-shared/pkg/util`이 breaker·markdown·valkey·instance·string을 한 패키지에 모은 grab-bag이고, `IsValkeyNil`은 `errors.Is(err, valkey.Nil)`로 대체된다. alarm-worker `internal/service/envconfig`는 shared `envload.RequiredPositiveIntEnv`·`RequiredMillisDurationEnv`와 같은 일을 한다. youtubejs `RateLimiter`는 `x/time/rate`와 같지만 의존성 승인이 필요해 유지한다. 재시도 구현 28곳 중 shared-go `retry`를 쓰는 곳은 11곳이며, Holodex apiclient의 자체 루프는 cooldown·회로 차단과 얽혀 있어 이번 범위에서 제외한다.

## 범위 (코드만으로 진행)

| ID | 대상 | 조치 | 근거 |
| --- | --- | --- | --- |
| C1 | collector 오류 분류 | `*url.Error`의 `Timeout()`(response header·dial·client timeout)을 `collection_timeout/TIMEOUT`으로 분류. context deadline·socket deadline 규칙은 유지 | 로그 1,200여 건이 INTERNAL로 기록되어 진단을 가린다. 재시도 동작은 같다(미분류도 비치명) |
| P1 | bot durable 유휴 폴링 | 큐별로 유휴 worker 하나만 polling timer를 돌리고 나머지는 wake만 기다린다. claim 성공 시 wake를 한 번 더 보내 burst는 연쇄로 깨운다. inbox·outbox 루프를 하나로 합친다 | 유휴 claim 16/s → 큐당 약 0.5/s. 생산자가 모두 같은 프로세스라 wake가 1차 신호이고 polling은 안전망이다 |
| V1 | `fmt.Errorf("failed to …")` 92곳 | `"<action>: %w"` 형태로 바꾸고 문구를 단정하는 테스트 2곳을 맞춘다 | 저장소 오류 문구 규칙 |
| V2 | `nil response:` 접두 | Holodex apiclient·alarm client에서 제거(net/http 오류는 항상 nil response) | 로그 가독성 |
| R1 | `pkg/util` 해체 | `IsValkeyNil` → `errors.Is`, `Breaker` → holodex apiclient, `InstanceID` → alarm-worker `workerruntime`, `NormalizeSuffix` → bot matcher, `KakaoZeroWidthSpace`·`MarkdownNeutralize` → `pkg/kakaotext` | 의미 있는 패키지 이름 규칙(generic bucket 금지) |
| R2 | alarm-worker `envconfig` | `envload.RequiredPositiveIntEnv`·`RequiredMillisDurationEnv`로 교체, `ParseHourOfDay`는 호출자 패키지로 이동 | 공유 facility 재활용. 공백만 있는 값은 기본값 대신 오류가 된다(strict 계약) |
| D1~D8 | dupl 11쌍 중 8쌍 | 표 기반 loader, 공통 helper, generic normalize로 합친다 | 동작 변화 없음 |

범위 밖(후속 또는 승인 필요): lease 빈 획득 비용 축소(UPDATE 우선 획득·후보 행 존재 힌트)는 collector 계약·traffic 시험 개정과 AP 4대 배포가 필요해 [DB 리뷰 후속](2026-10-08-db-review-followup.md)대로 다음 collector 릴리스에서 다룬다. `observation_contract_generations` 조회 캐시는 세대 추가 migration과 collector 재시작 순서 계약이 필요하다. YouTube 429로 helper 종료 시 런타임 종료는 systemd 재시작에 기대는 현행 계약이다. 메트릭 `service` 라벨·slog 메시지 규칙은 대시보드·알림 계약 변경이라 승인이 필요하다. deadcode의 테스트 전용 운영 함수 34개는 기존 부채로 보고만 한다.

## 검증

- 변경 패키지 `go test -race`와 golangci-lint(저장소 설정), `./build-all.sh --build-only --no-bump`.
- P1은 helper 단위 시험(poller 선출, wake 연쇄, 취소)과 기존 durable 시험으로 확인한다. 운영 반영 뒤 `bot_durable_claim_idle_total` 비율이 분당 약 30회로 내려가는지 본다.
- C1은 real client로 response header timeout을 재현하는 시험과 collecterr 단위 시험으로 확인한다.

## 진행 기록

### 2026-10-14 적용 (branch `refactor/metrics-log-code-20261014`)

| ID | 결과 |
| --- | --- |
| C1 | `collecterr.Normalize`·`FromContext`가 `*url.Error`의 `Timeout()`을 `collection_timeout/TIMEOUT`으로 분류한다. bare `os.ErrDeadlineExceeded`와 timeout이 아닌 `url.Error`는 미분류 유지. `providerhttp` real client(httptest 서버 header 지연 + `ResponseHeaderTimeout` 50ms) 시험 추가 |
| P1 | `durable_worker_loop.go`의 `runDurableWorker`가 inbox·outbox 루프를 대신한다. 큐별 `pollSlot`(cap 1)을 쥔 유휴 worker만 timer polling, 나머지는 wake 대기. claim 성공 시 `notifyDurable(wake)`로 burst 연쇄. synctest 시험 3개 |
| V1 | `fmt.Errorf("failed to …")` 92곳 → `"<action>: %w"`. 문구를 단정하던 테스트 10파일을 새 문구로 맞춤 |
| V2 | Holodex apiclient·alarm client의 `nil response:` 접두 제거 |
| R1 | `pkg/util` 삭제. `kakaotext.ZeroWidthSpace`·`kakaotext.MarkdownNeutralize`(신규 `pkg/kakaotext`), holodex apiclient 내부 `circuitBreaker`, alarm-worker `internal/leaseowner.InstanceID`, bot matcher `NormalizeSuffix`, `errors.Is(err, valkey.Nil)`. `scripts/deploy/ap-rsync-files.txt`의 util 6줄 제거(collector는 후속 패키지를 쓰지 않아 추가 항목 없음, manifest 검사 PASS) |
| R2 | `internal/service/envconfig` 삭제. `envload.RequiredPositiveIntEnv`·`RequiredMillisDurationEnv`로 교체, `parseHourOfDayEnv`는 workerapp 내부로 이동. 비어 있거나 공백뿐인 값은 기본값 대신 설정 오류가 된다(strict env 계약, 테스트에 명시) |
| D1 | retention loader 4개 → `youtubePlaneRetentionAgeEnvs` 표 하나. production 검증도 같은 표를 읽는다 |
| D2 | `content_consumer.go` video/shorts evidence → `listEvidence` |
| D3 | `scanCelebrationMemberRow` 삭제, `scanMemberFullRow` 재사용 |
| D4 | tracking/observation batch normalize 2벌 → generic `normalizeBatchRecords` |
| D5 | cache `SAdd`/`SRem` → `setMembersMutation` |
| D6 | alarm 명령 `handleAdd`/`handleRemove` → `handleAlarmMutation` + `alarmMutation` spec |
| D7 | majorevent weekly/monthly 실행 → `executePeriodNotification` + `periodNotification` spec |
| D8 | canonicalwrite short/community published_at 검증 → `validatePublishedAtPayload` |

검증(마지막 수정 이후, 모두 worktree에서):

- golangci-lint 2.14.0(저장소 설정) 4개 모듈 전체 0건.
- `go test -race` 변경 패키지(shared cache·member·template·holodex·alarm·tracking, api bot plane 전체·config·templateview·canonicalwrite·majorevent·acl·admin api·youtube plane, worker leaseowner·alarmdispatch·notificationdelivery·dispatchoutbox·workerapp·youtubedispatch, collector collecterr·providerhttp·holodexcollector·officialcollector·collectorruntime) 통과. `go test ./...` hololive-shared·alarm-worker·collector 통과(collector youtubejs는 worktree에 `npm ci` 후 통과). hololive-api는 `internal/youtube/...`·`internal/planes/llm/...`·`internal/httpapi/...`·`internal/apifoundation/...`·`internal/app/...` 추가 통과.
- 기준을 v9.3.2로 옮긴 뒤 `scripts/ci/local-ci.sh`를 다시 실행해 전 단계(architecture gate, tidy·vet·staticcheck·golangci-lint·NilAway, Go·collector 빌드, 운영 workspace·AP manifest·PG capacity·YouTube plane 예산 gate, race·non-race 테스트)가 통과했다. integration 태그 테스트는 기본 설정대로 건너뛰었다.
- `./build-all.sh --build-only --no-bump`(v9.3.1 기준): local CI gate(NilAway, Go build, collector 운영 빌드, X Spaces helper, 운영 workspace·AP manifest·PG capacity·YouTube plane 예산 gate, race 테스트, non-race 테스트) 통과. 이어지는 이미지 빌드는 제가 손대지 않은 `deploy/images/deunhealth`(main과 동일, #613 이후 로컬 미빌드)가 `cmd/app/main.go:155: unknown field BuildDate in struct literal of type gosplash.Settings`로 실패해 bake 전체가 취소됐다. 변경 서비스는 `./build-all.sh --build-only --no-bump --skip-local-ci hololive-api alarm-worker youtube-collector`로 hololive-api·alarm-worker·youtube-collector·po-sandbox 이미지 빌드 성공. deunhealth 실패는 기존 결함으로 별도 처리한다.

worktree 설정 기록: `go.work`의 `../shared-go`·`../iris-client-go`는 symlink이면 `scripts/ci/go-workspace-modules.sh`의 경계 검사(`resolve()`)에 걸리므로 같은 HEAD(shared-go `bfd5eeb`, iris-client-go `b9f576d`)의 detached git worktree로 두었다.

범위 밖으로 남긴 항목은 위 "범위" 절과 같다. 운영 효과(`bot_durable_claim_idle_total` 감소, `collection_internal_invariant` 로그 감소)는 배포 뒤 Prometheus·로그로 확인한다.
