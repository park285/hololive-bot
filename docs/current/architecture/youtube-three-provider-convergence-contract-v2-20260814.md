# YouTube Three-Provider Convergence — Code-Level Architecture and Implementation Contract v2.1

## 문서 상태

이 문서는 `park285/hololive-bot`의 로컬 스냅샷에 포함된 다음 두 문서를 코드 수준으로 대체하는 규범 문서다.

- `docs/current/architecture/youtube-three-provider-observation-target-20260814.md`
- `docs/current/plans/2026-08-14-youtube-three-provider-convergence.md`

기존 Community vertical slice의 `legacy/shadow/authoritative`, 중앙 singleton collector, `youtube-producer` consumer 구조는 중간 WIP일 뿐이며 최종 branch에 남기지 않는다. 이 문서는 목표 구조와 구현 순서를 함께 소유한다. 실제 target code, migration 적용, 배포 완료를 주장하지 않는다.

이 문서에서 `MUST`, `MUST NOT`, `SHOULD`는 구현 계약을 뜻한다. production migration, deploy, restart, live data 변경은 별도 승인 없이는 수행하지 않는다.

### 근거와 신규 설계 결정의 구분

제공된 snapshot에서 직접 확인된 사실은 다음과 같다.

- Community WIP는 authority fence, checkpoint, observation outbox와 producer consumer를 사용한다.
- current publish repository는 checkpoint의 observation key/payload hash/generation이 같으면 후속 observation 발행을 생략한다.
- producer는 Community 외에도 videos, shorts, live, channel stats와 photo sync를 소유한다.
- 기존 target refresher에는 5초 refresh와 30초 empty/cache grace 동작이 존재한다.
- `hololive-api` bot/admin/llm plane은 각각 bounded PostgreSQL pool을 소유하며 제공된 코드의 default max는 4다.
- repository blocking gate는 `./build-all.sh --no-bump`와 `PRE_PUSH_MODE=full ./scripts/ci/pre-push-gate.sh`다.

이 문서가 개선안으로 새로 고정하는 결정은 다음과 같다.

- immutable evidence와 mutable processing queue를 분리한다.
- PostgreSQL scheduled slot은 same-run retry identity를, monotonic fence epoch는 stale holder 차단을 소유한다.
- payload가 같아도 다음 successful collection slot은 새 observation을 발행한다.
- `channel_stats`를 `viewer_sample`과 분리한다.
- target projection은 staging/current generation으로 원자 교체한다.
- scoped absence에는 typed coverage, strict newer-time, 서로 다른 slot의 2회 확인과 grace를 요구한다.
- grace가 지난 뒤 새 observation이 없어도 DB due-finalizer가 LIVE end candidate를 재평가한다.
- profile/photo는 provider precedence와 arrival-order last-write-wins를 사용하지 않는다.
- pool, worker, retention 기본값은 초기 구현값이며 representative capacity gate 없이 성능 개선으로 주장하지 않는다.

### 근거와 새 규범 선택의 구분

첨부 snapshot이 직접 지지하는 사실은 현재 Community intermediate slice, migration `144` WIP, producer의 stats/live/videos/shorts/profile/photo 책임, AP producer topology와 기존 targeted test 결과다. 반면 아래 값은 snapshot에 이미 존재하는 운영값이라고 주장하지 않는 **v2.1 초기 규범 default**다. 구현 시 settings와 regression test로 고정하며 변경하려면 같은 수준의 contract review가 필요하다.

- `source_event_at` 미래 skew default `5m`, 허용 설정 범위 `0..15m`
- scoped absence 기반 tombstone/end에 필요한 서로 다른 complete-negative slot 최소 `2`개
- API YouTube plane 초기 pool/worker/claim 예산 `4/2/4`, 공용 DB operation concurrency `3`과 transaction/lease 관계

retention 기간, 기존 product grace, provider별 request budget처럼 snapshot이 정확한 수치를 제공하지 않은 항목은 임의 숫자를 만들지 않고 inventory와 측정을 implementation prerequisite로 남긴다.

### 이전 리뷰 finding closure

| 이전 finding | 이 문서의 닫는 계약 | 소유 section |
|---|---|---|
| 반복 snapshot/sample identity 손실 | DB `scheduled_for` slot identity, `evidence_sha256`, payload가 같아도 다음 slot 보존 | 4, 6, 10 |
| stale collector publish race | PostgreSQL monotonic `fence_epoch`와 publish-time row lock predicate | 8 |
| coverage·clock·LIVE 종료 모호성 | typed coverage relation, absence capability, effective clock, DB grace, two-slot scoped absence | 5, 6, 13.3 |
| target stale/empty/disable 모호성 | staging/current/retired generation의 atomic swap과 `valid_until` | 7 |
| `channel_stats` 누락 | 독립 kind·payload·snapshot/latest parity wave | 3.2, 13.5, Task 7 |
| profile/photo arrival-order 의존 | field presence/validity/stability reducer와 media identity 규칙 | 13.6, 13.7 |
| collision DLQ 불가능 | immutable evidence와 별도 bounded collision audit | 9.3, 9.4, 10.2 |
| retention/replay·manifest/grant 누락 | queue/evidence 수명 분리, replay audit, migration manifest·grant test | 9, 12, Tasks 1·8 |
| API pool/readiness budget 누락 | dedicated bounded pool, worker/claim/lease validation, degraded/readiness 분리 | 14 |
| canonical gate 누락 | repository full pre-push와 stack gate를 blocking으로 명시 | Task 10 |

### 수용 후 v2.1 보정

| 재검토 finding | v2.1 계약 |
|---|---|
| 동일 projection의 5초 generation churn | normalized hash와 row count가 같으면 generation을 유지하고 validity만 갱신 |
| kind별 lease와 multi-kind batch 충돌 | collection job kind와 observation kind를 분리하고 subject bundle을 허용 |
| 장기 중단 뒤 과거 slot 누적 | DB `date_bin` 기반 missed-slot coalescing으로 최신 due boundary 하나만 수집 |
| explicit end와 scoped absence predicate 혼합 | explicit end/cancel validity와 scoped absence eligibility를 별도 판정 |
| due-finalizer/conflict 저장소 누락 | live reconciliation head와 reconciliation conflict audit를 migration에 포함 |
| viewer/profile/photo 결정 미완료 | viewer conflict는 `UNRESOLVED`로 고정하고 stability 설정 미확정 시 canonical change를 disable |
| pool validation이 보조 loop를 누락 | plane 전체 DB operation이 max 3인 공용 semaphore를 통과 |
| audit table bounds 누락 | text/hash/vocab/FK/terminal-shape constraint를 migration contract에 포함 |

## 1. 사용자 가시 결과와 핵심 invariant

최종 branch는 다음 결과를 만족해야 한다.

1. evidence provider는 `holodex`, `youtubejs`, `hololive_official` 세 개다.
2. provider 이름은 provenance와 capability를 나타낼 뿐, primary/fallback/authority 우선순위를 나타내지 않는다.
3. 동일한 evidence 집합은 도착 순서와 collector instance에 관계없이 같은 canonical state와 notification intent를 만든다.
4. `youtube-collector`는 AP fleet에서 외부 수집, 정규화, collection lease, checkpoint, observation publish만 소유한다.
5. `hololive-api` YouTube plane만 canonical YouTube state, domain watermark, notification intent, replay와 retention을 소유한다.
6. `alarm-worker`만 proactive notification delivery와 Iris/Kakao egress를 소유한다.
7. `youtube-producer` binary, module, Compose profile, systemd helper, runtime registration, PGO inventory와 current runbook은 최종 branch에서 제거한다.
8. collector publish와 collection checkpoint는 한 PostgreSQL transaction이다.
9. API canonical write, notification intent, observation completion은 한 PostgreSQL transaction이다.
10. collection job의 stale holder는 PostgreSQL의 단조 fencing epoch로 publish가 차단된다.
11. queue, worker, batch, retry, payload, DB pool과 외부 요청은 모두 bounded다.
12. provider outage, timeout, parse drift, partial pagination, continuity gap은 negative evidence가 아니다.
13. `channel_stats`는 `viewer_sample`이나 `channel_profile`에 암묵적으로 포함하지 않고 독립 observation kind로 보존한다.
14. migration `144`가 production 미적용이라는 전제가 확인된 경우에만 direct rewrite하며 compatibility migration이나 dual writer를 만들지 않는다.

## 2. Runtime 및 코드 소유 경계

| Runtime | 소유 | 금지 소유 |
|---|---|---|
| `youtube-collector` | external clients, provider adapters, fixture-backed parsing, normalization, collection target read, DB job lease/fence, bounded scheduling, rate limit/retry/cooldown, checkpoint, observation publish, provider health | canonical tables, live transition, domain watermark, notification intent/outbox, profile/photo 최종 선택, proactive egress |
| `hololive-api` YouTube plane | target projection, observation claim/finalize, source-neutral reconciliation, canonical writes, notification intent, conflict/provenance, replay, retention, YouTube plane health | external scraping, provider precedence, proactive egress |
| `alarm-worker` | notification claim, rendering, retry, delivery ledger, Iris/Kakao egress | external collection, YouTube canonical detection write |

규범 파일 경계는 다음과 같다.

```text
hololive/hololive-shared/pkg/contracts/sourceobservation/
  envelope.go
  identity.go
  coverage.go
  community_v1.go
  video_list_v1.go
  shorts_list_v1.go
  live_snapshot_v1.go
  viewer_sample_v1.go
  channel_stats_v1.go
  channel_profile_v1.go
  channel_photo_v1.go
  schedule_snapshot_v1.go

hololive/hololive-shared/pkg/service/youtube/sourceobservation/
  repository.go
  repository_publish.go
  repository_claim.go
  repository_finalize.go
  repository_replay.go
  repository_retention.go
  errors.go
  queries/*.sql

hololive/hololive-shared/pkg/service/youtube/reconcile/
  community/
  content/
  live/
  channel/
  schedule/

hololive/hololive-youtube-collector/internal/runtime/
  collectorruntime/
  joblease/
  holodexcollector/
  officialcollector/
  youtubejscollector/

hololive/hololive-api/internal/planes/youtube/
  app/
  runtime/
  targetprojection/
  processor/
  health/
```

실제 repository에 이미 더 좁은 owning package가 있으면 그 owner를 유지하고 위 이름을 기계적으로 추가하지 않는다. 다만 contract, collector adapter, reconciler, API runtime orchestration의 경계는 섞지 않는다.

금지 import는 architecture gate로 고정한다.

- collector package는 canonical repository, notification outbox writer, alarm service를 import하지 않는다.
- reconciler package는 Holodex/YouTube.js/Official client나 provider parser를 import하지 않는다.
- alarm-worker는 observation publish/claim repository를 import하지 않는다.
- API YouTube plane은 producer `internal` package를 import하지 않는다.

## 3. Provider와 observation kind 계약

### 3.1 Provider

```go
type Provider string

const (
    ProviderHolodex          Provider = "holodex"
    ProviderYouTubeJS        Provider = "youtubejs"
    ProviderHololiveOfficial Provider = "hololive_official"
)
```

Provider 지원 범위는 capability matrix로 선언한다. capability는 어떤 evidence를 발행할 수 있는지를 뜻하며 canonical 우선순위가 아니다. 지원하지 않는 provider/kind 조합은 empty observation을 만들지 않는다.

### 3.2 Observation kind

```go
type ObservationKind string

const (
    KindCommunityPage  ObservationKind = "community_page"
    KindVideoList      ObservationKind = "video_list"
    KindShortsList     ObservationKind = "shorts_list"
    KindLiveSnapshot   ObservationKind = "live_snapshot"
    KindViewerSample   ObservationKind = "viewer_sample"
    KindChannelStats   ObservationKind = "channel_stats"
    KindChannelProfile ObservationKind = "channel_profile"
    KindChannelPhoto   ObservationKind = "channel_photo"
    KindSchedule       ObservationKind = "schedule_snapshot"
)
```

`channel_stats`는 subscriber count, channel view count, video count의 시계열 snapshot을 소유한다. `viewer_sample`은 개별 방송의 동시 시청자 시계열이며 서로 대체하지 않는다. `channel_profile`은 handle, description, country, joined date 같은 profile 필드를 소유하고, `channel_photo`는 avatar/banner variant를 소유한다.

### 3.3 초기 capability matrix

| Provider | Kind | 초기 상태 |
|---|---|---|
| `youtubejs` | community, video, shorts, live, viewer, channel stats/profile/photo | fixture로 안정 semantics가 입증된 kind만 enable |
| `holodex` | live, schedule, channel metadata/photo | 실제 API 응답이 제공하고 fixture가 있는 필드만 enable |
| `hololive_official` | schedule | 검증된 Schedule JSON API 범위만 enable |

한 kind를 두 provider가 동시에 지원해도 reducer는 provider 이름을 tie-breaker로 사용하지 않는다.

## 4. Observation envelope v2

### 4.1 Go contract

```go
type Completeness string

const (
    CompletenessComplete Completeness = "COMPLETE"
    CompletenessPartial  Completeness = "PARTIAL"
    CompletenessUnknown  Completeness = "UNKNOWN"
)

type Continuity string

const (
    ContinuityContiguous    Continuity = "CONTIGUOUS"
    ContinuityGapUnresolved Continuity = "GAP_UNRESOLVED"
    ContinuityNotApplicable Continuity = "NOT_APPLICABLE"
)

type LeaseProof struct {
    JobKey               string    `json:"job_key"`
    CollectionJobKind    string    `json:"collection_job_kind"`
    OwnerInstance        string    `json:"owner_instance"`
    FenceEpoch           int64     `json:"fence_epoch"`
    ProjectionGeneration int64     `json:"projection_generation"`
    ScheduledFor         time.Time `json:"scheduled_for"`
}

type Envelope struct {
    Provider           Provider        `json:"provider"`
    ObservationKind    ObservationKind `json:"observation_kind"`
    SubjectKey         string          `json:"subject_key"`
    ObservationKey     string          `json:"observation_key"`
    SchemaVersion      int16           `json:"schema_version"`
    ContractGeneration int64           `json:"contract_generation"`

    ScheduledFor  time.Time  `json:"scheduled_for"`
    ObservedAt    time.Time  `json:"observed_at"`
    SourceEventAt *time.Time `json:"source_event_at,omitempty"`

    ScopeSHA256  string       `json:"scope_sha256"`
    Completeness Completeness `json:"completeness"`
    Continuity   Continuity   `json:"continuity"`

    Payload        json.RawMessage `json:"payload"`
    PayloadSHA256  string          `json:"payload_sha256"`
    EvidenceSHA256 string          `json:"evidence_sha256"`

    CollectorInstance string     `json:"collector_instance"`
    Lease             LeaseProof `json:"lease"`
}
```

