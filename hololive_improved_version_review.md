# hololive improved version review (2026-04-09)

## 총평

이 버전은 **이전 상태보다 확실히 좋아졌습니다.**  
특히 다음 세 가지는 실제 개선으로 인정할 수 있습니다.

1. **운영 정확성**
   - YouTube 알람이 `exact minute` 판정에서 `crossed window` 판정으로 바뀌었습니다.
   - 런타임 알람 루프가 completion-based reset에서 wall-clock aligned reset으로 바뀌었습니다.

2. **구조 정리**
   - `ProvideYouTubeService` 제거
   - `outboxConfigFromEnv` 제거
   - stream-ingester의 `runtimeName == youtube-scraper` 스타일 hidden branching 제거 후 `ingestionRuntimeSpec` 도입
   - `schedulerkit` 도입
   - bot 조립부에 module struct(`botCoreModule`, `botDataModule`, `botStreamModule` 등) 도입

3. **repo-wide 방향성**
   - `go.work`가 in-repo `./shared-go`를 사용하도록 바뀌었습니다.
   - `docs/architecture/release-governance-assets.txt`와 `docs/architecture/shared-go-package-allowlist.txt`가 채워졌습니다.
   - admin-dashboard에 `src/features/*`와 generated API client가 들어왔습니다.

다만 이 버전은 아직 **“완성형 리팩토링”이라기보다 1차 구조 안정화**에 가깝습니다.  
핵심 운영 이슈 일부는 해결됐지만, 저장소 전체 기준으로 보면 **반쯤 정리된 상태**입니다.

---

## 잘된 점

### 1) YouTube 알람 드리프트 대응이 실제로 들어감

- `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go:55`  
  `CrossedTarget(...)` 추가
- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go:222`  
  `minutesUntil` exact match 대신 `CrossedTarget(...)` 사용
- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go:215`  
  `nextAligned(...)` 도입

이건 실질 개선입니다.  
기존의 “5분 02초에 돌면 5분 알람을 놓치는” 문제를 상당 부분 해소합니다.

### 2) scraper poll interval이 이제 config에서 읽힘

- `hololive/hololive-shared/pkg/config/config.go:223`
- `hololive/hololive-shared/pkg/config/config_types.go:90`
- `hololive/hololive-stream-ingester/internal/app/stream_ingester_poller_registrations.go:42`

이전에는 stream-ingester/youtube-scraper가 poller interval env를 사실상 무시했는데, 이제 `ScraperConfig.Poll` → `PollOrDefault()` → poller registration으로 연결됩니다.

### 3) hidden runtime branching이 명시적 spec으로 바뀜

- `hololive/hololive-stream-ingester/internal/app/bootstrap_stream_ingester.go:49`

`ingestionRuntimeSpec`를 도입해서 `youtube-scraper`가 어떤 feature override를 가지는지 코드에서 바로 보입니다.  
이건 유지보수 관점에서 꽤 좋은 변경입니다.

### 4) reflection nil workaround 제거

- `hololive/hololive-stream-ingester/internal/app/runtime_helpers.go:30`

타입 우회 reflection helper를 제거하고 concrete dependency 기반으로 정리했습니다.

### 5) schedulerkit 도입

- `hololive/hololive-llm-sched/internal/schedulerkit/runtime.go:27`

major event / member news scheduler 공통 lifecycle을 묶은 것은 방향이 맞습니다.  
중복된 waiting / stop / manual stop / clock injection 패턴을 줄였습니다.