`received_at`은 collector가 보내지 않는다. PostgreSQL이 insert 시점에 생성한다. collector wall clock은 domain ordering의 SSOT가 아니다.

### 4.1.1 Canonical JSON identity contract

`scope_sha256`, `payload_sha256`, `evidence_sha256`과 `observation_key`의 JSON input은 [`source-observation-canonical-json-v1`](../contracts/source-observation-canonical-json-v1.md)을 사용한다. 이 profile은 RFC 8785/JCS 출력과 일치하는 safe-integer strict subset이며 Go `encoding/json`의 map ordering, number spelling 또는 HTML escaping을 identity 계약으로 사용하지 않는다.

language conformance 기준은 `hololive/hololive-shared/pkg/contracts/sourceobservation/testdata/canonical_json_v1.json` 하나다. 허용된 모든 implementation은 fixture의 canonical bytes와 SHA-256을 동일하게 만들고 rejection case를 fail closed해야 한다. profile 변경은 fixture와 source observation contract generation을 함께 version-up한다.

### 4.2 필드 의미

- `subject_key`: kind가 직접 대상으로 삼는 안정 identity다. channel snapshot은 channel ID, viewer sample은 video ID, global batch snapshot은 `global:hololive-schedule`처럼 contract가 정의한 namespaced subject key다.
- `scheduled_for`: PostgreSQL job lease가 배정한 수집 슬롯이다. snapshot freshness와 retry identity의 SSOT다. collector의 `time.Now()`를 사용하지 않는다.
- `observed_at`: 실제 외부 응답을 수신한 collector 시각이며 latency와 clock-skew 진단에만 사용한다.
- `source_event_at`: provider가 명시적인 event/revision 시각을 제공할 때만 채운다.
- `scope_sha256`: payload 안의 typed coverage를 canonical JSON으로 직렬화한 SHA-256이다.
- `payload_sha256`: kind payload의 canonical JSON SHA-256이다.
- `evidence_sha256`: payload뿐 아니라 scope, completeness, continuity, source event time 같은 reconciliation 의미를 포함한 semantic evidence hash다.
- `collector_instance`, `job_key`, `fence_epoch`, `projection_generation`은 provenance와 stale writer 차단용이며 provider 우선순위가 아니다.

### 4.3 semantic evidence hash

다음 구조를 canonical JSON으로 직렬화해 `evidence_sha256`을 계산한다.

```go
type EvidenceDigestV1 struct {
    Provider           Provider        `json:"provider"`
    ObservationKind    ObservationKind `json:"observation_kind"`
    SubjectKey         string          `json:"subject_key"`
    ObservationKey     string          `json:"observation_key"`
    SchemaVersion      int16           `json:"schema_version"`
    ContractGeneration int64           `json:"contract_generation"`
    ScheduledFor       time.Time       `json:"scheduled_for"`
    SourceEventAt      *time.Time      `json:"source_event_at,omitempty"`
    ScopeSHA256        string          `json:"scope_sha256"`
    Completeness       Completeness    `json:"completeness"`
    Continuity         Continuity      `json:"continuity"`
    PayloadSHA256      string          `json:"payload_sha256"`
}
```

`observed_at`, `received_at`, collector instance, job owner와 lease expiry는 semantic hash에서 제외한다. 동일 수집 슬롯의 network retry가 clock 차이만으로 collision이 되지 않게 하기 위함이다.

동일 identity에서 다음 중 하나라도 바뀌면 collision이다.

- payload
- typed coverage
- completeness
- continuity
- source event time
- schema version 또는 contract generation

### 4.4 Kind별 observation identity

공통 unique identity는 다음과 같다.

```text
(provider, observation_kind, subject_key, observation_key, schema_version, contract_generation)
```

`observation_key` 생성 규칙은 kind별로 고정한다.

| Kind | `subject_key` | `observation_key`의 semantic 입력 |
|---|---|---|
| `community_page` | channel ID | `scheduled_for + scope_sha256` |
| `video_list` | channel ID 또는 global group | `scheduled_for + scope_sha256` |
| `shorts_list` | channel ID | `scheduled_for + scope_sha256` |
| `live_snapshot` | channel ID 또는 global group | `scheduled_for + scope_sha256` |
| `viewer_sample` | video ID | normalized sample-window start + scope hash |
| `channel_stats` | channel ID | `scheduled_for + scope_sha256` |
| `channel_profile` | channel ID | `scheduled_for + scope_sha256` |
| `channel_photo` | channel ID | `scheduled_for + scope_sha256` |
| `schedule_snapshot` | group key | `scheduled_for + scope_sha256` |

snapshot key는 다음 helper만 사용한다.

```go
func SnapshotObservationKey(
    provider Provider,
    kind ObservationKind,
    subjectKey string,
    scopeSHA256 string,
    scheduledFor time.Time,
) string
```

이 helper는 canonical tuple의 SHA-256을 반환한다. `scheduled_for`는 DB가 배정한 slot이므로 임의 poll timestamp가 아니다.

결과적으로 다음 계약이 성립한다.

- 같은 payload라도 다음 수집 slot이면 새로운 observation이다.
- 같은 slot의 동일 retry는 하나의 observation으로 멱등 처리된다.
- `viewer_sample` 값이 두 slot 연속 같아도 두 sample이 보존된다.
- payload가 같아도 `PARTIAL -> COMPLETE`, `GAP -> CONTIGUOUS`, scope 변경은 collision로 드러난다.

## 5. Typed coverage와 negative evidence

### 5.1 공통 원칙

coverage는 공용 `map[string]any`로 만들지 않는다. 각 payload V1은 자신의 typed coverage를 포함한다. contract validator는 kind별 payload를 strict JSON decode하고 coverage를 검증한 뒤 `scope_sha256`을 재계산한다.

예시는 다음과 같다.

```go
type ChannelListCoverageV1 struct {
    ChannelID string `json:"channel_id"`
    MaxResults int   `json:"max_results"`
    CursorStart string `json:"cursor_start,omitempty"`
    CursorEnd   string `json:"cursor_end,omitempty"`
    Exhausted   bool   `json:"exhausted"`
    Filters     VideoListFiltersV1 `json:"filters"`
}

type GlobalChannelCoverageV1 struct {
    RequestedChannelIDs []string `json:"requested_channel_ids"`
    GroupKey            string   `json:"group_key,omitempty"`
    Filters             LiveFiltersV1 `json:"filters"`
}
```

모든 ID slice는 trim, validate, sort, deduplicate한 뒤 hash한다. caller가 전달한 순서가 scope identity를 바꾸지 않는다.

### 5.2 completeness와 continuity

negative evidence가 되려면 다음 조건을 모두 만족해야 한다.

```go
func NegativeEligible(meta EvidenceMeta) bool {
    return meta.Completeness == CompletenessComplete &&
        (meta.Continuity == ContinuityContiguous ||
         meta.Continuity == ContinuityNotApplicable)
}
```

`UNKNOWN`, `PARTIAL`, `GAP_UNRESOLVED`는 positive entity를 포함할 수 있지만 부재를 증명하지 못한다.

### 5.3 Pagination

V1은 한 collection run의 모든 page를 성공적으로 읽고 bounded payload limit 안에 합친 경우에만 `COMPLETE` aggregate observation을 publish한다.

- 중간 page 실패, cursor loop, max-page 도달, payload limit 도달은 `PARTIAL`이다.
- partial aggregate가 empty여도 negative evidence가 아니다.
- collector는 실패한 page를 complete-empty로 변환하지 않는다.
- max page와 max payload는 provider adapter config에서 bounded다.
- chunk/manifest protocol은 V1 비범위다. V1 limit을 초과하는 source는 `PARTIAL`로 남기고 별도 versioned contract 없이 임의 chunking을 추가하지 않는다.

### 5.4 Coverage relation

reconciler가 absence를 적용하기 전에 kind별 relation을 계산한다.

```go
type CoverageRelation uint8

const (
    CoverageDisjoint CoverageRelation = iota
    CoverageEqual
    CoverageCovers
    CoverageCoveredBy
)
```

absence 후보 entity에 대해 `Equal` 또는 `Covers`인 complete evidence만 사용할 수 있다. scope 밖 entity는 무시한다. 서로 다른 filter나 time range는 자동으로 동등하다고 보지 않는다.

### 5.5 Kind capability와 absence 권한

provider/kind fixture가 complete absence semantics를 증명했을 때만 해당 adapter를 `SCOPED_ABSENCE` capability로 등록한다. 나머지는 `POSITIVE_ONLY`다.

```go
type AbsenceCapability string

const (
    AbsencePositiveOnly AbsenceCapability = "POSITIVE_ONLY"
    AbsenceScoped       AbsenceCapability = "SCOPED_ABSENCE"
)
```

- `POSITIVE_ONLY` observation은 `COMPLETE`여도 canonical absence/tombstone/end 입력으로 사용하지 않는다.
- Community V1은 upstream page가 전체 역사나 안정 window를 증명하지 않는 한 `POSITIVE_ONLY`다.
- global batch가 요청 channel subset과 실패 channel을 구분하지 못하면 `SCOPED_ABSENCE`를 선언할 수 없다.
- capability matrix는 typed contract와 fixture test가 소유하며 runtime 설정만으로 격상할 수 없다.
- negative reducer는 `NegativeEligible`뿐 아니라 `AbsenceCapability == SCOPED_ABSENCE`를 확인한다.

## 6. Temporal ordering과 clock 계약

### 6.1 세 시각의 역할

| 시각 | 소유 | 용도 |
|---|---|---|
| `scheduled_for` | PostgreSQL collection lease | snapshot/sample freshness와 동일 run retry identity |
| `source_event_at` | provider | 명시적 upstream event/revision ordering |
| `received_at` | PostgreSQL | 도착·처리 lag, grace elapsed, 운영 audit |
| `observed_at` | collector | 수집 latency·clock skew 진단만 사용 |

### 6.2 Effective time

```go
func EffectiveAt(observation Observation) time.Time {
    if observation.SourceEventAt != nil && SourceEventAtAllowed(
        observation,
        DefaultMaxSourceEventFutureSkew,
    ) {
        return observation.SourceEventAt.UTC()
    }
    return observation.ScheduledFor.UTC()
}
```

`source_event_at` 사용 여부는 kind contract에 명시한다. list snapshot의 arbitrary item time을 전체 snapshot effective time으로 승격하지 않는다.

### 6.3 `source_event_at` 신뢰 경계

`source_event_at`은 provider가 명시적으로 제공했다는 이유만으로 무조건 ordering clock이 되지 않는다. API consumer는 DB `received_at`을 기준으로 다음 predicate를 적용한다.

```go
const DefaultMaxSourceEventFutureSkew = 5 * time.Minute

func SourceEventAtAllowed(
    observation Observation,
    maxFutureSkew time.Duration,
) bool {
    if observation.SourceEventAt == nil {
        return false
    }
    if !KindAllowsSourceEventTime(observation.ObservationKind) {
        return false
    }
    return !observation.SourceEventAt.After(
        observation.ReceivedAt.Add(maxFutureSkew),
    )
}
```

- 초기 default는 `5m`이며 startup validation 범위는 `0..15m`다. 이 값은 correctness budget이므로 무제한 설정을 허용하지 않는다.
- 허용 범위를 넘는 미래 시각은 raw provenance에는 보존하되 canonical ordering에는 사용하지 않고 `scheduled_for`로 fallback한다.
- invalid future event time은 `youtube_observation_clock_skew_total{provider,kind,direction="future"}`와 bounded structured log로 구분한다.
- 과거 event time은 게시 지연을 표현할 수 있으므로 일률적으로 거부하지 않는다. 다만 kind별 domain validator가 허용 범위를 별도로 좁힐 수 있다.
- collector `observed_at`은 이 검증의 대체 기준이 아니다. AP clock이 domain ordering을 결정하지 않게 하기 위함이다.

### 6.4 동일 시각 conflict

- 같은 entity, 같은 effective time, 같은 semantic value는 replay다.
- 같은 effective time의 positive와 negative가 충돌하면 positive가 우선한다. 이는 provider 우선순위가 아니라 안전한 state transition 규칙이다.
- 같은 effective time에 서로 다른 positive 값이 충돌하면 arrival order로 선택하지 않는다. conflict를 기록하고 기존 canonical 값을 유지한다.
- DB observation ID나 collector instance를 canonical tie-breaker로 사용하지 않는다.

## 7. Collection target projection

### 7.1 목표

API는 operational roster, subscription, birthday/live policy를 해석해 collector가 소비할 수 있는 완전한 projection을 만든다. collector는 정책 이유를 해석하지 않는다.

Projection은 generation 단위로 원자적으로 교체한다. refresh 실패가 current target set을 빈 집합으로 덮어쓰면 안 된다.

target source mapping은 다음과 같다.

- notification target: `community_page`, `video_list`, `shorts_list`
- operational roster: `live_snapshot`, `viewer_sample`, `channel_stats`, `channel_profile`, `channel_photo`
- fixed global operational target: `schedule_snapshot` with `subject_key=global:hololive-schedule`

### 7.2 Schema shape

```sql
CREATE TABLE youtube_collection_projection_generations (
    generation BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('STAGING', 'CURRENT', 'RETIRED')),
    row_count INTEGER NOT NULL CHECK (row_count >= 0),
    projection_sha256 TEXT NOT NULL CHECK (projection_sha256 ~ '^[0-9a-f]{64}$'),
    valid_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ,
    CONSTRAINT chk_youtube_collection_projection_activation_shape CHECK (
        (status = 'STAGING' AND activated_at IS NULL)
        OR
        (status IN ('CURRENT', 'RETIRED') AND activated_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_youtube_collection_projection_one_current
    ON youtube_collection_projection_generations ((status))
    WHERE status = 'CURRENT';

CREATE TABLE youtube_collection_targets (
    projection_generation BIGINT NOT NULL
        REFERENCES youtube_collection_projection_generations(generation)
        ON DELETE CASCADE,
    subject_key TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    priority SMALLINT NOT NULL CHECK (priority BETWEEN 0 AND 100),
    poll_interval_ms BIGINT NOT NULL CHECK (poll_interval_ms BETWEEN 1000 AND 86400000),
    enabled BOOLEAN NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (projection_generation, subject_key, observation_kind),
    CHECK (length(subject_key) BETWEEN 1 AND 256)
);

CREATE TABLE youtube_collection_target_reasons (
    projection_generation BIGINT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    reason_kind TEXT NOT NULL,
    reason_key TEXT NOT NULL,
    PRIMARY KEY (
        projection_generation,
        subject_key,
        observation_kind,
        reason_kind,
        reason_key
    ),
    FOREIGN KEY (projection_generation, subject_key, observation_kind)
        REFERENCES youtube_collection_targets(
            projection_generation,
            subject_key,
            observation_kind
        ) ON DELETE CASCADE
);
```

`youtube_collection_target_reasons`는 API 진단용이며 collector에 SELECT grant를 주지 않는다. `subject_key`는 `channel:<id>`뿐 아니라 `global:hololive-schedule` 같은 global target을 표현한다.

collector가 읽을 수 있는 target은 current generation과 유효 기간을 통과한 row뿐이다.

```sql
SELECT target.projection_generation,
       target.subject_key,
       target.observation_kind,
       target.priority,
       target.poll_interval_ms,
       target.valid_until
FROM youtube_collection_projection_generations AS generation
JOIN youtube_collection_targets AS target
  ON target.projection_generation = generation.generation
WHERE generation.status = 'CURRENT'
  AND generation.valid_until > NOW()
  AND target.enabled = TRUE
  AND target.valid_until > NOW();
```

### 7.3 Rebuild algorithm

```go
type TargetProjectionBuilder interface {
    Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]TargetSpec, []TargetReason, error)
}
```

API refresh transaction은 다음 순서를 따른다.

1. 모든 authoritative input을 읽는다. 하나라도 실패하면 rollback한다.
2. normalized `TargetSpec`을 `(subject_key, observation_kind)`로 deterministic sort/dedup하고 projection SHA-256을 계산한다. hash 입력은 collector scheduling 의미를 가진 subject, kind, priority, poll interval, enabled만 포함하며 heartbeat용 `valid_until`과 진단용 reason은 제외한다.
3. 기존 `CURRENT` generation을 lock한다.
4. row count와 hash가 기존 current와 같으면 새 generation을 만들지 않는다. current generation과 target row의 `valid_until`을 연장하고, reason tuple이 달라졌다면 해당 generation의 진단용 reason row만 같은 transaction에서 교체한 뒤 commit한다. 이 heartbeat는 collector-facing content identity와 generation을 바꾸지 않는다.
5. row count 또는 hash가 다르면 새 generation을 `STAGING`으로 insert한다.
6. target과 reason을 bulk insert한다.
7. insert row count와 generation row count가 같고 projection hash가 재계산 결과와 같은지 확인한다.
8. 기존 `CURRENT`를 `RETIRED`, 새 generation을 `CURRENT`로 한 transaction에서 전환한다.
9. 성공 후에만 in-memory cache와 metrics를 갱신한다.

5초 refresh는 freshness heartbeat이지 generation clock이 아니다. 동일 projection이 반복된다는 이유만으로 in-flight fetch를 stale 처리하지 않는다. reason-only 변경도 collector fence 입력이 아니므로 generation을 회전시키지 않는다.

정상적으로 계산된 빈 target set은 유효하다. input load 실패로 얻은 빈 slice와 구분하기 위해 builder는 error를 반환해야 하며, 실패 시 current generation은 유지된다.

### 7.4 Stale·disabled 계약

- current generation은 `valid_until` 전까지만 acquisition에 사용한다.
- refresh 실패 시 last-good generation을 `valid_until`까지 유지한다.
- `valid_until` 이후 collector는 새 job을 획득하지 않는다.
- target disable은 새 projection generation 활성화로 표현한다.
- 이전 generation에서 시작한 in-flight fetch는 publish transaction에서 current generation mismatch로 거부된다.
- stale projection은 YouTube plane health를 `degraded`로 만들지만 bot/admin/llm global readiness를 자동 실패시켜 restart loop를 만들지 않는다.

## 8. PostgreSQL monotonically fenced collection lease

### 8.1 Correctness boundary

Valkey coordination은 중복 acquisition과 외부 호출을 줄이는 최적화로만 사용한다. stale holder publish를 막는 correctness fence는 PostgreSQL이 소유한다.

```sql
CREATE TABLE youtube_collection_job_leases (
    job_key TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    job_class TEXT NOT NULL CHECK (job_class IN ('GLOBAL', 'SUBJECT')),
    collection_job_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    projection_generation BIGINT NOT NULL,
    poll_interval_ms BIGINT NOT NULL
        CHECK (poll_interval_ms BETWEEN 1000 AND 86400000),
    slot_state TEXT NOT NULL DEFAULT 'IDLE'
        CHECK (slot_state IN ('IDLE', 'ACTIVE', 'DEFERRED')),
    scheduled_for TIMESTAMPTZ NOT NULL,
    next_due_at TIMESTAMPTZ NOT NULL,
    retry_not_before TIMESTAMPTZ,
    fence_epoch BIGINT NOT NULL DEFAULT 0 CHECK (fence_epoch >= 0),
    owner_instance TEXT,
    lease_expires_at TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ,
    last_error_code TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_collection_job_identity CHECK (
        length(collection_job_kind) BETWEEN 1 AND 128
        AND length(subject_key) BETWEEN 1 AND 256
    ),
    CONSTRAINT chk_youtube_collection_job_slot_shape CHECK (
        (slot_state = 'IDLE'
            AND owner_instance IS NULL
            AND lease_expires_at IS NULL
            AND retry_not_before IS NULL)
        OR
        (slot_state = 'ACTIVE'
            AND owner_instance IS NOT NULL
            AND lease_expires_at IS NOT NULL
            AND retry_not_before IS NULL)
        OR
        (slot_state = 'DEFERRED'
            AND owner_instance IS NULL
            AND lease_expires_at IS NULL
            AND retry_not_before IS NOT NULL)
    )
);

CREATE INDEX idx_youtube_collection_job_due
    ON youtube_collection_job_leases (
        slot_state,
        next_due_at,
        retry_not_before,
        lease_expires_at,
        job_key
    );
```

collection job과 observation kind는 같은 개념이 아니다. 한 external fetch가 stats/profile/photo처럼 여러 observation kind를 만들 수 있으므로 lease row는 `collection_job_kind`를 소유하고 단일 `observation_kind`를 강제하지 않는다. `job_class=SUBJECT`는 channel 또는 namespaced subject bundle이며, `GLOBAL`도 안정적인 namespaced `subject_key`를 가진다. compile-time job contract가 각 `collection_job_kind`가 발행할 수 있는 provider/kind 집합을 선언한다. due 판단은 query predicate에서 `NOW()`를 사용하고 index에는 volatile expression을 넣지 않는다.

### 8.2 Acquisition

Acquisition은 row lock 안에서 epoch를 반드시 증가시킨다.

```sql
UPDATE youtube_collection_job_leases
SET owner_instance = $2,
    fence_epoch = fence_epoch + 1,
    projection_generation = $3,
    scheduled_for = CASE
        WHEN slot_state = 'IDLE' THEN date_bin(
            poll_interval_ms * INTERVAL '1 millisecond',
            NOW(),
            next_due_at
        )
        ELSE scheduled_for
    END,
    slot_state = 'ACTIVE',
    retry_not_before = NULL,
    lease_expires_at = NOW() + ($4::bigint * INTERVAL '1 millisecond'),
    updated_at = NOW()
WHERE job_key = $1
  AND (
      (slot_state = 'IDLE' AND next_due_at <= NOW())
      OR
      (slot_state = 'DEFERRED' AND retry_not_before <= NOW())
      OR
      (slot_state = 'ACTIVE' AND lease_expires_at <= NOW())
    )
  AND (owner_instance IS NULL OR lease_expires_at <= NOW())
RETURNING job_key,
          collection_job_kind,
          subject_key,
          owner_instance,
          fence_epoch,
          projection_generation,
          poll_interval_ms,
          slot_state,
          scheduled_for,
          lease_expires_at;
```

새 job row 생성과 acquisition을 같은 helper가 소유할 수 있지만, concurrent `INSERT ... ON CONFLICT` 뒤에는 반드시 row lock과 epoch increment를 거쳐야 한다.
acquisition transaction은 UPDATE 전에 current projection과 이 job이 대표하는 target 집합의 enable/validity를 검증한다. caller가 전달한 `$3`만 신뢰해 stale generation을 lease row에 기록하지 않는다.

#### Missed-slot coalescing

장기 중단 뒤 missed slot을 무제한 재생하지 않는다. `IDLE` acquisition에서만 `date_bin`이 `next_due_at`을 origin으로 사용해 DB `NOW()` 이하의 가장 최신 due boundary 하나를 `scheduled_for`로 선택한다. lease-expired takeover와 `DEFERRED` retry는 기존 `scheduled_for`를 유지한다. 성공 또는 known collision completion은 `slot_state=IDLE`, `next_due_at = scheduled_for + poll_interval`로 전진한다. transient provider failure의 `Defer`는 owner/expiry를 clear하고 `slot_state=DEFERRED`, bounded `retry_not_before`를 설정하며 같은 slot identity를 유지한다. shutdown `Release`도 성공으로 간주하지 않고 같은 slot을 bounded jitter 뒤 재획득할 수 있게 한다.

따라서 한 acquisition은 최대 한 slot만 대표하며 AP 복구가 과거 slot 폭주를 만들지 않는다. poll interval 변경은 current projection activation transaction이 다음 acquisition 전에 job row에 반영하며, 이미 acquired된 proof는 projection mismatch로 stale 처리한다.

### 8.3 Renewal과 cancellation

```go
type JobLease interface {
    Proof() contract.LeaseProof
    Renew(ctx context.Context, ttl time.Duration) error
    Complete(ctx context.Context) error
    Defer(ctx context.Context, retryAt time.Time, code string) error
    Release(ctx context.Context) error
}
```

- renewal은 동일 `job_key + owner_instance + fence_epoch`에만 성공한다.
- renewal failure는 in-flight provider request context를 즉시 cancel한다.
- lease TTL은 provider request timeout, normalization, DB publish budget보다 커야 한다.
- renew interval은 TTL의 1/3 이하로 bounded한다.
- detached renew goroutine을 만들지 않는다. collection run owner가 renew loop를 start/join한다.

### 8.4 Publish-time fence predicate

collector publish transaction은 observation insert 전에 lease row를 `FOR UPDATE`하고 다음을 모두 검증한다.

```sql
SELECT collection_job_kind,
       subject_key,
       slot_state,
       fence_epoch,
       owner_instance,
       projection_generation,
       scheduled_for,
       lease_expires_at
FROM youtube_collection_job_leases
WHERE job_key = $1
  AND owner_instance = $2
  AND fence_epoch = $3
  AND projection_generation = $4
  AND scheduled_for = $5
  AND slot_state = 'ACTIVE'
  AND lease_expires_at > NOW()
FOR UPDATE;
```

추가로 current projection generation과 `valid_until`을 같은 transaction에서 확인한다. batch의 **각 observation**은 해당 `(generation, subject_key, observation_kind)` target이 enabled인지 검증한다. compile-time job contract는 `collection_job_kind`가 그 provider/kind를 발행할 수 있는지도 검증한다. global Holodex/Official job도 generation만 확인하고 우회하지 않으며, batch에 포함된 모든 channel/global observation이 current target을 가져야 한다.

이 predicate 때문에 다음 race가 차단된다.

```text
A epoch=1 acquire -> lease expires
B epoch=2 acquire -> B publish
A late publish -> epoch mismatch -> zero write
```

## 9. Migration 144와 index migration target schema

Migration `144`는 production 미적용을 확인한 뒤 아래 table, constraint, seed, grant 구조로 직접 재작성한다. `CREATE INDEX CONCURRENTLY`는 migration runner의 single-statement 규칙에 따라 required index마다 migration `145`부터 `155`까지 하나씩 분리한다. 기존 `source_authority_fences`, `mode`, `parity_status`, `legacy/shadow/authoritative` column과 code path는 만들지 않는다.

### 9.1 Contract generation

```sql
CREATE TABLE observation_contract_generations (
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    current_schema_version SMALLINT NOT NULL CHECK (current_schema_version > 0),
    current_generation BIGINT NOT NULL CHECK (current_generation > 0),
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, observation_kind),
    CONSTRAINT chk_observation_provider_vocab CHECK (
        provider IN ('holodex', 'youtubejs', 'hololive_official')
    ),
    CONSTRAINT chk_observation_kind_vocab CHECK (
        observation_kind IN (
            'community_page',
            'video_list',
            'shorts_list',
            'live_snapshot',
            'viewer_sample',
            'channel_stats',
            'channel_profile',
            'channel_photo',
            'schedule_snapshot'
        )
    )
);
```

collector publish는 current schema version과 generation을 검증한다. generation은 provider authority가 아니라 stale contract binary를 차단하는 fence다.

API claim은 queue row를 current generation만으로 필터링하지 않는다. 이미 발행된 observation은 immutable evidence이므로 consumer가 compile-time `SupportedContractSet`으로 지원 여부를 판단한다. current generation이 증가했다는 이유만으로 이전 generation을 자동 DLQ 처리하면 정상 backlog와 replay 가능성을 잃기 때문이다.

```go
type ContractVersion struct {
    Provider   Provider
    Kind       ObservationKind
    Schema     int16
    Generation int64
}

type SupportedContractSet interface {
    Supports(ContractVersion) bool
}
```

초기 migration `144`는 각 활성 provider/kind를 generation `1`로만 seed한다. 향후 generation 변경은 별도 change wave에서 다음 순서를 지켜야 한다.

1. 새 API binary가 기존 generation과 다음 generation decoder를 모두 지원한다.
2. migration 또는 승인된 internal operation이 current generation을 증가시킨다.
3. 새 collector만 다음 generation을 publish한다.
4. 이전 generation queue가 0이 된 뒤 old decoder를 별도 cleanup change에서 제거한다.