### 6) bot composition이 module 기반으로 가기 시작함

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_types.go:81`

`botCoreModule`, `botMessagingModule`, `botDataModule`, `botStreamModule` 등은  
DI 라이브러리 없이도 composition root를 정리하는 좋은 방향입니다.

### 7) repo self-contained 방향으로 일부 전진

- `go.work:4` → `./shared-go`

workspace 기준은 맞아졌습니다.

---

## 아직 남아있는 핵심 문제

### P0-1. 알람 advance minute 동적 변경이 runtime checker/dedup에 재주입되지 않음

문제 지점:
- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go:106-115`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go:87`

현재 `targetMinutes`는 scheduler 생성 시점에만 계산되어
- `dedup.NewService(cacheSvc, targetMinutes, logger)`
- `checker.NewYouTubeChecker(..., targetMinutes, ...)`

로 들어갑니다.

하지만 이후 `AlarmService.UpdateAlarmAdvanceMinutes(...)`가 호출되어도
runtime scheduler 쪽에서 이를 다시 `YouTubeChecker`와 `dedup.Service`에 재주입하는 경로가 없습니다.

즉:
- **초기 부팅 시점에는 맞음**
- **운영 중 설정 변경 반영은 아직 불완전**

이건 기능 버그입니다.

### P0-2. target minute 정책이 아직 3군데에 중복됨

- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/common.go:187`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go:279`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go:101`

핵심 규칙(0 이하 제거, 중복 제거, 내림차순 정렬, fallback 1분 보장)이 여전히 세 군데입니다.  
이번 버전에서 `CrossedTarget`은 shared로 옮겼지만 normalization 정책은 아직 shared single source가 아닙니다.

이건 전형적인 “절반만 정리된 상태”입니다.

### P0-3. scraper scheduler backlog 시 +10초 지연 로직이 그대로 남아 있음

- `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go:291`

```go
job.NextRunAt = now.Add(10 * time.Second)
```

worker channel이 꽉 차면 anchor를 유지하지 않고 임의 10초 지연을 넣습니다.  
이건 “백프레셔를 처리하는 코드”가 아니라 “스케줄을 깨뜨리는 코드”에 가깝습니다.

현재 `rescheduleJob()`도 `time.Now()` 기반으로 다시 다음 슬롯을 계산합니다.
즉 이전보다 좋아졌지만, backlog 상황에서는 여전히 예정 슬롯 의미를 잃을 수 있습니다.

### P0-4. poll interval config는 생겼지만 운영 compose에 노출되지 않음

- `hololive/hololive-shared/pkg/config/config.go:227` 에서는
  `SCRAPER_VIDEOS_SECONDS`, `SCRAPER_SHORTS_SECONDS`, `SCRAPER_COMMUNITY_SECONDS`, `SCRAPER_STATS_SECONDS`, `SCRAPER_LIVE_SECONDS`
  를 읽습니다.
- 그런데 `docker-compose.prod.yml`의 `youtube-scraper` 블록(362행 이후)에는 이 env가 없습니다.

즉 코드 수준에서는 연결됐지만,
운영자가 compose만 보고는 이 설정 존재를 알기 어렵고 기본값 제어도 바로 안 됩니다.

### P0-5. worker count는 아직 고정 2개

- `hololive/hololive-shared/pkg/providers/youtube_providers.go:86`

`ProvideScraperScheduler(...)`가 아직도:

```go
poller.NewScheduler(poller.SchedulerConfig{
    WorkerCount: 2,
    RequestInterval: 0,
})
```

로 박혀 있습니다.

즉 poll interval은 config화했지만 scheduler capacity는 여전히 configurable하지 않습니다.

### P0-6. 기본 poll cadence가 여전히 공격적일 수 있음

- `hololive/hololive-shared/pkg/config/config_types.go:90`
  - Videos: 5m
  - Shorts: 10m
  - Community: 10m
  - Stats: 6h
  - Live: 5m
- `hololive/hololive-shared/pkg/constants/api.go`
  - scraper request interval: 3s
  - distributed limit: 1 request / 3s

이 구성에서 채널 수를 `N`이라고 하면 분당 기대 요청량은 대략:

`N * (1/5 + 1/10 + 1/10 + 1/360 + 1/5) = N * 0.6028 req/min`

레이트리미터 총량은 약 `20 req/min`입니다.  
즉 **지속 가능한 채널 수는 약 33개 수준**입니다.

실운영 채널 수가 이보다 많으면, env wiring이 생겼더라도 구조적으로 backlog가 남습니다.

---

## 구조적으로 좋아졌지만 아직 AI 흔적이 남는 부분

### 1) 동일 helper가 아직 복제돼 있음