이것은 payload drain 계약이며 provider authority/dual writer 호환층이 아니다. consumer가 지원하지 않는 version만 `unsupported_contract` permanent failure로 DLQ 처리한다.

`live_snapshot` generation `2`는 generation `1`의 identity/status/time 필드에 다음 optional metadata를 추가한다.

- `title`
- `topic_id`
- `thumbnail_url`

generation `1` decoder는 이 필드를 unknown member로 계속 거부한다. generation `2` metadata는 positive evidence일 때만 canonical `youtube_live_sessions`에 반영하며, 빈 후속 관측값은 이미 저장된 metadata를 지우지 않는다. Holodex는 세 필드를, YouTube.js는 upstream에서 확인한 title과 HTTPS thumbnail을 제공한다. API의 supported set은 두 generation을 동시에 유지하고 collector는 DB의 provider/kind별 current generation이 `2`일 때만 새 필드를 발행한다.

### 9.2 Collection checkpoint

```sql
CREATE TABLE source_collection_checkpoints (
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    scope_sha256 TEXT NOT NULL,
    contract_generation BIGINT NOT NULL,
    last_observation_key TEXT NOT NULL,
    last_evidence_sha256 TEXT NOT NULL,
    last_scheduled_for TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ NOT NULL,
    collection_latency_ms BIGINT NOT NULL CHECK (collection_latency_ms >= 0),
    continuity TEXT NOT NULL,
    cursor JSONB,
    last_error_code TEXT,
    last_error_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, observation_kind, subject_key, scope_sha256),
    CONSTRAINT fk_source_checkpoint_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_checkpoint_subject CHECK (
        length(subject_key) BETWEEN 1 AND 256
    ),
    CONSTRAINT chk_source_checkpoint_hashes CHECK (
        scope_sha256 ~ '^[0-9a-f]{64}$'
        AND last_evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_checkpoint_cursor CHECK (
        cursor IS NULL OR jsonb_typeof(cursor) = 'object'
    ),
    CONSTRAINT chk_source_checkpoint_error_shape CHECK (
        (last_error_code IS NULL) = (last_error_at IS NULL)
    )
);
```

checkpoint는 duplicate suppression의 SSOT가 아니다. 같은 content의 다음 `scheduled_for` snapshot도 observation으로 발행한다. checkpoint는 cursor, continuity, last success와 collection health만 소유한다.

### 9.3 Immutable evidence와 queue 분리

```sql
CREATE TABLE source_observations (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    schema_version SMALLINT NOT NULL CHECK (schema_version > 0),
    contract_generation BIGINT NOT NULL CHECK (contract_generation > 0),
    scheduled_for TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    source_event_at TIMESTAMPTZ,
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    scope_sha256 TEXT NOT NULL,
    completeness TEXT NOT NULL,
    continuity TEXT NOT NULL,
    payload JSONB NOT NULL,
    payload_sha256 TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    collector_instance TEXT NOT NULL,
    job_key TEXT NOT NULL,
    collection_job_kind TEXT NOT NULL,
    fence_epoch BIGINT NOT NULL CHECK (fence_epoch > 0),
    projection_generation BIGINT NOT NULL CHECK (projection_generation > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_observation_identity UNIQUE (
        provider,
        observation_kind,
        subject_key,
        observation_key,
        schema_version,
        contract_generation
    ),
    CONSTRAINT fk_source_observation_contract
        FOREIGN KEY (provider, observation_kind)
        REFERENCES observation_contract_generations(provider, observation_kind)
        ON DELETE RESTRICT,
    CONSTRAINT chk_source_observation_text_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(collector_instance) BETWEEN 1 AND 128
        AND length(job_key) BETWEEN 1 AND 512
        AND length(collection_job_kind) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_observation_payload CHECK (
        jsonb_typeof(payload) = 'object'
        AND octet_length(payload::text) <= 1048576
    ),
    CONSTRAINT chk_source_observation_hashes CHECK (
        scope_sha256 ~ '^[0-9a-f]{64}$'
        AND payload_sha256 ~ '^[0-9a-f]{64}$'
        AND evidence_sha256 ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_observation_completeness CHECK (
        completeness IN ('COMPLETE', 'PARTIAL', 'UNKNOWN')
    ),
    CONSTRAINT chk_source_observation_continuity CHECK (
        continuity IN ('CONTIGUOUS', 'GAP_UNRESOLVED', 'NOT_APPLICABLE')
    )
);

CREATE TABLE source_observation_queue (
    observation_id BIGINT PRIMARY KEY
        REFERENCES source_observations(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'PENDING',
    attempt_count SMALLINT NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 64),
    replay_count SMALLINT NOT NULL DEFAULT 0 CHECK (replay_count BETWEEN 0 AND 16),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_owner TEXT,
    lease_token TEXT,
    lease_expires_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    last_error_code TEXT,
    last_error_detail TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_source_observation_queue_status CHECK (
        status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'DEAD_LETTER')
    ),
    CONSTRAINT chk_source_observation_queue_lease_owner CHECK (
        lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_observation_queue_lease_token CHECK (
        lease_token IS NULL OR lease_token ~ '^[0-9a-f]{64}$'
    ),
    CONSTRAINT chk_source_observation_queue_error_bounds CHECK (
        (last_error_code IS NULL OR length(last_error_code) BETWEEN 1 AND 128)
        AND
        (last_error_detail IS NULL OR length(last_error_detail) <= 2048)
    ),
    CONSTRAINT chk_source_observation_queue_lease_shape CHECK (
        (status = 'PROCESSING'
            AND lease_owner IS NOT NULL
            AND lease_token IS NOT NULL
            AND lease_expires_at IS NOT NULL)
        OR
        (status <> 'PROCESSING'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND lease_expires_at IS NULL)
    ),
    CONSTRAINT chk_source_observation_queue_terminal_shape CHECK (
        (status = 'PROCESSED') = (processed_at IS NOT NULL)
        AND (status = 'DEAD_LETTER') = (dead_lettered_at IS NOT NULL)
    )
);
```

immutable evidence와 mutable delivery state를 분리해 queue retention과 evidence retention을 독립적으로 운영한다.

### 9.4 Collision audit

```sql
CREATE TABLE source_observation_collisions (
    id BIGSERIAL PRIMARY KEY,
    existing_observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    schema_version SMALLINT NOT NULL,
    contract_generation BIGINT NOT NULL,
    existing_evidence_sha256 TEXT NOT NULL,
    attempted_evidence_sha256 TEXT NOT NULL,
    attempted_payload_sha256 TEXT NOT NULL,
    collector_instance TEXT NOT NULL,
    job_key TEXT NOT NULL,
    fence_epoch BIGINT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

collision candidate의 전체 payload를 중복 보관하지 않는다. hash와 provenance만 보관해 PII·payload amplification을 막는다.

### 9.5 Consumer offset, replay, application audit

```sql
CREATE TABLE source_observation_consumer_offsets (
    consumer_name TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    last_processed_id BIGINT NOT NULL DEFAULT 0,
    last_effective_at TIMESTAMPTZ,
    last_processed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer_name, observation_kind)
);

CREATE TABLE source_observation_replay_requests (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    previous_attempt_count SMALLINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'APPLIED', 'REJECTED')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    rejection_code TEXT,
    CONSTRAINT chk_source_observation_replay_terminal_shape CHECK (
        (status = 'APPLIED' AND applied_at IS NOT NULL AND rejection_code IS NULL)
        OR
        (status = 'REJECTED' AND applied_at IS NULL AND rejection_code IS NOT NULL)
        OR
        (status = 'PENDING' AND applied_at IS NULL AND rejection_code IS NULL)
    )
);