- `hololive/hololive-stream-ingester/internal/app/runtime_helpers.go:30`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_proxy_toggle.go:31`

`applyScraperProxyToggle(...)` 구현이 사실상 동일합니다.

### 2) 동일 runtime Close wrapper가 5군데 남아 있음

예:
- `hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_runner.go`
- `hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go`
- `hololive/hololive-kakao-bot-go/internal/app/runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/db_integration_runtime.go`
- `hololive/hololive-kakao-bot-go/internal/app/fetch_profiles_runtime.go`

모두 아래 형태입니다.

```go
func (r *XRuntime) Close() {
    if r == nil {
        return
    }
    r.CleanupCloser.Close()
}
```

이건 shared lifecycle utility를 만들었는데 마지막 wrapper 표면은 그대로 남은 상태입니다.

### 3) app/provider duplication이 아직 큼

대표 예:
- `ProvideInfraResources`
  - `hololive/hololive-stream-ingester/internal/app/providers/infra_resources.go`
  - `hololive/hololive-kakao-bot-go/internal/app/providers/infra_resources.go`
- `ProvideYouTubeStack`
  - `hololive/hololive-stream-ingester/internal/app/providers/youtube.go`
  - `hololive/hololive-kakao-bot-go/internal/app/providers/youtube.go`
- `ProvideAPIServer`
  - bot / llm-sched / stream-ingester 각자 존재

즉 “module struct로 정리 시작”은 했지만,  
조립 유틸리티 전반은 아직 runtime별 복제가 꽤 많습니다.

### 4) hololive-shared는 여전히 shared-monolith

대략적인 현재 규모:
- `hololive/hololive-shared`: **330개 Go 파일 / 약 60,793 LOC**

특히
- `pkg/service`: 약 **41,667 LOC**
- `pkg/providers`: 약 **1,134 LOC**

shared가 여전히 “공용 타입/헬퍼” 수준을 넘어서 runtime orchestration과 domain service를 대량 포함합니다.

즉 이름은 shared지만 실제로는 platform monolith 성격이 여전합니다.

---

## repo-wide 관점에서 아직 덜 끝난 부분

### 1) 저장소 자급자족화가 절반만 완료됨

좋아진 점:
- `go.work:4` → `./shared-go`

남은 문제:
- `build-all.sh:19` → 기본 경로가 `../llm/shared-go`
- `scripts/deploy/compose-redeploy-service.sh:13` → 동일
- `docker-compose.prod.yml:147`, `235`, `291`, `368`, `445` → `additional_contexts.shared_go_workspace: ../llm/shared-go`

즉 workspace는 in-repo 기준으로 바뀌었지만
실제 빌드/배포 파이프라인은 여전히 외부 canonical workspace를 기본값으로 가정합니다.

이 상태는 **“개발은 self-contained처럼 보이는데 배포는 외부 경로를 암묵 전제”** 하는 반쪽 상태입니다.

### 2) architecture gate는 일부만 실효화됨

좋아진 점:
- `docs/architecture/release-governance-assets.txt` 내용 있음
- `docs/architecture/shared-go-package-allowlist.txt` 내용 있음

남은 문제:
- `docs/architecture/go-module-loc-thresholds.txt`는 사실상 header만 있고 threshold entry가 없음

결과적으로 `scripts/architecture/check-go-module-loc.sh`는  
파일을 읽긴 하지만 **실제 제약 파일이 비어 있어 M4 gate가 실질적으로 아무 것도 막지 못합니다.**

---

## admin-dashboard 리뷰

### 좋아진 점

- `frontend/src/features/*` 폴더 추가
- `src/types/index.ts` 같은 전역 수동 DTO 파일은 정리된 편
- `src/api/generated/*` 도입
- 백엔드 OpenAPI가 auth/docker/status는 실제 핸들러 기준으로 정리됨

### 아직 남은 핵심 문제

#### 1) `/admin/api/holo/*`는 여전히 blind proxy
- `admin-dashboard/backend/src/routes.rs:73`

```rust
.route("/admin/api/holo/{*path}", any(crate::proxy::bot_proxy::proxy_holo))
```

즉 dashboard backend는 holo contract를 소유하지 않고,
그냥 upstream bot API를 proxy하고 있습니다.

#### 2) 프런트엔드도 holo 쪽은 여전히 수동 래퍼/수동 타입
- `admin-dashboard/frontend/src/features/alarms/api.ts:4`
- `admin-dashboard/frontend/src/features/alarms/types.ts`
- `admin-dashboard/frontend/src/api/core.ts:36`

auth/docker/status는 generated contract 쪽이지만,
holo alarm/member/room/settings 쪽은 아직 feature별 수동 wrapper입니다.

즉 generated client와 manual transport contract가 혼재합니다.

#### 3) giant tab component가 그대로 큼
- `admin-dashboard/frontend/src/components/AlarmsTab.tsx` → 382 LOC
- `admin-dashboard/frontend/src/components/MembersTab.tsx` → 376 LOC
- `admin-dashboard/frontend/src/components/RoomsTab.tsx` → 329 LOC
- `admin-dashboard/frontend/src/components/StatsTab.tsx` → 297 LOC
- `admin-dashboard/frontend/src/components/StreamsTab.tsx` → 288 LOC

feature 폴더는 생겼지만 route entry component는 아직 query/mutation/filter/rendering/state를 많이 안고 있습니다.

즉 **구조 개선은 시작됐지만 decomposition은 아직 절반 정도**입니다.

---

## 정량 관찰

### Go 규모
- `shared-go`: 30 files / 3,826 LOC
- `hololive-shared`: 330 files / 60,793 LOC
- `hololive-kakao-bot-go`: 264 files / 45,243 LOC
- `hololive-llm-sched`: 94 files / 19,992 LOC
- `hololive-stream-ingester`: 26 files / 3,035 LOC
- `hololive-dispatcher-go`: 12 files / 2,327 LOC

### duplicate / wrapper heuristic
정적 스캔 기준:
- non-test Go 함수 duplicate body group: **62개**
- trivial wrapper로 보이는 함수: **수백 개 수준**
- `Provide*` 함수: **57개**

이 수치만으로 “나쁘다”라고 단정할 수는 없지만,
현재 저장소가 아직도 **manual DI helper / wrapper / thin adapter**가 과한 편이라는 정황은 분명합니다.

---

## 최종 판단

### 등급
- **운영 안정화 관점:** B
- **구조 정리 관점:** B-
- **repo-wide 완성도 관점:** C+
- **AI 흔적 제거 관점:** C+

### 한 줄 평가
이 버전은 **“문제 중심 핫픽스 + 1차 구조 정리”까지는 성공**했습니다.  
하지만 아직 **repo-wide source of truth 정리, 중복 provider/helper 정리, shared monolith 슬리밍, dashboard contract 일원화**는 끝나지 않았습니다.

---

## 다음 우선순위

### 반드시 바로 할 것
1. `targetMinutes` normalization을 shared single source로 통합
2. runtime scheduler가 `AlarmService` 변경값을 `YouTubeChecker`/`dedup.Service`에 재주입
3. `docker-compose.prod.yml`에 scraper poll env 노출
4. `ProvideScraperScheduler` worker count config화
5. `worker_channel_full => +10s` 로직 제거

### 바로 다음 단계
6. `applyScraperProxyToggle` 단일화
7. runtime `Close()` wrapper 공통화 또는 제거
8. `ProvideInfraResources` / `ProvideYouTubeStack` 중복 제거
9. `build-all.sh`, deploy script, compose의 `../llm/shared-go` 기본값 제거
10. `go-module-loc-thresholds.txt` 실제 threshold 채우기

### 구조 리팩토링 단계
11. `hololive-shared` slimming
12. dashboard holo contract를 backend가 직접 소유하도록 전환
13. giant tab component를 feature entry + page/presenter split로 재분해

---

## 검증 메모

이 문서 시점에는 `go.work`가 `go 1.26.1`을 요구했지만 실행 환경의 로컬 Go는 `1.23.2`였고,
네트워크가 막혀 toolchain download가 불가능해서 전체 `go test` 실행은 하지 못했습니다.

따라서 본 리뷰는
- 정적 코드 분석
- 구조 비교
- 설정 wiring 검토
- 파일/LOC/중복도 스캔

기반으로 작성했습니다.