CREATE TABLE source_observation_applications (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_key TEXT NOT NULL,
    decision TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_observation_application UNIQUE (
        observation_id,
        entity_kind,
        entity_key
    )
);
```

replay는 source evidence를 수정하지 않는다. replay request와 queue 재활성화를 한 transaction에서 audit한다. public HTTP control plane은 추가하지 않고 existing operator path 또는 내부 CLI owner가 호출하도록 한다.

### 9.6 Reconciliation conflict와 live head

Identity collision과 domain reconciliation conflict는 별도 사건이다. 같은 observation identity의 semantic hash 불일치는 `source_observation_collisions`가, 서로 다른 observation이 같은 entity/effective time에 상충하는 것은 다음 audit가 소유한다.

```sql
CREATE TABLE source_reconciliation_conflicts (
    id BIGSERIAL PRIMARY KEY,
    observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    observation_kind TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    observation_key TEXT NOT NULL,
    evidence_sha256 TEXT NOT NULL,
    entity_kind TEXT NOT NULL,
    entity_key TEXT NOT NULL,
    field_name TEXT NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    existing_value_sha256 TEXT NOT NULL,
    attempted_value_sha256 TEXT NOT NULL,
    decision TEXT NOT NULL
        CHECK (decision IN ('KEEP_EXISTING', 'UNRESOLVED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_reconciliation_conflict UNIQUE (
        observation_id,
        entity_kind,
        entity_key,
        field_name
    ),
    CONSTRAINT chk_source_reconciliation_conflict_bounds CHECK (
        length(subject_key) BETWEEN 1 AND 256
        AND length(observation_key) BETWEEN 1 AND 512
        AND length(entity_kind) BETWEEN 1 AND 64
        AND length(entity_key) BETWEEN 1 AND 256
        AND length(field_name) BETWEEN 1 AND 128
    ),
    CONSTRAINT chk_source_reconciliation_conflict_hashes CHECK (
        evidence_sha256 ~ '^[0-9a-f]{64}$'
        AND existing_value_sha256 ~ '^[0-9a-f]{64}$'
        AND attempted_value_sha256 ~ '^[0-9a-f]{64}$'
    )
);

CREATE TABLE youtube_live_reconciliation_heads (
    video_id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK (status IN ('UPCOMING', 'LIVE', 'ENDED')),
    last_upcoming_positive_at TIMESTAMPTZ,
    last_upcoming_positive_seen_at TIMESTAMPTZ,
    last_live_positive_at TIMESTAMPTZ,
    last_live_positive_seen_at TIMESTAMPTZ,
    last_end_evidence_at TIMESTAMPTZ,
    last_complete_absence_at TIMESTAMPTZ,
    last_absence_scheduled_for TIMESTAMPTZ,
    consecutive_absence_slots SMALLINT NOT NULL DEFAULT 0
        CHECK (consecutive_absence_slots BETWEEN 0 AND 32767),
    end_candidate_kind TEXT
        CHECK (end_candidate_kind IN (
            'EXPLICIT_END',
            'EXPLICIT_CANCEL',
            'SCOPED_ABSENCE'
        )),
    end_candidate_observation_id BIGINT
        REFERENCES source_observations(id) ON DELETE RESTRICT,
    next_end_check_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    end_reason TEXT CHECK (end_reason IN (
        'EXPLICIT_END',
        'CANCELLED_BEFORE_LIVE',
        'SCOPED_ABSENCE'
    )),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_youtube_live_head_candidate_shape CHECK (
        (end_candidate_kind IS NULL
            AND end_candidate_observation_id IS NULL
            AND next_end_check_at IS NULL)
        OR
        (end_candidate_kind IS NOT NULL
            AND end_candidate_observation_id IS NOT NULL
            AND next_end_check_at IS NOT NULL)
    )
);

CREATE INDEX CONCURRENTLY idx_youtube_live_reconciliation_due
    ON youtube_live_reconciliation_heads (next_end_check_at, video_id)
    WHERE next_end_check_at IS NOT NULL;
```

`source_observation_collisions`, `source_observation_replay_requests`, `source_observation_applications`에도 provider/kind vocabulary 또는 contract FK, 64-character lowercase hash, identifier/reason/error text bounds와 terminal shape를 `CREATE TABLE` 안에서 강제한다. Section 17의 bounded-storage 규칙은 Go validation만으로 대체하지 않는다. grant/schema golden test는 이 constraint 이름과 권한을 검증한다.

### 9.7 Required indexes

아래 각 문장은 migration `145`부터 `155`까지 순서대로 독립된 single-statement migration으로 적용한다. 앞선 section의 projection current, collection job due, live reconciliation due index를 포함해 한 migration 파일에 둘 이상의 index를 넣지 않는다.

```sql
CREATE INDEX CONCURRENTLY idx_source_observation_queue_claim
    ON source_observation_queue (available_at, observation_id)
    WHERE status = 'PENDING';

CREATE INDEX CONCURRENTLY idx_source_observation_queue_lease_recovery
    ON source_observation_queue (lease_expires_at, observation_id)
    WHERE status = 'PROCESSING';

CREATE INDEX CONCURRENTLY idx_source_observation_queue_terminal_retention
    ON source_observation_queue (status, updated_at, observation_id)
    WHERE status IN ('PROCESSED', 'DEAD_LETTER');

CREATE INDEX CONCURRENTLY idx_source_observations_subject_time
    ON source_observations (
        observation_kind,
        subject_key,
        scheduled_for DESC,
        id DESC
    );

CREATE INDEX CONCURRENTLY idx_source_observations_received
    ON source_observations (received_at, id);

CREATE INDEX CONCURRENTLY idx_source_observations_kind_id
    ON source_observations (observation_kind, id);

CREATE INDEX CONCURRENTLY idx_source_observation_collisions_occurred
    ON source_observation_collisions (occurred_at, id);

CREATE INDEX CONCURRENTLY idx_source_observation_replay_pending
    ON source_observation_replay_requests (requested_at, id)
    WHERE status = 'PENDING';
```

### 9.8 Grants

`hololive_scraper` 최소 권한은 다음과 같다.

- `SELECT`: current contract generation, current target generation과 targets
- `SELECT, INSERT, UPDATE`: collection checkpoint, DB job lease
- `INSERT, SELECT`: immutable observations와 queue insert 결과 확인
- `INSERT`: collision audit
- 필요한 sequence `USAGE, SELECT`
- canonical table, notification outbox, replay request, queue claim/finalize UPDATE 권한 없음

`hololive_runtime`은 target generation write, observation read, queue claim/finalize, replay/retention, canonical write 권한을 가진다. `alarm-worker` role에는 observation DML 권한을 추가하지 않는다.

migration은 role이 없을 때 NOTICE로 skip하되 schema golden과 grant test가 role 존재 환경에서 정확한 권한을 검증해야 한다.

## 10. Publish repository contract

### 10.1 API

```go
type PublishBatchInput struct {
    Lease       contract.LeaseProof
    Checkpoint  CheckpointUpdate
    Observations []contract.Envelope
}

type PublishOutcome string

const (
    PublishInserted  PublishOutcome = "INSERTED"
    PublishDuplicate PublishOutcome = "DUPLICATE"
    PublishCollision PublishOutcome = "COLLISION"
)

type PublishedObservation struct {
    ObservationID int64
    Outcome       PublishOutcome
}

type PublishBatchResult struct {
    Results []PublishedObservation
}

func (r *Repository) PublishBatch(
    ctx context.Context,
    input PublishBatchInput,
) (PublishBatchResult, error)
```

같은 external fetch가 `channel_stats`, `channel_profile`, `channel_photo` 세 payload를 만들면 하나의 `PublishBatch` transaction으로 commit한다. 한 payload가 invalid하면 checkpoint를 포함해 전체 batch를 rollback한다.

### 10.2 Transaction sequence

1. 입력 크기, observation 수, payload size와 hash를 메모리에서 검증한다.
2. transaction을 시작한다.
3. DB job lease row를 lock하고 owner, epoch, expiry, scheduled slot, projection generation을 검증한다.
4. current projection과 target enable 상태를 검증한다.
5. 각 provider/kind의 current schema version·generation을 검증한다.
6. batch의 모든 observation identity를 preflight한다.
7. existing identity가 없으면 `INSERTED` 후보, 같은 `evidence_sha256`이면 `DUPLICATE` 후보로 분류한다.
8. 다른 `evidence_sha256`를 가진 identity는 row 단위로 collision audit에 기록하고 해당 identity의 evidence를 덮어쓰거나 queue에 넣지 않는다.
9. collision row를 격리한 뒤에도 독립적인 non-collision row는 자체 outcome에 따라 immutable observation을 insert하고 queue에 넣을 수 있으며, 각 row의 checkpoint를 upsert하고 `INSERTED`·`DUPLICATE`·`COLLISION` 결과를 ordinal별로 반환한다.
10. 하나라도 collision이면 job row에 bounded error code를 기록하고 다음 정상 slot으로 전진한다. known collision은 audit 보존을 위해 Go error로 rollback시키지 않으며, 같은 transaction 안의 독립 row side effect는 함께 commit한다.
11. collision이 없을 때도 모든 new immutable observation과 queue row를 insert하고 checkpoint를 upsert한다.
12. job lease를 complete해 `slot_state=IDLE`로 전환하고 owner, lease expiry, retry 시각을 clear한 뒤 `next_due_at = scheduled_for + poll_interval`로 전진시킨다.
13. commit한다.

`PublishCollision`은 충돌한 identity에 대한 fail-closed permanent outcome이다. 같은 external fetch가 만든 stats/profile/photo batch에 collision row가 있어도 독립적인 non-collision row를 억제하지 않는다. collided identity는 기존 immutable evidence를 보존하고 queue에 넣지 않으며, collector는 canonical processing을 기대하지 않고 row outcome별 metric과 bounded log를 남기며 같은 candidate를 무한 retry하지 않는다.

### 10.3 Error classes

```go
var (
    ErrInvalidEnvelope     = errors.New("source observation envelope is invalid")
    ErrStaleContract       = errors.New("source observation contract is stale")
    ErrCollectionFenceLost = errors.New("collection job fence was lost")
    ErrProjectionStale     = errors.New("collection projection is stale")
    ErrTargetDisabled      = errors.New("collection target is disabled")
    ErrClaimLost           = errors.New("source observation claim was lost")
)
```

모든 wrapping은 원인을 보존한다.

```go
return PublishBatchResult{}, fmt.Errorf("publish source observation batch: verify job fence: %w", err)
```

## 11. API queue processing contract

### 11.1 Claim

API worker는 `FOR UPDATE SKIP LOCKED`로 bounded batch를 claim한다. attempts exhausted item을 candidate LIMIT 안에서 분류하되, permanent quarantine가 valid item을 굶기지 않도록 claim query 또는 claim loop가 다음 batch로 즉시 진행할 수 있어야 한다.

claim SQL은 `observation_contract_generations.current_generation`과 불일치한다는 이유만으로 row를 quarantine하지 않는다. consumer의 `SupportedContractSet`에 없는 version만 item 처리 단계에서 `unsupported_contract`로 `DEAD_LETTER` 처리한다.

```go
type ClaimOptions struct {
    ConsumerName  string
    LeaseOwner    string
    Kinds         []contract.ObservationKind
    Limit         int
    LeaseDuration time.Duration
}
```

`Kinds`, `Limit`, lease duration은 bounded validate한다.

### 11.2 Item isolation

한 item 오류로 batch의 뒤 item을 lease expiry까지 방치하지 않는다.

```go
func (c *Consumer) ConsumeBatch(ctx context.Context, batch ClaimedBatch) error {
    var errs []error

    for i := range batch.Observations {
        observation := batch.Observations[i]
        if err := c.repo.EnsureClaimBudget(ctx, observation, c.transactionTimeout); err != nil {
            errs = append(errs, fmt.Errorf("consume observation %d: renew claim: %w", observation.ID, err))
            continue
        }
        if err := c.consumeOne(ctx, observation); err != nil {
            errs = append(errs, err)
        }
    }

    return errors.Join(errs...)
}
```

`EnsureClaimBudget`은 남은 lease가 `2 * transaction timeout`보다 짧으면 해당 observation row만 연장한다. renewal이 실패하면 canonical transaction을 시작하지 않는다. `consumeOne`은 반드시 다음 terminal action 중 하나를 시도한다.

- success: canonical write + notification intent + application audit + `PROCESSED`
- transient: rollback 후 `Retry`로 lease 해제와 bounded delay 설정
- permanent malformed/unsupported: `DEAD_LETTER`
- claim lost: 아무 canonical write도 commit되지 않았음을 보장하고 metric만 증가

### 11.3 Finalize transaction

```go
type ReconcileWrite func(context.Context, dbx.Tx, Observation) (ReconcileResult, error)

func (r *Repository) Finalize(
    ctx context.Context,
    claim Claim,
    reconcile ReconcileWrite,
) (ReconcileResult, error)
```

transaction 순서는 다음과 같다.

1. queue row와 observation row를 lock한다.
2. processing lease token과 expiry를 검증한다.
3. compile-time `SupportedContractSet`의 schema/generation 지원 여부를 검증한다. publish current 여부와 이미 발행된 evidence의 소비 가능 여부를 혼동하지 않는다.
4. typed payload를 strict decode한다.
5. canonical/provenance row를 필요한 최소 범위로 lock한다.
6. reducer를 실행한다.
7. canonical state와 notification intent를 멱등 key로 write한다.
8. `source_observation_applications`를 insert한다.
9. queue를 `PROCESSED`로 전환한다.
10. consumer offset을 upsert한다.
11. commit한다.
12. AfterCommit metric/log만 실행한다. 필수 correctness side effect를 AfterCommit에 두지 않는다.

## 12. Retention과 replay

### 12.1 Lifetime 분리

- `source_observation_queue`: handoff/retry 상태. terminal retention이 가장 짧다.
- `source_observations`: immutable raw evidence. kind별 replay·audit 기간을 보존한다.
- canonical tables와 `source_observation_applications`: 제품 상태와 결정 provenance를 보존한다.
- collision audit와 replay audit: 운영 조사 기간을 보존한다.

초기 implementation에서 retention duration을 SQL에 임의 hardcode하지 않는다. 기존 producer의 동등 데이터 보존 기간을 조사해 settings contract로 옮긴다. 현재 policy가 없는 kind는 production apply 전에 명시적인 bounded duration과 storage estimate를 승인받아야 한다.

### 12.2 Bounded retention worker

```go
type RetentionConfig struct {
    QueueProcessedAge time.Duration
    QueueDLQAge       time.Duration
    EvidenceAgeByKind map[contract.ObservationKind]time.Duration
    CollisionAge     time.Duration
    ReplayAuditAge   time.Duration
    BatchSize        int
    Interval         time.Duration
}
```

- `BatchSize`는 1..1000 범위로 validate한다.
- 한 tick에서 table별 최대 한 batch만 삭제해 DB burst를 제한한다.
- pending replay와 active queue가 있는 evidence는 삭제하지 않는다.
- `youtube_live_reconciliation_heads.end_candidate_observation_id`가 참조하는 active end candidate evidence는 finalizer가 적용하거나 candidate를 clear하기 전까지 삭제하지 않는다.
- terminal replay/collision/application audit는 identity와 hash를 자체 보존하므로 evidence 삭제 뒤에도 조사 가능해야 한다.
- FK가 `SET NULL`인 audit row는 identity/hash를 자체 보존한 뒤 evidence 삭제를 허용한다. FK가 `RESTRICT`인 row가 남아 있으면 audit retention을 먼저 적용한다.
- queue terminal row를 먼저 정리하고 active/replay queue가 없는 evidence만 정리한다.
- deletion count, duration, error와 backlog age를 metric으로 노출한다.

### 12.3 Replay

replay는 terminal queue row를 무조건 덮어쓰지 않는다.

1. operator request를 audit insert한다.
2. observation schema/generation이 현재 consumer에서 지원되는지 확인한다.
3. 현재 queue가 `PROCESSING`이면 reject한다.
4. queue row가 `PROCESSED` 또는 `DEAD_LETTER`이면 이전 attempt count를 replay request에 기록하고 `attempt_count=0`, terminal timestamps/error/lease fields를 clear한 뒤 `replay_count+1`, `PENDING`으로 전환한다.
5. queue row가 terminal retention으로 이미 삭제됐으면 같은 transaction에서 새 queue row를 insert하고, 이전 applied replay request 수로 `replay_count`를 복원한다. immutable observation을 복제하지 않는다.
6. canonical writes, `source_observation_applications`, notification intent의 기존 unique identity가 duplicate side effect를 막아야 한다. 이미 적용된 observation은 idempotent no-op application으로 `PROCESSED`가 될 수 있다.
7. replay 완료 또는 reject를 request row에 기록한다.

## 13. Source-neutral reconciliation

### 13.1 공통 reducer 규칙

모든 reducer는 가능하면 pure function으로 분리한다.

```go
type Reducer[S any, E any] interface {
    Reduce(current S, evidence E, now time.Time) (Decision[S], error)
}
```

- provider 이름으로 branch하지 않는다.
- effective time, scope relation, validity, positive/negative semantics만 사용한다.
- equal-time conflict는 기존 canonical 유지 + conflict audit다.
- arrival order permutation test에서 state와 notification intent가 같아야 한다.
- reducer는 external client나 DB를 호출하지 않는다.

### 13.2 Community, videos, shorts

Canonical identity는 upstream YouTube post/video ID다.

State는 최소 다음 evidence clocks를 보존한다.

```go
type ContentEvidenceClock struct {
    LastPositiveEffectiveAt time.Time
    LastPositiveReceivedAt  time.Time
    LastNegativeEffectiveAt *time.Time
    MissingSinceEffectiveAt *time.Time
}
```

규칙은 다음과 같다.

1. valid positive entity는 `LastPositiveEffectiveAt`보다 새롭거나 같고 값이 일치하면 upsert한다.
2. equal-time conflicting positive는 canonical을 바꾸지 않고 conflict를 기록한다.
3. absence는 complete+contiguous scope가 entity를 cover하고 `negative_effective_at > last_positive_effective_at`일 때만 후보가 된다.
4. 첫 absence는 `missing_since`만 기록한다.
5. tombstone/withdrawn 전환은 기존 제품 계약의 더 엄격한 규칙을 보존한다. 기존 규칙이 없다면 서로 다른 `scheduled_for` slot에서 나온 complete-negative 최소 2개와 DB `received_at` 기반 grace를 모두 요구한다. 동일 observation replay는 횟수에 포함하지 않는다.
6. 더 최신 또는 같은 effective time의 positive는 missing candidate를 지운다.
7. `POSITIVE_ONLY` kind는 `missing_since`도 갱신하지 않는다.
8. 이미 생성된 notification intent를 list absence만으로 삭제하지 않는다.

`video_list`의 `is_premiere=true`는 이미 확정된 최초공개(Premiere) positive fact이며, 같은 observation finalize transaction에서 기존 live projection에도 병합한다. 이 교차 projection은 다음 경계를 따른다.

- `youtube_live_sessions.is_premiere`가 live alarm이 읽는 단일 canonical classification이다. `NULL`은 미확정, `true`와 `false`는 각각 확정 최초공개와 확정 일반 방송이다.
- content canonical write와 notification intent를 저장한 뒤 content subject lock에서 같은 channel의 live subject lock 순서로 진입하고, 해당 video ID row만 `FOR UPDATE`로 읽는다. 반대 lock 순서나 별도 writer를 추가하지 않는다.
- live row가 없으면 `UPCOMING`, content channel/title/scheduled time, observation `received_at`, `is_premiere=true`로 생성하되 live reconciliation head는 만들지 않는다. 조회 뒤 다른 writer가 row를 먼저 생성한 conflict에서는 content write mode가 기존 field를 모두 보존하고 classification만 병합한다.
- 기존 row는 `is_premiere=NULL`일 때만 `true`로 채운다. status, metadata, scheduled/started/ended time, `live_first_seen_at`, `last_seen_at`과 live head는 content evidence로 변경하지 않는다.
- 기존 `true`는 replay no-op이고, 기존 `false`와 새 `true`는 기존 값을 유지하면서 `source_reconciliation_conflicts`에 `is_premiere`/`KEEP_EXISTING`을 기록한다. 필드 부재와 `false` content는 live row를 만들거나 변경하지 않는다.
- live snapshot, schedule merge와 due-finalizer가 사용하는 공용 live-session upsert는 `COALESCE(existing.is_premiere, incoming.is_premiere)`로 최초 non-`NULL`을 보존한다. 추가 YouTube 조회, 새 schema/table, alarm-side join이나 renderer 변경은 이 병합에 포함하지 않는다.

#### 최초공개 알림 소유권

`DEC-20260830-hololive-premiere-content-owned-notifications`가 live alarm의 읽기 경로를 연다. `youtube_live_sessions.is_premiere=true`는 구독자 live upcoming과 live catchup 후보에서 제외하는 분류다. 구독자 최초공개 알림은 `NEW_VIDEO` outbox가 소유한다. content→live premiere 병합은 계속 새 schema/table을 만들지 않으며, 그 병합 범위에는 alarm-side join을 넣지 않는다는 기존 경계를 유지한다. live alarm은 이번 틱의 video_id 집합으로 확정 최초공개 여부를 직접 읽고, `LoadRecentSessions`의 `last_seen` 창에 의존하지 않는다.

### 13.3 Live state

#### LIVE 시작 증거 입장 정책

`LIVE` status는 session이 목록에 존재한다는 사실과 실제 방송 시작을 구분해 입장시킨다.
source-observation adapter는 provider별 upstream 의미를 source-neutral
`LiveStartConfirmed` fact로 정규화하고 reducer는 provider 이름을 보지 않는다.

- YouTube.js가 직접 확인한 `LIVE` status는 시작 확정 증거다.
- Holodex `LIVE`는 `start_actual`이 함께 있을 때만 시작 확정 증거다. Holodex API가
  `status=live`와 nullable `start_actual`을 함께 허용하므로 `start_actual=NULL`인 행은
  session 존재 증거지만 시작 증거는 아니다.
- 미확정 `LIVE`는 새 session을 만들 때 `UPCOMING`으로 보존하고 기존 session에서는
  status, `started_at`, `live_first_seen_at`과 live-positive clock을 전진시키지 않는다.
  Metadata, `last_seen_at`, presence와 더 최신 positive가 해제하는 end candidate는 정상
  positive와 같이 보존한다.
- 이미 `LIVE` 또는 `ENDED`인 session은 미확정 `LIVE` 때문에 역행하지 않는다. 이후
  YouTube.js `LIVE` 또는 `start_actual`이 있는 Holodex `LIVE`가 오면 기존 단조 전이를
  그대로 수행한다.
- 미확정 입장은 `LIVE_START_UNCONFIRMED` application decision으로 기록한다. 새 조회,
  provider 재시도, timer, schema/table, renderer 분기 또는 alarm-side join을 추가하지 않는다.

2026-08-29 운영 24시간 표본에서 Holodex `LIVE`/`start_actual=NULL`은 28개 video, 83개
fact였다. 정상 27개는 YouTube.js `LIVE`와 Holodex non-NULL `start_actual`로 모두 후속
확인됐고 확인 지연은 중앙값 85초, 최대 537초였다. 나머지 1개는 두 provider가
`UPCOMING`으로 정정한 대기방이었으므로 단일 nullable Holodex 신호를 즉시 시작으로
승격하는 기존 동작은 실제 오탐과 독립적인 정상 표본 모두에 비추어 안전하지 않다.

상태는 video/session identity별로 단조 전진한다.

```text
UPCOMING -> LIVE -> ENDED
```

필수 저장 clock은 다음과 같다.

```go
type LiveEvidenceClock struct {
    LastUpcomingPositiveAt      *time.Time
    LastUpcomingPositiveSeenAt  *time.Time
    LastLivePositiveAt          *time.Time
    LastLivePositiveSeenAt  *time.Time
    LastEndEvidenceAt       *time.Time
    LastCompleteAbsenceAt   *time.Time
    ConsecutiveAbsenceSlots int
    EndCandidateKind        *EndEvidenceKind
    EndCandidateObservationID *int64
    NextEndCheckAt          *time.Time
    EndedAt                 *time.Time
}
```

규칙은 다음과 같다.

- 어느 provider의 valid positive라도 `UPCOMING` 또는 `LIVE`로 전진시킬 수 있다.
- equal effective time의 positive와 end/absence가 충돌하면 positive가 이긴다.
- explicit end와 complete scoped absence는 둘 다 end evidence로 정규화하되 provenance를 보존한다.
- `ENDED` 조건은 모두 만족해야 한다.

```go
type EndEvidenceKind string

const (
    EndEvidenceExplicitEnd    EndEvidenceKind = "EXPLICIT_END"
    EndEvidenceExplicitCancel EndEvidenceKind = "EXPLICIT_CANCEL"
    EndEvidenceScopedAbsence  EndEvidenceKind = "SCOPED_ABSENCE"
)

type EndEvidence struct {
    Kind                 EndEvidenceKind
    EffectiveAt          time.Time
    Valid                bool
    EntityMatchesSession bool
    NegativeEligible     bool
    ScopeCoversSession   bool
    HasPositiveAtOrAfter bool
}

func CanEnd(state LiveEvidenceClock, evidence EndEvidence, dbNow time.Time, grace time.Duration) bool {
    if !evidence.Valid || !evidence.EntityMatchesSession || evidence.HasPositiveAtOrAfter {
        return false
    }

    switch evidence.Kind {
    case EndEvidenceExplicitEnd:
        if state.LastLivePositiveAt == nil || state.LastLivePositiveSeenAt == nil {
            return false
        }
        return evidence.EffectiveAt.After(*state.LastLivePositiveAt) &&
            !dbNow.Before(state.LastLivePositiveSeenAt.Add(grace))

    case EndEvidenceExplicitCancel:
        if state.LastLivePositiveAt != nil ||
            state.LastUpcomingPositiveAt == nil ||
            state.LastUpcomingPositiveSeenAt == nil {
            return false
        }
        return evidence.EffectiveAt.After(*state.LastUpcomingPositiveAt) &&
            !dbNow.Before(state.LastUpcomingPositiveSeenAt.Add(grace))

    case EndEvidenceScopedAbsence:
        if !evidence.NegativeEligible ||
            !evidence.ScopeCoversSession ||
            state.LastLivePositiveAt == nil ||
            state.LastLivePositiveSeenAt == nil {
            return false
        }
        return evidence.EffectiveAt.After(*state.LastLivePositiveAt) &&
            !dbNow.Before(state.LastLivePositiveSeenAt.Add(grace)) &&
            state.ConsecutiveAbsenceSlots >= 2

    default:
        return false
    }
}
```

ordering은 effective time을 사용하고 grace elapsed는 DB `received_at` 기반 positive-seen clock을 사용한다. AP clock skew가 end grace를 단축하지 못한다. explicit upstream end fact는 absence capability를 요구하지 않지만 entity match, freshness와 grace를 통과해야 한다. scoped absence만 `NegativeEligible`, scope coverage와 서로 다른 slot의 연속 complete-negative 2개를 요구한다.

LIVE가 된 적 없는 UPCOMING session은 scoped absence만으로 종료하지 않는다. typed explicit cancellation이 마지막 upcoming positive보다 새롭고 grace를 통과하면 canonical status를 `ENDED`로 전진시키고 `end_reason=CANCELLED_BEFORE_LIVE`를 기록한다. 별도 public status vocabulary를 추가하지 않는다.

이미 `ENDED`인 같은 session은 late evidence로 `LIVE`로 되돌리지 않는다. 더 최신 방송은 별도 session identity로 생성한다.

#### DB due-finalizer

end evidence가 먼저 도착하고 grace만 남은 경우, 이후 observation이 없어도 transition이 진행되어야 한다. reducer는 조건이 아직 이르지만 candidate가 유효하면 `NextEndCheckAt`을 저장한다.

API YouTube plane은 다음 contract의 bounded finalizer를 소유한다.

```sql
SELECT video_id
FROM youtube_live_reconciliation_heads
WHERE next_end_check_at IS NOT NULL
  AND next_end_check_at <= NOW()
ORDER BY next_end_check_at, video_id
LIMIT $1
FOR UPDATE SKIP LOCKED;
```

각 row는 별도 transaction에서 다음을 수행한다.

1. current live head와 candidate observation을 lock한다.
2. candidate 이후 더 최신 positive가 없는지 다시 확인한다.
3. typed explicit end/cancel 또는 서로 다른 slot의 scoped absence 2개 조건을 다시 확인한다.
4. DB `NOW()`가 grace를 지났는지 확인한다.
5. 조건이 유지되면 canonical `ENDED`, notification intent, reconciliation head update를 한 transaction에서 commit한다.
6. 조건이 깨지면 candidate와 `next_end_check_at`을 clear하거나 다음 안전 시각으로 이동한다.

finalizer는 in-memory session별 timer를 만들지 않으며 batch size, poll interval, transaction timeout이 bounded다. finalizer transaction rollback은 canonical state와 notification intent를 모두 rollback한다.

### 13.4 Viewer sample

`viewer_sample` identity는 `(video_id, normalized_sample_window_start, provider)` evidence를 보존한다. canonical sample projection은 다음 규칙을 따른다.

- count는 non-negative이며 hidden/unavailable은 explicit nullable state로 표현한다.
- 더 최신 sample window가 이전 canonical sample을 전진시킨다.
- 같은 window의 동일 값은 replay다.
- 같은 window의 서로 다른 값은 provider precedence로 선택하지 않는다. raw sample과 conflict audit를 모두 보존하고 해당 window를 `UNRESOLVED`로 표시한다. 마지막 resolved canonical sample은 전진시키지 않는다.
- 두 provider overlap을 enable하기 전에 위 equal-window conflict behavior를 test로 고정한다.

초기 rollout에서 한 provider만 viewer sample capability를 갖는다면 capability matrix에 명시하며, 이를 primary source 개념으로 일반화하지 않는다.

### 13.5 Channel stats

`ChannelStatsV1`은 다음 형태를 가진다.

```go
type ChannelStatsV1 struct {
    ChannelID       string  `json:"channel_id"`
    SubscriberCount *int64  `json:"subscriber_count,omitempty"`
    ViewCount       *int64  `json:"view_count,omitempty"`
    VideoCount      *int64  `json:"video_count,omitempty"`
    Coverage        ChannelStatsCoverageV1 `json:"coverage"`
}
```

- hidden count는 zero로 변환하지 않고 nil로 보존한다.
- count 감소를 일반적으로 거부하지 않는다. platform correction이 가능하므로 latest valid effective time을 사용한다.
- equal-time conflicting stats는 canonical을 덮어쓰지 않고 conflict를 기록한다.
- raw sample row는 `scheduled_for`별로 보존한다.
- 기존 `youtube_channel_stats_snapshots`와 latest projection의 의미와 query contract를 보존한 뒤 producer writer를 삭제한다.

### 13.6 Channel profile

fetch 결과가 stats와 profile을 함께 제공하더라도 payload kind는 분리한다.

```go
type FieldValue[T any] struct {
    Present bool `json:"present"`
    Value   T    `json:"value"`
}

type ChannelProfileV1 struct {
    ChannelID   string             `json:"channel_id"`
    Handle      FieldValue[string] `json:"handle"`
    Description FieldValue[string] `json:"description"`
    Country     FieldValue[string] `json:"country"`
    JoinedDate  FieldValue[string] `json:"joined_date"`
    Coverage    ChannelProfileCoverageV1 `json:"coverage"`
}
```

`Present=false`는 provider가 그 필드를 관측하지 않았다는 뜻이며 clear가 아니다. `Present=true, Value=""`만 explicit empty 후보다.

필드 reducer 규칙:

| 필드 | validity | 새 값 적용 | clear | equal-time conflict |
|---|---|---|---|---|
| handle | trim, bounded, platform 형식 | newer valid | explicit empty를 허용하지 않음 | 기존 유지 + conflict |
| description | bounded UTF-8 | newer valid | 같은 empty가 연속 complete observation에서 안정화된 뒤 | 기존 유지 + conflict |
| country | normalized bounded code/value | newer valid | explicit empty 안정화 후 | 기존 유지 + conflict |
| joined date | parseable date | 최초 valid 또는 동일 값 | clear 금지 | 기존 유지 + conflict |

stability duration과 consecutive count는 settings로 bounded하며 provider 이름별 우선순위를 두지 않는다. Task 7은 기존 제품 규칙을 inventory한 뒤 `ProfileClearMinObservations`, `ProfileClearStability`, `PhotoChangeMinObservations`, `PhotoChangeStability`의 초기값과 validation 범위를 같은 reviewable change에 고정해야 한다. 이 값이 승인되지 않은 상태에서는 raw variant만 저장하고 profile clear 또는 canonical photo change를 enable하지 않는다.

### 13.7 Channel photo

```go
type PhotoVariantV1 struct {
    URL                string `json:"url"`
    Width              int    `json:"width"`
    Height             int    `json:"height"`
    StableMediaID      string `json:"stable_media_id,omitempty"`
    ContentFingerprint string `json:"content_fingerprint,omitempty"`
}
```

- URL은 HTTPS, bounded length, valid host를 검증한다.
- provider adapter만 provider-specific ephemeral query parameter를 제거할 수 있다. reconciler가 임의 query stripping을 하지 않는다.
- `StableMediaID`와 `ContentFingerprint` 중 하나 이상이 있어야 canonical change 후보가 된다. 둘 다 없으면 raw URL variant만 보존하고 canonical을 변경하지 않는다.
- canonical identity는 `StableMediaID`가 있으면 이를 사용하고, 없으면 provider response metadata에서 검증 가능한 content fingerprint를 사용한다.
- fingerprint 생성을 위해 collector가 이미지를 추가 다운로드하는 것은 V1 비범위다. 별도 유료/대역폭/SSRF 검토 없이 URL을 따라가거나 unbounded media fetch를 추가하지 않는다.
- 같은 content identity의 resolution/URL variant는 raw provenance만 갱신하고 change event를 만들지 않는다.
- 다른 identity는 complete observation에서 설정된 안정화 횟수/시간을 만족한 뒤 canonical photo를 변경한다.
- equal-time conflicting identity는 canonical을 유지하고 conflict를 기록한다.

### 13.8 Schedule

schedule entity는 YouTube video ID가 있으면 이를 canonical identity로 사용한다. video ID가 없는 external item은 provider-scoped temporary identity로 보존하되 기존 YouTube session과 임의 merge하지 않는다.

Official `isLive`는 schedule payload의 evidence field일 뿐 `LIVE` canonical truth가 아니다. Live reducer가 지원하는 typed fact로 명시적으로 변환된 경우에만 사용한다.

## 14. API YouTube plane runtime and NFR budget

### 14.1 Config contract

초기 default는 현재 `hololive-api`의 bounded plane 구조와 총 PostgreSQL capacity를 해치지 않는 범위에서 시작한다. 실제 성능 개선은 동일 workload 측정 전에는 주장하지 않는다.

```go
type YouTubePlaneConfig struct {
    Enabled bool

    PostgresPoolMinConns int32
    PostgresPoolMaxConns int32

    ConsumerWorkers      int
    DBOperationConcurrency int
    ClaimBatchSize         int
    ClaimLease      time.Duration
    ClaimInterval   time.Duration
    TransactionTimeout time.Duration

    ShutdownTimeout time.Duration
    Retention       RetentionConfig
    TargetProjection TargetProjectionConfig
    LiveEndFinalizer LiveEndFinalizerConfig
}
```

권장 초기 default와 validation 관계는 다음과 같다.

```text
POSTGRES_POOL_MIN_CONNS = 1
POSTGRES_POOL_MAX_CONNS = 4
CONSUMER_WORKERS         = 2
DB_OPERATION_CONCURRENCY = 3
CLAIM_BATCH_SIZE         = 4
CLAIM_LEASE              = 60s
TRANSACTION_TIMEOUT      = 10s
```

다음 invariant를 startup에서 검증한다.

```go
func (c YouTubePlaneConfig) Validate() error {
    if c.PostgresPoolMinConns < 0 || c.PostgresPoolMaxConns <= 0 {
        return errors.New("youtube plane postgres pool bounds are invalid")
    }
    if c.PostgresPoolMinConns > c.PostgresPoolMaxConns {
        return errors.New("youtube plane postgres pool min exceeds max")
    }
    if c.ConsumerWorkers < 1 || c.ConsumerWorkers > 16 {
        return errors.New("youtube plane consumer workers must be between 1 and 16")
    }
    if c.DBOperationConcurrency < 1 ||
        int32(c.DBOperationConcurrency) >= c.PostgresPoolMaxConns {
        return errors.New("youtube plane DB operation concurrency must leave one pool connection reserved")
    }
    if c.ConsumerWorkers > c.DBOperationConcurrency {
        return errors.New("youtube plane consumers exceed the shared DB operation budget")
    }
    if c.ClaimBatchSize < 1 || c.ClaimBatchSize > 100 {
        return errors.New("youtube plane claim batch must be between 1 and 100")
    }
    if c.TransactionTimeout <= 0 {
        return errors.New("youtube plane transaction timeout must be positive")
    }
    minimumLease := time.Duration(c.ClaimBatchSize)*c.TransactionTimeout + 10*time.Second
    if c.ClaimLease < minimumLease {
        return fmt.Errorf(
            "youtube plane claim lease must be at least %s for the configured batch",
            minimumLease,
        )
    }
    return nil
}
```

기존 bot/admin/llm pool과 합산한 process-wide connection budget을 deployment test에서 검증한다. YouTube pool 추가를 이유로 PostgreSQL max connections를 근거 없이 올리지 않는다. consumer claim/finalize, target projection, live due-finalizer, replay와 retention의 모든 DB operation은 capacity `DBOperationConcurrency`인 plane 공용 semaphore를 통과한다. health ping과 shutdown recovery를 위해 최소 한 pool connection을 예약한다.

### 14.2 Lifecycle

API app owner는 다음 순서로 YouTube plane을 관리한다.

Startup:

1. config validate
2. dedicated pgxpool 생성과 ping
3. target projection state 확인
4. worker supervisor 생성
5. plane 공용 DB operation semaphore 생성
6. claim loop, live end due-finalizer, replay worker, retention worker 시작
7. health component 등록

Shutdown:

1. 새 claim 중단
2. worker context cancel
3. 현재 item이 transaction timeout 안에 종료되도록 join
4. 미처리 claim은 explicit retry/release를 시도
5. live end finalizer, retention, replay loop를 join
6. dedicated pool close

worker, ticker, timer, rows, transaction과 connection의 owner가 명확해야 하며 detached goroutine을 남기지 않는다.

### 14.3 Health와 readiness

Global process readiness를 실패시키는 조건:

- invalid YouTube plane config
- startup dedicated pool 연결 실패
- worker supervisor 비정상 종료
- 필수 migration/schema 불일치

Global process readiness를 자동 실패시키지 않고 `degraded`로 노출하는 조건:

- observation pending age 증가
- provider freshness 저하
- target projection이 last-good grace 안에서 stale
- DLQ 또는 collision 증가

이 구분은 deunhealth/autoheal restart가 backlog를 악화시키지 않기 위한 계약이다.

## 15. Collector runtime NFR

### 15.1 Scheduling

- 동일 collector binary를 AP a/b/c/d 위치에 배치한다.
- instance별 provider role을 고정하지 않는다.
- global job key는 provider와 job kind를 모두 포함합니다. Holodex는 `collector:holodex:holodex_live:global`, `collector:holodex:holodex_schedule:global`, `collector:holodex:holodex_metadata:global`, Official은 `collector:hololive_official:official_schedule:global`입니다.
- YouTube.js job은 `collector:youtubejs:<collection_job_kind>:<subject_key>`다.
- `collection_job_kind`는 compile-time emission contract를 가지며, 같은 fetch/due cadence를 공유하는 observation kind만 한 subject bundle로 묶는다. cadence가 다르면 별도 job으로 유지한다.
- 한 instance의 global job과 YouTube.js worker가 공유하는 total concurrency는 bounded semaphore 하나가 소유한다.
- provider별 request budget과 total worker limit을 모두 통과해야 외부 호출을 시작한다.
- local queue는 bounded channel 또는 scheduler-owned bounded set을 사용한다. target 전체를 무제한 goroutine으로 펼치지 않는다.
- YouTube.js channel 목록에서 `UPCOMING`이지만 기계가독 `scheduled_at`이 없는 고유 video ID만 raw `/player`로 순차 보강한다. 한 channel collection의 상세 조회 후보는 최대 32개이며, 목록이 이미 시각을 제공하거나 상태가 `LIVE`/`ENDED`/`CANCELLED`이면 상세 조회하지 않는다.
- convergence 구현 동안 collector는 Go로 유지한다. 8개 YouTube.js kind가 실제 활성화된 뒤 helper RPC call 수·latency·CPU 또는 failure amplification이 동일 workload 측정에서 material bottleneck일 때만 TypeScript collector를 검토한다.
- TypeScript 검토의 선행 조건은 Go와 TypeScript 양쪽이 `source-observation-canonical-json-v1` fixture를 통과하는 것이다. fixture conformance 없이 runtime 언어를 바꾸지 않는다.

### 15.2 Provider failure

- HTTP/client timeout은 context deadline을 보존한다.
- response body는 모든 경로에서 close한다.
- retry budget은 기존 provider policy를 재사용하고 신규 무제한 retry를 만들지 않는다.
- rate limit/429는 bounded retry-after를 존중한다.
- parser drift는 permanent collection error로 metric을 남기고 complete-empty를 publish하지 않는다.
- YouTube.js raw player 보강의 32개 상한 초과, video identity 불일치, 잘못된 boolean/시각 shape 또는 끝까지 시각이 없는 `UPCOMING`은 `parser_drift`다. 해당 collection은 live observation과 checkpoint를 만들지 않으며 다른 provider 호출, 표시 문자열 파싱, partial success로 전환하지 않는다.
- 목록과 player 조회 사이에 실제 `LIVE`로 전환된 행은 예정 시각을 발명하지 않고 `LIVE`로 반영해 기존 catch-up admission에 맡긴다. 처음부터 `LIVE`인 목록 행도 상세 조회 없이 같은 경로를 유지한다.
- circuit cooldown 동안 provider/kind freshness와 skip reason을 구분해 노출한다.

### 15.2.1 YouTube.js raw metadata ownership

Collector 로컬 adapter가 raw `/player`의 `videoDetails.videoId`, `isLive`, `isUpcoming`, `isLiveContent`와 `microformat.playerMicroformatRenderer.liveBroadcastDetails.startTimestamp` 해석을 소유한다. live schedule 보강과 content-owned premiere 판정은 같은 adapter와 sanitized fixture를 사용한다. `youtubei.js@18.0.0`은 Innertube session, request context, browse/transport와 범용 parser 기반층으로 유지하며 upstream 전체 source를 복사하지 않는다.

Dependency upgrade 때는 upstream release note와 로컬 사용 surface만 검토하고 raw fixture, 전체 helper test와 typecheck를 통과시킨다. 사용 field의 parser 변경만 로컬 adapter에 선택적으로 반영한다. raw Actions 접근 제거로 incident 수정이 막히거나 adapter 밖 upstream parser 지연이 독립적으로 두 번 확인되기 전에는 전체 fork나 더 넓은 vendoring을 검토하지 않는다.

### 15.3 AP failover

Valkey 장애가 발생해도 PostgreSQL lease가 correctness를 보존한다. Valkey bypass 시 DB acquisition 빈도를 bounded jitter로 제한해 thundering herd를 방지한다. PostgreSQL lease를 우회하는 in-memory owner fallback은 금지한다.

## 16. Observability contract

최소 metric은 다음과 같다.

```text
youtube_collection_attempts_total{provider,kind,result}
youtube_collection_duration_seconds{provider,kind}
youtube_collection_last_success_timestamp_seconds{provider,kind}
youtube_collection_freshness_seconds{provider,kind}
youtube_collection_completeness_total{provider,kind,completeness,continuity}
youtube_collection_lease_acquire_total{provider,kind,result}
youtube_collection_lease_lost_total{provider,kind,phase}
youtube_observation_publish_total{provider,kind,outcome}
youtube_observation_collision_total{provider,kind}
youtube_observation_clock_skew_total{provider,kind,direction}
youtube_observation_pending_age_seconds{kind}
youtube_observation_claim_duration_seconds{kind}
youtube_observation_processing_duration_seconds{kind,result}
youtube_observation_retry_total{kind,error_code}
youtube_observation_dlq_total{kind,error_code}
youtube_observation_replay_total{kind,result}
youtube_observation_retention_deleted_total{table,kind,status}
youtube_target_projection_generation
youtube_target_projection_age_seconds
youtube_target_projection_rows{kind}
youtube_reconcile_conflict_total{kind,field}
youtube_live_end_due_total{result}
youtube_live_end_due_age_seconds
```

structured log는 `slog`를 사용하고 최소 다음 field를 가진다.

```text
provider
observation_kind
subject_key
observation_id
job_key
fence_epoch
projection_generation
contract_generation
error_code
retryable
```

payload, message body, token, secret와 전체 description을 log하지 않는다. 필요한 경우 hash와 bounded count만 기록한다.

## 17. Security와 resource lifetime

- collector DB role은 최소 권한으로 제한한다.
- raw API key, DB password, certificate를 log 또는 document artifact에 기록하지 않는다.
- URL, channel ID, cursor, group selector, provider payload는 bounded validation한다.
- SQL identifier를 runtime string interpolation하지 않는다.
- external HTTP는 기존 HTTPS/H3/TLS contract를 유지한다.
- response body, DB rows, transaction, acquired connection, timer, ticker, worker와 renew loop는 owner가 close/join한다.
- payload canonicalization은 최대 1 MiB를 넘지 않는다. provider fetch result가 이를 넘으면 partial로 분류하거나 kind contract를 version-up한다.
- collision audit와 error detail은 bounded text만 저장한다.

## 18. 구현 순서

각 task는 정상·실패·경계 경로를 함께 완료해야 다음 task로 진행한다. 각 kind 전환 wave는 대체된 producer fetch/persist registration을 같은 task에서 삭제한다.

### Task 1 — Migration 144와 contract v2.1 재작성

소유 파일:

```text
hololive/hololive-api/scripts/migrations/144_source_observation_outbox.sql
hololive/hololive-api/scripts/migrations/145_source_observation_projection_current_index.sql
hololive/hololive-api/scripts/migrations/146_source_observation_job_due_index.sql
hololive/hololive-api/scripts/migrations/147_source_observation_queue_claim_index.sql
hololive/hololive-api/scripts/migrations/148_source_observation_queue_lease_recovery_index.sql
hololive/hololive-api/scripts/migrations/149_source_observation_queue_terminal_retention_index.sql
hololive/hololive-api/scripts/migrations/150_source_observations_subject_time_index.sql
hololive/hololive-api/scripts/migrations/151_source_observations_received_index.sql
hololive/hololive-api/scripts/migrations/152_source_observations_kind_id_index.sql
hololive/hololive-api/scripts/migrations/153_source_observation_collisions_occurred_index.sql
hololive/hololive-api/scripts/migrations/154_source_observation_replay_pending_index.sql
hololive/hololive-api/scripts/migrations/155_youtube_live_reconciliation_due_index.sql
hololive/hololive-api/scripts/migrations/manifest.txt
hololive/hololive-dbtest/migrations_p1_test.go
hololive/hololive-dbtest/testdata/schema_snapshot.golden.sql
hololive/hololive-shared/pkg/contracts/sourceobservation/*
hololive/hololive-shared/pkg/service/youtube/sourceobservation/*
```

구현:

- provider/kind envelope, scheduled slot identity, typed coverage, payload/evidence hash를 구현한다.
- authority fence, parity, legacy/shadow/authoritative symbol을 제거한다.
- immutable evidence와 queue를 분리한다.
- collision audit, reconciliation conflict, live reconciliation head와 replay/application audit schema를 추가한다.
- 모든 audit table의 text/hash/vocab/FK/terminal-shape bounds를 DDL과 grant/schema golden test로 고정한다.
- direct migration rewrite, single-statement concurrent index migration과 manifest entry를 current manifest 순서에 맞춰 갱신한다.
- old WIP queries를 새 column/transaction contract로 교체한다.

필수 test:

```text
same identity + same evidence -> one observation, one queue row
same identity + different payload -> collision audit, no second observation
same identity + completeness change -> collision audit
same payload + next scheduled slot -> two observations
viewer equal value + next sample window -> two observations
unknown field / malformed payload -> validation reject
stale schema/generation -> publish reject
current generation bump does not strand old supported queue rows
unsupported contract -> item DLQ with bounded audit
source_event_at future skew -> scheduled_for fallback + metric
checkpoint + observation rollback atomicity
migration replay twice
role grants and schema golden
```

검증:

```bash
go test ./hololive/hololive-shared/pkg/contracts/sourceobservation \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation \
  ./hololive/hololive-dbtest/...
```

### Task 2 — Target projection과 PostgreSQL job fence

소유 파일:

```text
hololive/hololive-api/internal/planes/youtube/targetprojection/*
hololive/hololive-youtube-collector/internal/runtime/joblease/*
hololive/hololive-shared/pkg/service/youtube/sourceobservation/*
```

구현:

- generation-based target rebuild를 구현한다.
- target reason과 collector-facing target row를 분리한다.
- DB lease acquire/renew/complete/defer/release를 구현한다.
- publish transaction에 job fence, projection generation, target enabled predicate를 결합한다.
- renewal loss가 fetch context를 cancel하도록 한다.
- Valkey는 optional contention optimization으로만 연결한다.

필수 race test:

```text
A epoch 1 acquire
A fetch blocks
lease expires
B epoch 2 acquire
B publish succeeds
A resumes and publish fails with ErrCollectionFenceLost
checkpoint, queue, observation, next_due_at are unchanged by A
```

추가 test:

```text
projection rebuild input failure preserves current generation
same hash/row count refresh preserves generation and only extends validity
successful empty projection activates zero-row generation
target disable during fetch blocks every emitted observation
multi-kind subject bundle rejects an undeclared or disabled emitted kind
long outage coalesces missed slots to one latest due boundary
lease takeover, Defer와 Release retry preserve the same scheduled_for slot
projection valid_until expiry blocks acquire
renew failure cancels provider request
only one global holder active
YouTube.js channel jobs distribute without duplicate publish
```

검증:

```bash
go test ./hololive/hololive-youtube-collector/internal/runtime/joblease \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime \
  ./hololive/hololive-api/internal/planes/youtube/targetprojection \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

### Task 3 — Three-provider collector adapters

구현:

- Holodex external calls를 collector-owned adapter로 이동한다.
- Official Schedule JSON API adapter를 collector-owned `schedule_snapshot` publisher로 이동한다.
- YouTube.js helper를 fixture가 있는 kind부터 확장한다.
- provider fetch 한 번이 여러 kind를 만들면 `PublishBatch`로 commit한다.
- complete/partial/continuity와 typed coverage를 adapter에서 명시한다.
- parser failure/timeout/cursor gap은 complete-empty를 만들지 않는다.
- metrics와 bounded logs를 추가한다.

필수 fixture test:

```text
input ordering change -> canonical payload/hash unchanged
missing page -> PARTIAL + GAP_UNRESOLVED
empty successful exhausted page -> COMPLETE
POSITIVE_ONLY complete-empty cannot become negative evidence
timeout/parser error -> no observation publish
scope requested IDs sorted/deduplicated
Official isLive remains schedule evidence only
```

검증:

```bash
go test ./hololive/hololive-youtube-collector/internal/runtime/... \
  ./hololive/hololive-shared/pkg/contracts/sourceobservation
(cd hololive/hololive-youtube-collector/youtubejs && npm test)
```

### Task 4 — API YouTube plane과 Community ownership 이전

구현:

- dedicated pool과 lifecycle-owned worker supervisor를 추가한다.
- Community observation claim/reconcile/finalize를 API plane으로 이동한다.
- detection/effective time에 `time.Now()`를 사용하지 않고 observation clocks를 전달한다.
- per-item failure isolation을 구현한다.
- Community canonical write, notification intent, application audit, queue completion을 한 transaction에 둔다.
- producer Community poller, consumer wiring, persist helper registration을 삭제한다.

필수 test:

```text
API shutdown stops claim and joins workers
transaction failure rolls back canonical, notification, application, processed state
one invalid item DLQ while later batch item processes
replay does not duplicate notification intent
same evidence permutations yield same Community artifacts
producer package has no live Community registration/import
```

검증:

```bash
go test ./hololive/hololive-api/internal/planes/youtube/... \
  ./hololive/hololive-shared/pkg/service/youtube/community \
  ./hololive/hololive-shared/pkg/service/youtube/sourceobservation
```

### Task 5 — Videos와 Shorts

구현:

- provider-neutral content reducer를 추출한다.
- typed coverage relation과 negative eligibility를 적용한다.
- canonical YouTube video identity와 notification identity를 보존한다.
- raw provider provenance/application audit를 남긴다.
- producer videos/shorts/backfill registration과 direct writer를 삭제한다.

필수 permutation test:

```text
positive A -> complete negative B
complete negative B -> positive A
partial negative -> positive
late positive after negative
narrow scope negative
one complete-negative slot only records missing candidate
two distinct complete-negative slots plus grace may tombstone
replayed negative does not increment consecutive count
same evidence set의 모든 순열
```

모든 순열은 같은 canonical row, missing state와 notification intent를 만들어야 한다.

### Task 6 — Live, viewer, schedule

구현:

- monotonic live reducer, persisted end candidate와 bounded DB due-finalizer를 구현한다.
- explicit end, complete absence, positive live evidence를 typed fact로 정규화한다.
- viewer sample의 repeated equal value 보존과 equal-window conflict를 구현한다.
- schedule entity merge와 Official evidence boundary를 구현한다.
- birthday/live-session/outbox identity와 recipient behavior를 보존한다.
- Holodex-primary/YouTube-fallback branching과 producer live/viewer/schedule registration을 삭제한다.

필수 test:

```text
UPCOMING -> LIVE -> ENDED normal path
partial/gap/timeout cannot end
complete absence before grace cannot end
one scoped absence after grace still cannot end
two distinct scoped absence slots after grace can end
explicit end after freshness/grace can end without absence capability
explicit cancellation can end never-live UPCOMING after upcoming freshness/grace
scoped absence cannot end never-live UPCOMING
late positive at same/newer effective time prevents end
future-skewed source event falls back to scheduled slot
collector clock skew does not shorten grace
A stale publish after B epoch cannot create end evidence
already ENDED session never returns LIVE
same live evidence permutations converge
alarm-worker remains sole egress owner
```

### Task 7 — Channel stats, profile, photo

구현:

- `channel_stats` V1과 existing snapshot/latest persistence parity를 구현한다.
- profile fields를 stats에서 분리한다.
- `Present` semantics와 field reducer를 구현한다.
- photo content fingerprint, stability, resolution-only no-event rule을 구현한다.
- raw variants와 conflict audit를 보존한다.
- stability settings inventory와 승인 전에는 profile clear와 canonical photo change를 disabled로 유지한다.
- producer stats/profile/photo sync와 registrations를 삭제한다.

필수 test:

```text
subscriber/view/video equal consecutive samples are both retained by scheduled slot
hidden count remains nil, not zero
equal-time conflicting stats do not arrival-order overwrite
profile absent field does not clear
explicit empty requires stability
joined date conflict is rejected/recorded
same photo identity with new URL/resolution creates no change event
photo without stable ID/fingerprint cannot change canonical state
different photo identity requires stability threshold
collector does not fetch media bytes to synthesize fingerprint
provider arrival permutations yield same projection
```

### Task 8 — Retention과 replay

구현:

- current producer/evidence table retention 정책을 inventory한다.
- queue/evidence/collision/replay duration settings를 추가한다.
- bounded cleanup query와 worker를 구현한다.
- replay request, queue reactivation, audit와 duplicate notification protection을 구현한다.

필수 test:

```text
one tick deletes at most batch size
active/pending replay evidence is not deleted
processed and DLQ durations differ
replay of processed observation is idempotent
unsupported old schema replay is rejected with audit
shutdown joins retention and replay workers
```

### Task 9 — `youtube-producer` 완전 제거

삭제 및 수정 범위:

```text
hololive/hololive-youtube-collector/
go.work
.env.example
README.md
CHANGELOG.md
deploy/compose/**
scripts/deploy/**
scripts/architecture/**
scripts/ci/**
internal/workspace/**
.github/**
docs/current/PROJECT_MAP.md
docs/current/SERVICE_OWNERSHIP.md
docs/current/CODEBASE_OVERVIEW.md
docs/current/DEPLOYMENT_BASELINE.md
docs/current/services/**
docs/current/runbooks/**
PGO/perf inventories and source revision provenance fixtures
```

AP a/b/c/d service identity는 `youtube-collector`로 교체하되 host별 port, TLS, DB role, health/readiness와 deploy completion contract를 명시적으로 이전한다.

repository 전체 tracked current surface를 검사한다.

```bash
git grep -nE \
  'hololive-youtube-producer|youtube-producer|AuthorityMode|source_authority_fences|legacy|shadow|authoritative' \
  -- . ':!docs/history/**'
```

historical document와 unrelated API vocabulary는 exact allowlist로만 제외한다. broad directory exclusion을 사용하지 않는다.

### Task 10 — Final verification와 merge-readiness

focused test를 통과한 final state에서 다음을 실행한다.

```bash
./build-all.sh --build-only --no-bump
(cd hololive/hololive-youtube-collector/youtubejs && npm test)
PRE_PUSH_MODE=full ./scripts/ci/pre-push-gate.sh
git diff --check
```

full gate를 임의 command 묶음으로 대체하지 않는다. Stage 3 lint, NilAway, race, architecture/function budget, perf/PGO, workflow/security shell, vulnerability와 tidy drift는 blocking이다.

workflow, CI script, DB access policy 또는 stack-wide contract를 변경했고 stack checkout이 있으면 다음도 실행한다.

```bash
cd /home/kapu/work/iris-stack
bash tools/check-ci-consistency.sh
bash tools/check-stack-db-access-policy.sh
```

## 19. Test matrix

| Surface | 정상 경로 | 실패 경로 | 경계/회귀 |
|---|---|---|---|
| Identity | same-run replay dedup | semantic collision audit | same payload next slot preserved |
| Coverage | complete covered absence | partial/gap rejected | disjoint/narrow scope ignored |
| Fence | current epoch/multi-kind bundle publish | stale epoch rejected | target disabled mid-fetch, missed-slot coalescing |
| Queue | claim/process/finalize | retry/DLQ/claim lost | one item failure isolation |
| Transaction | canonical+notify+processed commit | rollback all | replay no duplicate intent |
| Live | upcoming/live/end + due-finalizer | false end/rollback blocked | late positive, equal-time positive, no-new-observation grace |
| Stats | append snapshot | malformed/hidden values | equal values in consecutive slots |
| Profile | valid newer field | invalid/equal-time conflict | absent vs explicit empty |
| Photo | stable content change | invalid URL/transient | resolution-only no event |
| Projection | atomic changed-generation swap | input load failure | same-hash no churn, valid empty, expiry, re-enable |
| Retention | bounded delete | DB error retry | replay/collision FK protection |
| Lifecycle | start/drain/close | worker/pool failure | cancellation/race/leak |
| Architecture | collector/API/alarm ownership | forbidden import | producer references removed |

DB/concurrency 변경에는 `go test -race`와 실제 lease/transaction invariant test를 모두 포함한다. 성능은 existing benchmark와 `scripts/perf/perf-budget.yaml`의 동일 workload 전후 측정 없이는 개선됐다고 보고하지 않는다.

## 20. NFR gate

| NFR | 구현 계약 | 필수 검증 |
|---|---|---|
| 성능·용량 | dedicated pool, bounded workers/batch/payload, batch publish | pool capacity test, query count, perf budget |
| 동시성·정합성 | PostgreSQL fence epoch, SKIP LOCKED, item isolation, order-independent reducer | lease-loss race, transaction rollback, `go test -race` |
| 복원력 | bounded timeout/retry, last-good projection, replay audit, fail-closed collision | timeout/cancel, stale projection, replay tests |
| 보안 | least-privilege grants, strict payload validation, secret/body redaction | grant/schema test, invalid URL/payload tests |
| 자원 수명 | joined workers/renewers, closed rows/body/pool/timers | shutdown/leak tests |
| 관측성 | provider/kind failure, freshness, lag, collision, saturation 구분 | metric/log contract tests |
| 호환성·운영 | existing canonical/outbox IDs 보존, producer 완전 제거, no compatibility layer | parity tests, architecture scans, rollback doc |
| 비용 | request budget, no duplicate calls, bounded DB/network amplification | request-unit tests, queue/pool metrics |

## 21. Tech debt gate

최종 touched scope에는 다음을 남기지 않는다.

- 미완료 표식, skipped test, dead code
- authority compatibility type/table/path
- producer wrapper binary 또는 package alias
- untyped universal observation payload
- provider precedence branch
- test-only production bypass
- broad lint/race/NilAway/perf/architecture exclusion
- unbounded goroutine, queue, retry, payload
- temporary duplicate canonical writer
- Go `encoding/json`의 암묵적 number spelling, map ordering 또는 HTML escaping에 의존하는 observation identity

기존 범위 밖 부채가 blocking gate에서 드러나면 위치, 실패 command, 영향과 권장 후속 조치를 보고하되 gate를 낮추지 않는다.

## 22. Completion criteria

다음을 모두 만족해야 구현 완료다.

1. provider/kind typed observation contract, Canonical JSON v1 fixture와 migration replay가 통과한다.
2. same payload next collection slot과 viewer equal samples가 보존된다.
3. metadata-only semantic conflict가 collision audit로 fail closed된다.
4. stale collection holder가 PostgreSQL fence로 observation/checkpoint/job state를 변경하지 못한다.
5. target generation refresh 실패·same-hash stability·empty·disable·expiry 계약과 multi-kind per-observation target 검증이 통과한다.
6. source-order permutation, clock-skew와 no-new-observation due-finalizer test가 canonical state/outbox invariance를 증명한다.
7. `channel_stats`, profile, photo의 기존 user-visible behavior가 API plane에서 보존된다.
8. API pool/worker/shutdown/readiness NFR가 검증된다.
9. retention/replay가 bounded하고 auditable하다.
10. standalone `youtube-producer`와 authority compatibility layer가 repository current surface에서 제거된다.
11. `./build-all.sh --no-bump`와 `PRE_PUSH_MODE=full ./scripts/ci/pre-push-gate.sh`가 final state에서 통과한다.
12. production migration/deploy는 별도 승인 전까지 수행되지 않는다.

## 23. 검증 경계

이 문서는 제공된 local snapshot과 그 안에 포함된 code/migration evidence를 바탕으로 작성한 replacement contract다. 2026-08-25 final state에서 migration, fencing, target generation, source-order, channel stats, API plane, retention/replay와 producer retirement 구현을 확인했고 focused Go, Node, PostgreSQL과 producer-retirement 검증을 통과했다. 전체 local CI와 canonical pre-push gate도 통과했으며 active image는 build-only/no-bump mode로 빌드했다.

현재 달성 상태는 다음과 같다.

```text
document current
implementation complete
migration replay verified locally
race/NilAway/perf/security/canonical pre-push verified locally
production operations not authorized
```
