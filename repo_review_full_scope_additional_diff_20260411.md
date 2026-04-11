# Repo-wide review and additional diff blueprint (2026-04-11)

이번 업로드본(`hololive-bot-full-20260411T045041Z.tar.gz`)을 기준으로, 이전에 제가 남긴 잔존 이슈가 실제로 닫혔는지부터 다시 확인했다. 이번 문서는 **이미 해결된 항목을 반복하지 않고**, 현재 트리에서 **실제로 아직 남아 있는 것만** 다시 추린 보강판이다. 범위는 임의로 줄이지 않았다. Go 런타임, `hololive-shared`, `shared-go`, `admin-dashboard`, build/deploy scripts, architecture gates, review bundle/export 경로까지 전체를 다시 훑었다.

## 0. 이번 버전에서 이미 해결된 것으로 판단한 항목

아래는 이번 문서에서 **의도적으로 제외한 항목**이다. 실제 트리에서 이미 정리됐거나, 더 이상 잔존 이슈로 분류할 수준이 아니었다.

- admin-dashboard typed holo client의 upstream 4xx 보존 문제
- frontend `holoClient.ts` wrapper 잔존 문제
- SSR injector/`window.__SSR_DATA__` dead path
- Rust config/startup의 panic 중심 부팅 경로
- `shared-go` / `hololive-shared`의 `envutil` / `logging` 이중화 핵심 문제
- `MemberDataProvider` multi-result stub 문제
- major-event consensus deadline budget TODO
- alarm target minute runtime propagation
- scraper env 반영, worker full 시 `+10s` 임의 지연
- settlement-go 제거 전제 관련 항목

즉, 이번 문서는 "이미 고친 것의 반복 리뷰"가 아니다. **최신 업로드본에도 아직 남아 있는 것만** 다룬다.

---

## 1. 최종 판단

이번 버전은 이전 보강안 대비 분명히 더 좋아졌다. 구조적으로는 거의 마감 직전이다. 다만 지금 상태를 그대로 종료하면, 운영/CI/거버넌스 관점에서 아래 여섯 축이 남는다.

1. `admin-dashboard` OpenAPI export pipeline이 문서·CI·프런트 스크립트와 실제 backend 트리가 아직 맞지 않는다.
2. DB migration 적용 순서가 여전히 파일명/사전순에 암묵적으로 의존하고 있고, duplicate prefix가 unchecked 상태다.
3. architecture multi-language LOC gate가 **현재 트리에서 실제로 실패**한다.
4. review bundle / architecture script가 git checkout이 아닌 번들 환경에서 false-green을 낼 수 있고, 실제 업로드 번들은 현재 export 규칙과 어긋난다.
5. `cache` manual mock의 zero-value default가 여전히 lenient라서 테스트 false positive 여지가 남는다.
6. 얇은 provider/wrapper/alias residue가 일부 남아 있어 AI 흔적이 완전히 사라진 상태는 아니다.

아래부터는 각 항목별로 **왜 문제인지**, **어디를 바꿔야 하는지**, **바로 적용 가능한 diff 수준 코드안**까지 정리한다.

---

## 2. 남은 이슈 #1 — admin-dashboard OpenAPI export pipeline mismatch

### 2-1. 현재 상태

아래 네 군데는 모두 `export-openapi` binary가 존재한다고 가정한다.

- `admin-dashboard/frontend/package.json`
- `.github/workflows/admin-dashboard-frontend.yml`
- `admin-dashboard/docs/openapi-pipeline.md`
- `admin-dashboard/README.md`

그런데 실제 트리에는 `admin-dashboard/backend/src/bin/export-openapi.rs`가 없다.

즉 지금 구조는 이렇게 되어 있다.

- frontend는 `npm run generate:api`에서 `cargo run --bin export-openapi`를 실행하려고 한다.
- CI workflow도 같은 binary를 path filter와 실행 경로에 넣어두고 있다.
- 문서도 그 binary를 SSOT export entrypoint로 적고 있다.
- 하지만 실제 backend source tree에는 그 entrypoint가 없다.

이 상태는 **문서·CI·프런트 명령이 실제 코드를 가리키지 않는 상태**다. typed contract 정리는 이미 많이 되어 있지만, 마지막 export 파이프라인이 닫히지 않았다.

### 2-2. 판단

여기서는 문서/CI를 바꾸는 것이 아니라, **실제 binary를 추가하는 것**이 맞다. 이유는 이미 현재 레포의 의도된 설계가 명확하기 때문이다.

- `backend/src/openapi.rs`가 schema SSOT다.
- `backend/docs/swagger.json`가 committed artifact다.
- frontend generated client는 그 swagger에서만 나와야 한다.
- workflow도 이미 그 모델을 기준으로 짜여 있다.

즉 설계는 맞고, 마지막 조각만 비어 있다.

### 2-3. 적용 diff

#### 새 파일 추가

`admin-dashboard/backend/src/bin/export-openapi.rs`

```rust
use anyhow::Context;
use utoipa::OpenApi;

use admin_dashboard::openapi::ApiDoc;

fn main() -> anyhow::Result<()> {
    let document = ApiDoc::openapi();
    let json = serde_json::to_string_pretty(&document)
        .context("serialize openapi document")?;

    println!("{json}");
    Ok(())
}
```

설명은 단순하다. 이미 `backend/src/lib.rs`가 `pub mod openapi;`를 export하고 있으므로, package library crate인 `admin_dashboard`에서 `ApiDoc`을 그대로 가져오면 된다. 별도 wiring은 필요 없다.

#### 추가 검증용 테스트

`admin-dashboard/backend/tests/export_openapi_smoke.rs`

```rust
use utoipa::OpenApi;

use admin_dashboard::openapi::ApiDoc;

#[test]
fn openapi_document_serializes() {
    let json = serde_json::to_string(&ApiDoc::openapi()).expect("serialize openapi");
    assert!(json.contains("/admin/api/holo/members"));
    assert!(json.contains("/admin/api/holo/alarms"));
}
```

이 테스트는 binary 자체를 실행하지는 않지만, binary가 의존하는 핵심 경로가 끊기지 않았는지 빠르게 검증한다.

#### frontend package.json은 그대로 유지 가능

현재 script는 binary만 생기면 정상 의미가 된다.

```json
"generate:api": "mkdir -p ../backend/docs && (cd ../backend && cargo run --quiet --bin export-openapi > docs/swagger.json) && swagger-typescript-api generate -p ../backend/docs/swagger.json -o src/api/generated --axios --modular"
```

#### workflow는 path filter만 소폭 보강

현재는 `admin-dashboard/backend/src/bin/export-openapi.rs`를 already reference하고 있으므로, 파일이 생기면 자연스럽게 닫힌다. 여기에 아래 줄만 하나 추가하면 drift 감지가 더 안정적이다.

`.github/workflows/admin-dashboard-frontend.yml`

```diff
   pull_request:
     paths:
       - 'admin-dashboard/frontend/**'
       - 'admin-dashboard/backend/src/**'
+      - 'admin-dashboard/backend/tests/export_openapi_smoke.rs'
       - 'admin-dashboard/backend/src/bin/export-openapi.rs'
       - 'admin-dashboard/backend/docs/swagger.json'
       - 'admin-dashboard/backend/Cargo.toml'
       - 'admin-dashboard/backend/Cargo.lock'
```

### 2-4. 적용 후 기대 효과

이 수정으로 admin-dashboard는 진짜로 아래 순서가 닫힌다.

`openapi.rs -> export-openapi binary -> backend/docs/swagger.json -> frontend generated client -> CI drift gate`

지금은 마지막 연결이 문서상으로만 존재한다. 이 diff가 그 허공의 링크를 실제 코드로 바꾼다.

---

## 3. 남은 이슈 #2 — migration ordering governance is still implicit and unsafe

### 3-1. 현재 상태

`hololive/hololive-kakao-bot-go/scripts/migrations/apply-all.sh`는 현재 아래 방식으로 SQL을 적용한다.

```sh
for file in $(ls -1 "${MIGRATIONS_DIR}"/${MIGRATION_GLOB} 2>/dev/null | sort); do
  ...
done
```

즉 적용 순서가 사실상 **파일명 사전순**이다. 그런데 현재 migration 트리에는 duplicate numeric prefix가 이미 존재한다.

- `045_add_delivery_path_to_youtube_delivery_telemetry.sql`
- `045_create_youtube_content_alarm_tracking.sql`
- `051_add_alarm_timing_to_youtube_delivery_telemetry.sql`
- `051_add_closed_at_to_youtube_community_shorts_observation_windows.sql`
- `051_normalize_legacy_youtube_short_content_ids.sql`
- `053_add_canonical_content_identity_to_youtube_content_alarm_tracking.sql`
- `053_create_youtube_community_shorts_source_posts.sql`

즉 지금 구조는 다음 의미다.

- “번호가 곧 순서”라는 관례가 이미 깨졌다.
- 그런데 apply 스크립트는 여전히 “파일명 sort”에 의존한다.
- duplicate prefix를 CI가 막아주지도 않는다.
- 순서를 human이 추론해야 한다.

현재 SQL 내용상 당장 폭발하는 순서는 아니지만, 이것은 **장기적으로 merge collision을 반드시 부르는 구조**다.

### 3-2. 판단

여기서는 historical migration 파일을 지금 대규모 rename/renumber 하는 것보다, **명시적 manifest를 도입해 ordering을 코드로 고정**하는 것이 맞다.

이유는 세 가지다.

첫째, 이미 운영에 반영된 migration filename을 뒤늦게 대거 바꾸는 것은 리스크가 크다.  
둘째, duplicate prefix 문제의 본질은 “이름”이 아니라 “적용 순서가 명시되어 있지 않다”는 점이다.  
셋째, manifest를 넣으면 future merge에서도 번호 충돌과 순서 drift를 gate로 막을 수 있다.

### 3-3. 적용 diff

#### 새 파일 추가

`hololive/hololive-kakao-bot-go/scripts/migrations/manifest.txt`

아래 내용을 **그대로** 추가한다.

```text
007-add-approaching-notified.sql
008-optimize-indexes.sql
009-add-photo-column.sql
010-add-alarm-types-and-templates.sql
011-create-youtube-content-tables.sql
012-seed-all-templates.sql
013-add-template-revisions.sql
014-add-outbox-group-templates.sql
015-update-alarm-notification-header.sql
016-add-multi-group-support.sql
017-add-stellive-chzzk-support.sql
018-add-twitch-user-id-and-vspo-members.sql
019-add-youtube-stats-index.sql
020_create_youtube_channel_latest_stats.sql
021-optimize-alarm-outbox-query-indexes.sql
022-add-auth-acl-major-event-tables.sql
023-alarm-scheduled-time-and-flag-convention.sql
024-room-based-alarm-lookup.sql
025-alarm-notification-absolute-time.sql
026-seed-major-event-templates.sql
027-add-notified-month.sql
028-seed-monthly-event-template.sql
029-add-live-started-template.sql
030_add_member_news_subscriptions.sql
031_seed_member_news_templates.sql
032_notification_delivery_outbox.sql
033_update_member_news_digest_template.sql
034_add_major_event_link_check_columns.sql
035_add_scraper_user.sql
036_create_youtube_notification_delivery.sql
037_acl_blacklist_mode.sql
040_unify_indie_org.sql
041-refresh-stellive-chzzk-channel-ids.sql
042_add_youtube_community_published_at.sql
043_add_youtube_video_published_at.sql
044_create_youtube_delivery_telemetry.sql
045_add_delivery_path_to_youtube_delivery_telemetry.sql
045_create_youtube_content_alarm_tracking.sql
046_add_youtube_content_alarm_latency_result.sql
047_add_post_id_to_youtube_delivery_telemetry.sql
048_add_attempt_timeline_to_youtube_delivery_telemetry.sql
049_create_youtube_community_shorts_observation_windows.sql
050_add_observation_window_to_youtube_delivery_telemetry.sql
051_add_alarm_timing_to_youtube_delivery_telemetry.sql
051_add_closed_at_to_youtube_community_shorts_observation_windows.sql
051_normalize_legacy_youtube_short_content_ids.sql
052_add_delivery_status_to_youtube_content_alarm_tracking.sql
053_add_canonical_content_identity_to_youtube_content_alarm_tracking.sql
053_create_youtube_community_shorts_source_posts.sql
054_create_youtube_community_shorts_observation_post_baselines.sql
055_create_youtube_community_shorts_alarm_states.sql
```

이 순서는 현재 트리의 안정적인 적용 순서를 **명시적으로 고정**한 것이다. historical 파일명은 건드리지 않는다.

#### apply script 교체

`hololive/hololive-kakao-bot-go/scripts/migrations/apply-all.sh`

```diff
@@
 MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"
 MIGRATION_GLOB="${MIGRATION_GLOB:-[0-9]*.sql}"
+MIGRATION_MANIFEST="${MIGRATION_MANIFEST:-${MIGRATIONS_DIR}/manifest.txt}"
@@
-echo "==> applying migrations in ${MIGRATIONS_DIR}/${MIGRATION_GLOB} to ${PGDATABASE}@${PGHOST}:${PGPORT}"
-
-for file in $(ls -1 "${MIGRATIONS_DIR}"/${MIGRATION_GLOB} 2>/dev/null | sort); do
-  echo "==> apply ${file}"
-  PGPASSWORD="${PGPASSWORD}" psql \
-    -v ON_ERROR_STOP=1 \
-    -h "${PGHOST}" \
-    -p "${PGPORT}" \
-    -U "${PGUSER}" \
-    -d "${PGDATABASE}" \
-    -f "${file}"
-done
+if [ ! -f "${MIGRATION_MANIFEST}" ]; then
+  echo "migration manifest not found: ${MIGRATION_MANIFEST}" >&2
+  exit 1
+fi
+
+echo "==> applying migrations from manifest ${MIGRATION_MANIFEST} to ${PGDATABASE}@${PGHOST}:${PGPORT}"
+
+while IFS= read -r entry || [ -n "${entry}" ]; do
+  case "${entry}" in
+    ''|'#'*)
+      continue
+      ;;
+  esac
+
+  file="${MIGRATIONS_DIR}/${entry}"
+  if [ ! -f "${file}" ]; then
+    echo "manifest entry not found: ${file}" >&2
+    exit 1
+  fi
+
+  echo "==> apply ${file}"
+  PGPASSWORD="${PGPASSWORD}" psql \
+    -v ON_ERROR_STOP=1 \
+    -h "${PGHOST}" \
+    -p "${PGPORT}" \
+    -U "${PGUSER}" \
+    -d "${PGDATABASE}" \
+    -f "${file}"
+done < "${MIGRATION_MANIFEST}"
```

핵심은 두 가지다.  
하나는 `for file in $(ls ...)`를 없애는 것, 다른 하나는 ordering source를 manifest 하나로 고정하는 것이다.

#### manifest 검증 스크립트 추가

`scripts/architecture/check-migration-manifest.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${ROOT_DIR}/hololive/hololive-kakao-bot-go/scripts/migrations"
MANIFEST="${MIGRATIONS_DIR}/manifest.txt"

if [[ ! -f "${MANIFEST}" ]]; then
  echo "FAIL: migration manifest missing: ${MANIFEST}" >&2
  exit 1
fi

mapfile -t manifest_entries < <(grep -v '^[[:space:]]*$' "${MANIFEST}" | grep -v '^[[:space:]]*#')
mapfile -t sql_files < <(find "${MIGRATIONS_DIR}" -maxdepth 1 -type f -name '[0-9]*.sql' -printf '%f\n' | sort)

if [[ ${#manifest_entries[@]} -eq 0 ]]; then
  echo "FAIL: migration manifest is empty" >&2
  exit 1
fi

manifest_sorted="$(printf '%s\n' "${manifest_entries[@]}" | sort)"
manifest_unique="$(printf '%s\n' "${manifest_entries[@]}" | sort | uniq)"
if [[ "${manifest_sorted}" != "${manifest_unique}" ]]; then
  echo "FAIL: duplicate entries in migration manifest" >&2
  exit 1
fi

sql_joined="$(printf '%s\n' "${sql_files[@]}")"
manifest_joined="$(printf '%s\n' "${manifest_entries[@]}")"

if [[ "${sql_joined}" != "$(printf '%s\n' "${manifest_entries[@]}" | sort)" ]]; then
  echo "FAIL: migration manifest and actual SQL files differ" >&2
  echo "--- manifest only" >&2
  comm -23 <(printf '%s\n' "${manifest_entries[@]}" | sort) <(printf '%s\n' "${sql_files[@]}" | sort) >&2 || true
  echo "--- sql only" >&2
  comm -13 <(printf '%s\n' "${manifest_entries[@]}" | sort) <(printf '%s\n' "${sql_files[@]}" | sort) >&2 || true
  exit 1
fi

echo "OK: migration manifest matches SQL files"
```

#### architecture gate에 연결

`scripts/architecture/ci-boundary-gate.sh`

```diff
@@
 echo "[M1] Go trigger route hardcoding check"
 "${SCRIPT_DIR}/check-go-trigger-route-hardcoding.sh"
 echo
+
+echo "[M1] migration manifest check"
+"${SCRIPT_DIR}/check-migration-manifest.sh"
+echo
```

### 3-4. 적용 후 기대 효과

이제 migration ordering은 더 이상 "sort 결과"가 아니라 **명시된 운영 순서**가 된다. duplicate numeric prefix는 남아 있더라도, ordering ambiguity는 사라진다.

---

## 4. 남은 이슈 #3 — architecture file LOC gate is currently failing on this tree

### 4-1. 현재 상태

현재 트리에서 `./scripts/architecture/check-file-loc.sh`를 실행하면 실제로 실패한다.

실패 항목은 아래 한 건이다.

- `hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline.go:407 > 400`

이건 이론적 가능성이 아니라 **현재 레포가 자기 gate를 실제로 통과하지 못하는 상태**라는 뜻이다.

### 4-2. 판단

여기서는 threshold를 한 줄 추가해서 눌러버리는 것보다, **파일을 실제 의미 단위로 분리하는 것**이 맞다. 이 파일은 현재 세 덩어리가 섞여 있다.

- 타입/상수 선언
- baseline collection/build orchestration
- route/path/activation helper

이 세 층은 분리 기준이 명확하다.

### 4-3. 적용 diff

#### 기존 파일 삭제

```diff
-delete hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline.go
```

#### 새 파일 1: 타입/상수

`hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline_types.go`

아래 내용을 분리한다.

- line 18-89: constants, public structs, `communityShortsAlarmActivationKey`

```go
package app

import (
    "time"

    "github.com/kapu/hololive-shared/pkg/domain"
)

const (
    communityShortsLegacyDeliveryPath  = "legacy_alarm_queue"
    communityShortsNewDeliveryPath     = "youtube_outbox_dispatcher"
    communityShortsLegacyStatus        = "blocked"
    communityShortsDeliveryModeNew     = "new_only"
    communityShortsDeliveryModeOff     = "disabled"
    communityShortsDeliveryModePending = "pending_cutover"
)

type CommunityShortsTargetBaseline struct {
    GeneratedAt  time.Time                              `json:"generated_at"`
    Runtime      CommunityShortsTargetBaselineRuntime   `json:"runtime"`
    Sources      CommunityShortsTargetBaselineSources   `json:"sources"`
    PathMappings []CommunityShortsTargetBaselinePath    `json:"path_mappings"`
    Channels     []CommunityShortsTargetBaselineChannel `json:"channels"`
}

type CommunityShortsTargetBaselineRuntime struct {
    FinalDeliveryOwner              string     `json:"final_delivery_owner"`
    CommunityShortsBigBangEnabled   bool       `json:"community_shorts_bigbang_enabled"`
    CommunityShortsBigBangCutoverAt *time.Time `json:"community_shorts_bigbang_cutover_at,omitempty"`
    TargetChannelCount              int        `json:"target_channel_count"`
}

type CommunityShortsTargetBaselineSources struct {
    OperationalChannels string `json:"operational_channels"`
    TypedSubscriberKeys string `json:"typed_subscriber_keys"`
    RoomSubscriptions   string `json:"room_subscriptions"`
}

type CommunityShortsTargetBaselinePath struct {
    AlarmType                domain.AlarmType `json:"alarm_type"`
    LegacyDeliveryPath       string           `json:"legacy_delivery_path"`
    LegacyStatus             string           `json:"legacy_status"`
    LegacyPathActive         bool             `json:"legacy_path_active"`
    NewDeliveryPath          string           `json:"new_delivery_path"`
    NewPathConfigured        bool             `json:"new_path_configured"`
    CutoverPending           bool             `json:"cutover_pending"`
    FinalDeliveryOwner       string           `json:"final_delivery_owner"`
    FinalDeliveryPath        string           `json:"final_delivery_path"`
    SubscriberKeyPrefix      string           `json:"subscriber_key_prefix"`
    ConfiguredChannelCount   int              `json:"configured_channel_count"`
    AlarmEnabledChannelCount int              `json:"alarm_enabled_channel_count"`
    AlarmEnabledRoomCount    int              `json:"alarm_enabled_room_count"`
    ActivationSource         string           `json:"activation_source"`
}

type CommunityShortsTargetBaselineChannel struct {
    OwnerLabel              string                                      `json:"owner_label"`
    ChannelID               string                                      `json:"channel_id"`
    CommunitySubscribersKey string                                      `json:"community_subscribers_key"`
    ShortsSubscribersKey    string                                      `json:"shorts_subscribers_key"`
    Routes                  []CommunityShortsTargetBaselineChannelRoute `json:"routes"`
}

type CommunityShortsTargetBaselineChannelRoute struct {
    AlarmType             domain.AlarmType `json:"alarm_type"`
    SubscriberKey         string           `json:"subscriber_key"`
    AlarmEnabled          bool             `json:"alarm_enabled"`
    SubscriberRoomCount   int              `json:"subscriber_room_count"`
    LegacyPathActive      bool             `json:"legacy_path_active"`
    NewPathConfigured     bool             `json:"new_path_configured"`
    CutoverPending        bool             `json:"cutover_pending"`
    EffectiveDeliveryMode string           `json:"effective_delivery_mode"`
    FinalDeliveryOwner    string           `json:"final_delivery_owner"`
    FinalDeliveryPath     string           `json:"final_delivery_path"`
}

type communityShortsAlarmActivationKey struct {
    channelID string
    alarmType domain.AlarmType
}
```

#### 새 파일 2: 수집/빌드 진입점

`hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline_build.go`

기존 line 91-208을 이 파일로 이동한다.

```go
package app

import (
    "context"
    "fmt"
    "log/slog"
    "slices"
    "strings"
    "time"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/domain"
    sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
    sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
    sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func CollectCommunityShortsTargetBaseline(ctx context.Context, cfg *config.Config, logger *slog.Logger) (CommunityShortsTargetBaseline, error) {
    if ctx == nil {
        ctx = context.Background()
    }
    if cfg == nil {
        return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: config is nil")
    }
    if logger == nil {
        logger = slog.Default()
    }

    databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
    if err != nil {
        return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: provide database resources: %w", err)
    }
    if cleanupDB != nil {
        defer cleanupDB()
    }

    memberRepository := sharedproviders.ProvideMemberRepository(databaseResources.Service, logger)
    members, err := memberRepository.GetAllMembers(ctx)
    if err != nil {
        return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load members: %w", err)
    }

    alarmRepository := sharedalarm.NewRepository(databaseResources.Service, logger)
    alarms, err := alarmRepository.LoadAll(ctx)
    if err != nil {
        return CommunityShortsTargetBaseline{}, fmt.Errorf("collect community shorts target baseline: load alarms: %w", err)
    }

    channels := buildCommunityShortsOperationalChannelsFromMembers(members)
    return buildCommunityShortsTargetBaseline(channels, alarms, cfg.Ingestion, time.Now().UTC())
}

func buildCommunityShortsTargetBaseline(
    channels []communityShortsOperationalChannel,
    alarms []*domain.Alarm,
    ingestionCfg config.IngestionConfig,
    generatedAt time.Time,
) (CommunityShortsTargetBaseline, error) {
    if err := validateCommunityShortsOperationalTargets(channels); err != nil {
        return CommunityShortsTargetBaseline{}, fmt.Errorf("build community shorts target baseline: %w", err)
    }

    activationIndex := buildCommunityShortsAlarmActivationIndex(alarms)
    finalOwner := resolveCommunityShortsFinalDeliveryOwner(ingestionCfg)
    cutoverPending := communityShortsCutoverPending(ingestionCfg, generatedAt)

    enabledChannels := make([]CommunityShortsTargetBaselineChannel, 0, len(channels))
    for i := range channels {
        if !channels[i].enabled {
            continue
        }
        channelID := strings.TrimSpace(channels[i].channelID)
        if channelID == "" {
            continue
        }
        targetKeys := sharedalarmkeys.BuildChannelContentAlarmTargetKeys(channelID)
        enabledChannels = append(enabledChannels, CommunityShortsTargetBaselineChannel{
            OwnerLabel:              strings.TrimSpace(channels[i].ownerLabel),
            ChannelID:               channelID,
            CommunitySubscribersKey: targetKeys.CommunitySubscribersKey,
            ShortsSubscribersKey:    targetKeys.ShortsSubscribersKey,
            Routes:                  buildCommunityShortsTargetBaselineRoutes(channelID, finalOwner, activationIndex, cutoverPending),
        })
    }

    slices.SortFunc(enabledChannels, func(left, right CommunityShortsTargetBaselineChannel) int {
        if left.ChannelID != right.ChannelID {
            return strings.Compare(left.ChannelID, right.ChannelID)
        }
        return strings.Compare(left.OwnerLabel, right.OwnerLabel)
    })

    cutoverAt := normalizedCommunityShortsCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)

    return CommunityShortsTargetBaseline{
        GeneratedAt: generatedAt.UTC(),
        Runtime: CommunityShortsTargetBaselineRuntime{
            FinalDeliveryOwner:              finalOwner,
            CommunityShortsBigBangEnabled:   ingestionCfg.CommunityShortsBigBangEnabled,
            CommunityShortsBigBangCutoverAt: cutoverAt,
            TargetChannelCount:              len(enabledChannels),
        },
        Sources: CommunityShortsTargetBaselineSources{
            OperationalChannels: "postgres.members -> resolveCommunityShortsOperationalChannels",
            TypedSubscriberKeys: "alarm typed subscriber keys -> BuildChannelContentAlarmTargetKeys",
            RoomSubscriptions:   "postgres.alarms alarm_types -> community/shorts typed room counts",
        },
        PathMappings: buildCommunityShortsTargetBaselinePaths(enabledChannels, finalOwner, cutoverPending),
        Channels:     enabledChannels,
    }, nil
}

func buildCommunityShortsOperationalChannelsFromMembers(members []*domain.Member) []communityShortsOperationalChannel {
    channels := make([]communityShortsOperationalChannel, 0, len(members))
    seenChannelIDs := make(map[string]struct{}, len(members))
    for i := range members {
        member := members[i]
        if member == nil || member.IsGraduated {
            continue
        }
        channelID := strings.TrimSpace(member.ChannelID)
        if channelID != "" {
            if _, exists := seenChannelIDs[channelID]; exists {
                continue
            }
            seenChannelIDs[channelID] = struct{}{}
        }
        channels = append(channels, communityShortsOperationalChannel{
            ownerLabel: communityShortsTargetOwnerLabel(member),
            channelID:  channelID,
            enabled:    channelID != "",
        })
    }
    return channels
}
```

#### 새 파일 3: helper/route/path/activation

`hololive/hololive-stream-ingester/internal/app/community_shorts_target_baseline_helpers.go`

기존 line 210-407을 이 파일로 이동한다.

```go
package app

import (
    "strings"
    "time"

    "github.com/kapu/hololive-shared/pkg/config"
    "github.com/kapu/hololive-shared/pkg/domain"
    sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func buildCommunityShortsTargetBaselinePaths(
    channels []CommunityShortsTargetBaselineChannel,
    finalOwner string,
    cutoverPending bool,
) []CommunityShortsTargetBaselinePath {
    paths := make([]CommunityShortsTargetBaselinePath, 0, len(communityShortsTargetAlarmTypes()))
    for _, alarmType := range communityShortsTargetAlarmTypes() {
        configuredChannelCount := len(channels)
        alarmEnabledChannelCount := 0
        alarmEnabledRoomCount := 0
        pathCutoverPending := false
        for i := range channels {
            route, ok := communityShortsRouteForType(channels[i].Routes, alarmType)
            if !ok || !route.AlarmEnabled {
                continue
            }
            alarmEnabledChannelCount++
            alarmEnabledRoomCount += route.SubscriberRoomCount
            if route.CutoverPending {
                pathCutoverPending = true
            }
        }

        paths = append(paths, CommunityShortsTargetBaselinePath{
            AlarmType:                alarmType,
            LegacyDeliveryPath:       communityShortsLegacyDeliveryPath,
            LegacyStatus:             communityShortsLegacyStatus,
            LegacyPathActive:         false,
            NewDeliveryPath:          communityShortsNewDeliveryPath,
            NewPathConfigured:        true,
            CutoverPending:           cutoverPending && pathCutoverPending,
            FinalDeliveryOwner:       finalOwner,
            FinalDeliveryPath:        communityShortsFinalDeliveryPath(finalOwner),
            SubscriberKeyPrefix:      communityShortsSubscriberKeyPrefix(alarmType),
            ConfiguredChannelCount:   configuredChannelCount,
            AlarmEnabledChannelCount: alarmEnabledChannelCount,
            AlarmEnabledRoomCount:    alarmEnabledRoomCount,
            ActivationSource:         "postgres.alarms alarm_types",
        })
    }
    return paths
}

func buildCommunityShortsTargetBaselineRoutes(
    channelID string,
    finalOwner string,
    activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
    cutoverPending bool,
) []CommunityShortsTargetBaselineChannelRoute {
    routes := make([]CommunityShortsTargetBaselineChannelRoute, 0, len(communityShortsTargetAlarmTypes()))
    for _, alarmType := range communityShortsTargetAlarmTypes() {
        roomCount := lookupCommunityShortsAlarmRoomCount(activationIndex, channelID, alarmType)
        routeCutoverPending := cutoverPending && roomCount > 0
        routes = append(routes, CommunityShortsTargetBaselineChannelRoute{
            AlarmType:             alarmType,
            SubscriberKey:         sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType),
            AlarmEnabled:          roomCount > 0,
            SubscriberRoomCount:   roomCount,
            LegacyPathActive:      false,
            NewPathConfigured:     true,
            CutoverPending:        routeCutoverPending,
            EffectiveDeliveryMode: communityShortsEffectiveDeliveryMode(roomCount, routeCutoverPending),
            FinalDeliveryOwner:    finalOwner,
            FinalDeliveryPath:     communityShortsFinalDeliveryPath(finalOwner),
        })
    }
    return routes
}

func buildCommunityShortsAlarmActivationIndex(
    alarms []*domain.Alarm,
) map[communityShortsAlarmActivationKey]map[string]struct{} {
    index := make(map[communityShortsAlarmActivationKey]map[string]struct{})
    for _, alarmRecord := range alarms {
        if alarmRecord == nil {
            continue
        }

        roomID := strings.TrimSpace(alarmRecord.RoomID)
        channelID := strings.TrimSpace(alarmRecord.ChannelID)
        if roomID == "" || channelID == "" {
            continue
        }

        for _, alarmType := range normalizedCommunityShortsAlarmTypes(alarmRecord.AlarmTypes) {
            key := communityShortsAlarmActivationKey{channelID: channelID, alarmType: alarmType}
            roomSet := index[key]
            if roomSet == nil {
                roomSet = make(map[string]struct{})
                index[key] = roomSet
            }
            roomSet[alarmRecord.RegistryKey()] = struct{}{}
        }
    }
    return index
}

func normalizedCommunityShortsAlarmTypes(alarmTypes domain.AlarmTypes) []domain.AlarmType {
    if len(alarmTypes) == 0 {
        alarmTypes = domain.DefaultAlarmTypes
    }

    result := make([]domain.AlarmType, 0, len(alarmTypes))
    seen := make(map[domain.AlarmType]struct{}, len(alarmTypes))
    for _, alarmType := range alarmTypes {
        if alarmType != domain.AlarmTypeCommunity && alarmType != domain.AlarmTypeShorts {
            continue
        }
        if _, ok := seen[alarmType]; ok {
            continue
        }
        seen[alarmType] = struct{}{}
        result = append(result, alarmType)
    }
    return result
}

func communityShortsTargetAlarmTypes() []domain.AlarmType {
    return []domain.AlarmType{domain.AlarmTypeCommunity, domain.AlarmTypeShorts}
}

func communityShortsSubscriberKeyPrefix(alarmType domain.AlarmType) string {
    switch alarmType {
    case domain.AlarmTypeCommunity:
        return sharedalarmkeys.ChannelSubscribersCommunityPrefix
    case domain.AlarmTypeShorts:
        return sharedalarmkeys.ChannelSubscribersShortsPrefix
    default:
        return ""
    }
}

func communityShortsRouteForType(
    routes []CommunityShortsTargetBaselineChannelRoute,
    alarmType domain.AlarmType,
) (CommunityShortsTargetBaselineChannelRoute, bool) {
    for i := range routes {
        if routes[i].AlarmType == alarmType {
            return routes[i], true
        }
    }
    return CommunityShortsTargetBaselineChannelRoute{}, false
}

func lookupCommunityShortsAlarmRoomCount(
    activationIndex map[communityShortsAlarmActivationKey]map[string]struct{},
    channelID string,
    alarmType domain.AlarmType,
) int {
    return len(activationIndex[communityShortsAlarmActivationKey{
        channelID: strings.TrimSpace(channelID),
        alarmType: alarmType,
    }])
}

func communityShortsEffectiveDeliveryMode(roomCount int, cutoverPending bool) string {
    if roomCount == 0 {
        return communityShortsDeliveryModeOff
    }
    if cutoverPending {
        return communityShortsDeliveryModePending
    }
    return communityShortsDeliveryModeNew
}

func communityShortsCutoverPending(ingestionCfg config.IngestionConfig, generatedAt time.Time) bool {
    if !ingestionCfg.CommunityShortsBigBangEnabled {
        return false
    }
    cutoverAt := normalizedCommunityShortsCutoverAt(ingestionCfg.CommunityShortsBigBangCutoverAt)
    if cutoverAt == nil {
        return false
    }
    return generatedAt.UTC().Before(*cutoverAt)
}

func resolveCommunityShortsFinalDeliveryOwner(ingestionCfg config.IngestionConfig) string {
    if ingestionCfg.CommunityShortsBigBangEnabled {
        return youtubeScraperRuntimeName
    }
    return streamIngesterRuntimeName
}

func normalizedCommunityShortsCutoverAt(cutoverAt time.Time) *time.Time {
    if cutoverAt.IsZero() {
        return nil
    }
    normalized := cutoverAt.UTC()
    return &normalized
}

func communityShortsFinalDeliveryPath(finalOwner string) string {
    trimmedOwner := strings.TrimSpace(finalOwner)
    if trimmedOwner == "" {
        return communityShortsNewDeliveryPath
    }
    return trimmedOwner + "." + communityShortsNewDeliveryPath
}
```

### 4-4. threshold 파일은 당장 바꾸지 않는다

`docs/architecture/file-loc-thresholds.txt`에 이 파일 threshold를 추가하지 않는다. 이유는 이번 수정의 목적이 "gate를 눌러 통과"가 아니라 **실제로 분해해서 gate 기준 안으로 다시 들어오는 것**이기 때문이다.

### 4-5. 적용 후 기대 효과

- 현재 failing 상태인 multi-language LOC gate가 정상화된다.
- community/shorts baseline 영역이 타입/빌드/helper로 분해돼 유지보수성이 올라간다.
- 이 영역에서 추가 변경이 들어와도 LOC gate를 다시 깨뜨릴 가능성이 줄어든다.

---

## 5. 남은 이슈 #4 — review bundle / architecture scripts still drift from actual usage

### 5-1. 현재 상태

현재 트리의 `scripts/review/export-full-bundle.sh`와 `docs/current/review-bundles.md`는 `.worktrees`, `.tasklists`, `.gemini`, `.omc`, `BUNDLE_MANIFEST.txt` 등을 제외하도록 되어 있다.

그런데 이번에 업로드된 실제 full bundle extracted tree에는 아래가 포함되어 있었다.

- `.tasklists/`
- `.worktrees/`
- 루트 `BUNDLE_MANIFEST.txt`

게다가 현재 script가 생성하는 manifest schema는 다음 필드를 가진다.

- `tracked_only:`
- `excluded_patterns:`

반면 실제 업로드본의 manifest는 아래 필드만 있다.

- `repo_root:`
- `mode:`
- `generated_at:`
- `branch:`
- `commit:`
- `included_files:`
- `exclusions:`

즉, **레포 안의 공식 export script와 실제로 사용된 번들 생성 경로가 다르다**는 뜻이다.

추가로 architecture script도 bundle 환경에서 false-green을 낼 수 있다. 예를 들어 `scripts/architecture/check-tracked-local-artifacts.sh`는 git checkout이 아닌 extracted bundle에서 실행하면 `git ls-files`가 실패해도 최종적으로는 `OK: no tracked local artifacts`를 출력한다. 이것은 검사 통과가 아니라 **검사 미수행**이다.

### 5-2. 판단

여기서는 세 가지를 같이 해야 한다.

1. export script 자체는 유지한다.  
2. bundle validity를 사후 검증하는 별도 스크립트를 추가한다.  
3. git checkout 전제가 필요한 architecture script는 "git이 없으면 실패"하도록 바꾼다.

### 5-3. 적용 diff

#### `.dockerignore` 정리

현재 `.dockerignore`에는 `.omx`라는 단독 오타성 항목이 있고, `.gemini`는 없다. review/export 규칙과 docker build context exclusion이 맞지 않는다.

`.dockerignore`

```diff
 .git
-.omx
+.omc
+.gemini
 .codex
 .claude
 .serena
 .idea
 .vscode
 .github/
 .tasklists/
 .runlogs/
 .worktrees/
 .deploy-snapshots/
 artifacts/
 BUNDLE_MANIFEST.txt
@@
 **/.idea
 **/.vscode
+**/.gemini
+**/.omc
 **/.serena
 **/.gomodcache
 **/.gocache
```

#### 공통 git guard helper 추가

`scripts/architecture/lib/git_guard.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

require_git_checkout() {
  local root_dir="$1"
  if ! git -C "${root_dir}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "FAIL: git checkout required for this script: ${root_dir}" >&2
    exit 1
  fi
}
```

#### tracked-local-artifacts check 수정

`scripts/architecture/check-tracked-local-artifacts.sh`

```diff
@@
 SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
 ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
+
+source "${SCRIPT_DIR}/lib/git_guard.sh"
+require_git_checkout "${ROOT_DIR}"
@@
-done < <(git -C "${ROOT_DIR}" ls-files)
+done < <(git -C "${ROOT_DIR}" ls-files)
```

핵심은 git이 없는데도 false-green을 내지 않도록 만드는 것이다.

#### go import graph export 스크립트 정리

`scripts/architecture/export-go-workspace-import-graph.sh`

```diff
@@
 SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
 ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
-REPO_CANONICAL_ROOT="$(cd "$(git -C "${ROOT_DIR}" rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"
 OUTPUT_FILE="${1:-${ROOT_DIR}/artifacts/architecture/go-workspace-import-graph.txt}"
+
+source "${SCRIPT_DIR}/lib/git_guard.sh"
+require_git_checkout "${ROOT_DIR}"
```

`REPO_CANONICAL_ROOT`는 현재 계산만 하고 쓰지 않으므로 제거한다.

#### full bundle verification script 추가

`scripts/review/verify-full-bundle.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

ARCHIVE_PATH="${1:?usage: verify-full-bundle.sh <bundle.tar.gz>}"
TMP_DIR="$(mktemp -d)"
cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

tar -xzf "${ARCHIVE_PATH}" -C "${TMP_DIR}"
ROOT="${TMP_DIR}"

fail_if_exists() {
  local path="$1"
  if [[ -e "${ROOT}/${path}" ]]; then
    echo "FAIL: excluded path found in bundle: ${path}" >&2
    exit 1
  fi
}

fail_if_exists ".worktrees"
fail_if_exists ".tasklists"
fail_if_exists ".runlogs"
fail_if_exists ".codex"
fail_if_exists ".claude"
fail_if_exists ".serena"
fail_if_exists ".gemini"
fail_if_exists "artifacts"

if [[ ! -f "${ROOT}/BUNDLE_MANIFEST.txt" ]]; then
  echo "FAIL: bundle manifest missing" >&2
  exit 1
fi

grep -q '^tracked_only:' "${ROOT}/BUNDLE_MANIFEST.txt" || {
  echo "FAIL: bundle manifest schema drift: tracked_only missing" >&2
  exit 1
}

grep -q '^excluded_patterns:' "${ROOT}/BUNDLE_MANIFEST.txt" || {
  echo "FAIL: bundle manifest schema drift: excluded_patterns missing" >&2
  exit 1
}

echo "OK: full bundle matches in-repo export policy"
```

#### review 문서 보강

`docs/current/review-bundles.md`

```diff
@@
 ## Full review bundle export
@@
 - full bundle의 `BUNDLE_MANIFEST.txt`는 export script가 생성하는 내부 manifest만 포함합니다.
+
+## Verification
+
+- 검토용 bundle을 외부 경로/수동 tar로 만들지 않습니다.
+- `scripts/review/export-full-bundle.sh`만 정식 경로로 사용합니다.
+- 산출물 전달 전 `scripts/review/verify-full-bundle.sh <bundle.tar.gz>`로 excluded path와 manifest schema를 검증합니다.
```

### 5-4. 적용 후 기대 효과

- 레포가 말하는 번들 규칙과 실제 전달 산출물이 다시 일치한다.
- architecture script가 git checkout이 아닌 환경에서 false-green을 내지 않는다.
- review bundle이 더 이상 `.worktrees`나 `.tasklists`로 오염되지 않는다.

---

## 6. 남은 이슈 #5 — cache mock default still allows silent false positives

### 6-1. 현재 상태

`hololive/hololive-shared/pkg/service/cache/mocks/client.go`는 지금 `Strict bool` 필드를 가진다.

```go
type Client struct {
    Strict bool
    ...
}
```

그리고 constructor는 다음과 같다.

```go
func NewStrictClient() *Client {
    return &Client{Strict: true}
}

func NewLenientClient() *Client {
    return &Client{Strict: false}
}
```

문제는 zero-value다. `&cachemocks.Client{}`는 `Strict: false`이므로 lenient다. 그런데 현재 테스트 트리에는 `&cachemocks.Client{}` literal이 아직 많이 남아 있다. 즉 의도하지 않은 lenient mock이 조용히 `nil` / zero value를 반환하면서 false positive를 만들 수 있다.

이건 테스트 신뢰도 이슈다. 특히 레포 전체에서 manual mock을 광범위하게 쓰는 구조라 영향이 작지 않다.

### 6-2. 판단

여기서는 `Strict bool`을 유지하면서 call site에 주석을 다는 식으로는 부족하다. **zero-value가 strict**가 되도록 의미를 뒤집는 것이 맞다.

즉 필드 이름을 `Lenient bool`으로 바꾸고, constructor 의미도 뒤집는다.

- `&cachemocks.Client{}` -> strict by default
- `NewStrictClient()` -> zero-value wrapper
- `NewLenientClient()` -> 정말 필요한 테스트만 opt-in

### 6-3. 적용 diff

#### mock struct 수정

`hololive/hololive-shared/pkg/service/cache/mocks/client.go`

```diff
 type Client struct {
-    Strict bool
+    Lenient bool
@@
 func NewStrictClient() *Client {
-    return &Client{Strict: true}
+    return &Client{}
 }
 
 func NewLenientClient() *Client {
-    return &Client{Strict: false}
+    return &Client{Lenient: true}
 }
 
 func (m *Client) panicIfUnset(name string) {
-    if m != nil && m.Strict {
+    if m == nil || !m.Lenient {
         panic("cache mock: " + name + " not set")
     }
 }
```

file header comment도 같이 바꾼다.

```diff
-// For unit tests, set only the function fields you need; unconfigured calls will panic
-// to avoid silent false-positives.
+// For unit tests, zero-value Client is strict by default; unconfigured calls panic
+// unless Lenient is explicitly enabled.
```

#### direct literal call site 교체 규칙

다음 규칙으로 repo-wide codemod를 적용한다.

1. **아무 함수 필드 없이** `&cachemocks.Client{}`만 쓰는 경우  
   → `cachemocks.NewStrictClient()`로 치환.

2. 일부 function field를 채우는 literal인 경우  
   → 그대로 두되, 정말 부분 mock 의도라면 `cachemocks.NewStrictClient()`에 field assignment 패턴으로 바꾸거나, literal에 `Lenient: true`를 명시.

3. 의도적으로 permissive한 경우만  
   → `cachemocks.NewLenientClient()` 또는 `Lenient: true`.

#### 즉시 치환 대상 예시

아래는 zero-value strict 전환에서 가장 먼저 고쳐야 하는 대표 call site다.

```diff
- cacheSvc := &cachemocks.Client{}
+ cacheSvc := cachemocks.NewStrictClient()
```

대표 대상:

- `hololive/hololive-shared/pkg/service/member/cache_test.go`
- `hololive/hololive-shared/pkg/service/alarm/dedup/service_test.go`
- `hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer_additional_test.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_admin_additional_test.go`
- `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/checker_additional_test.go`

그리고 아래 패턴은 전수 확인 대상이다.

```bash
rg -n '&cachemocks\.Client\{' hololive admin-dashboard shared-go
```

현재 트리에서는 이 패턴이 50건 이상 남아 있으므로, 이번에는 **repo-wide mechanical cleanup**으로 끝내는 것이 맞다.

#### 추가 회귀 테스트

`hololive/hololive-shared/pkg/service/cache/mocks/client_test.go`

```go
package mocks

import "testing"

func TestZeroValueClientIsStrict(t *testing.T) {
    mock := &Client{}

    defer func() {
        if recover() == nil {
            t.Fatal("expected panic for unset strict zero-value mock")
        }
    }()

    _, _ = mock.Exists(t.Context(), "key")
}

func TestNewLenientClientDoesNotPanic(t *testing.T) {
    mock := NewLenientClient()
    _, err := mock.Exists(t.Context(), "key")
    if err != nil {
        t.Fatalf("expected nil error, got %v", err)
    }
}
```

### 6-4. 적용 후 기대 효과

이제 `&cachemocks.Client{}`를 아무 생각 없이 써도 기본이 strict이므로, 테스트가 빠뜨린 expectation이 조용히 통과하는 일이 줄어든다. 수동 mock을 많이 쓰는 코드베이스에서는 이런 default semantics가 매우 중요하다.

---

## 7. 남은 이슈 #6 — thin wrapper / alias residue that still adds no meaning

### 7-1. 현재 상태

이전보다 많이 좋아졌지만, 아주 얇은 wrapper/alias가 일부 남아 있다. 대표적으로 다음 둘은 지금도 제거 가치가 있다.

1. `hololive/hololive-shared/pkg/providers/youtube_providers.go`
   - `ProvideHolodexAPIKey(cfg config.HolodexConfig) string { return cfg.APIKey }`
2. bot / stream-ingester bootstrap의 `type infraResources = sharedmodules.InfraModule`

둘 다 “추상화처럼 보이지만 실제 의미는 추가하지 않는” 코드다. 규모는 작지만, 이런 residue가 쌓이면 다시 AI 냄새가 난다.

### 7-2. 판단

여기서는 과감한 구조 변경이 아니라, **순수 residue 청소**만 하면 된다.

### 7-3. 적용 diff

#### Holodex API key wrapper 제거

`hololive/hololive-shared/pkg/providers/youtube_providers.go`

```diff
-// ProvideHolodexAPIKey - 설정에서 API 키 추출
-func ProvideHolodexAPIKey(cfg config.HolodexConfig) string {
-    return cfg.APIKey
-}
```

import에서도 `config`가 이 함수 때문에만 필요하면 같이 제거한다.

#### call site inline 치환

`hololive/hololive-stream-ingester/internal/app/stream_ingester_runtime_builder.go`

```diff
- holodexAPIKey := sharedproviders.ProvideHolodexAPIKey(cfg.Holodex)
+ holodexAPIKey := cfg.Holodex.APIKey
```

`hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_foundation.go`

```diff
- holodexAPIKey := providers.ProvideHolodexAPIKey(cfg.Holodex)
+ holodexAPIKey := cfg.Holodex.APIKey
```

#### infra alias 제거

`hololive/hololive-stream-ingester/internal/app/bootstrap.go`

```diff
-type infraResources = sharedmodules.InfraModule
-
 // initStreamInfra 는 stream-ingester에 필요한 캐시/DB 리소스를 초기화합니다.
-func initStreamInfra(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
+func initStreamInfra(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
     module, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
     if err != nil {
         return nil, fmt.Errorf("provide infra resources: %w", err)
     }
     return module, nil
 }
```

`hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go`

```diff
-type infraResources = sharedmodules.InfraModule
-
 // initInfraResources 는 캐시/DB 리소스를 초기화한다.
-func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
+func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*sharedmodules.InfraModule, error) {
     module, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
     if err != nil {
         return nil, fmt.Errorf("provide infra resources: %w", err)
     }
     return module, nil
 }
```

이후 bot 내부 call site 시그니처를 전부 `*sharedmodules.InfraModule`로 치환한다.

대표 파일:

- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_foundation.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm_stack.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_integration.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_modules.go`
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_services_alarm.go`
- 관련 테스트 파일들

단순 mechanical rename이므로 의미 변화는 없다.

### 7-4. 적용 후 기대 효과

이 수정은 기능 버그를 고치지는 않는다. 대신 조립층을 읽을 때 “정말 필요한 abstraction”만 남겨서 인지 부하를 낮춘다. 마지막 마감 단계에서 해야 하는 residue 청소에 가깝다.

---

## 8. 적용 우선순위

이번 보강안은 아래 순서로 적용하는 것이 안전하다.

### 1단계 — CI/contract를 먼저 닫는다

1. `admin-dashboard/backend/src/bin/export-openapi.rs` 추가  
2. frontend workflow 그대로 실행되도록 smoke test 추가  
3. `npm run generate:api` -> `git diff --exit-code` 경로가 실제로 닫히는지 확인

이 단계가 끝나면 admin-dashboard contract export가 finally 실체를 갖는다.

### 2단계 — migration governance를 먼저 고정한다

1. `manifest.txt` 추가  
2. `apply-all.sh`를 manifest 기반으로 교체  
3. `check-migration-manifest.sh` 추가 후 architecture gate 연결

이 단계가 끝나면 future SQL merge가 훨씬 안전해진다.

### 3단계 — failing LOC gate를 실제로 해소한다

1. `community_shorts_target_baseline.go` 3분할  
2. `check-file-loc.sh` 재실행  
3. threshold 파일은 불필요하면 건드리지 않음

이 단계는 현재 레포가 자기 gate를 통과하지 못하는 문제를 해소한다.

### 4단계 — review/export/process 정합성을 맞춘다

1. `.dockerignore` 수정  
2. `git_guard.sh` 추가  
3. `check-tracked-local-artifacts.sh`와 `export-go-workspace-import-graph.sh` 수정  
4. `verify-full-bundle.sh` 추가

이 단계가 끝나면 “레포가 말하는 정책”과 “실제로 전달되는 산출물”이 다시 맞는다.

### 5단계 — test reliability / residue cleanup

1. cache mock strict-by-default 전환  
2. direct literal call site 전수 교체  
3. `ProvideHolodexAPIKey` 제거  
4. `infraResources` alias 제거

이 단계는 마지막 품질 정리다. 기능 동작보다 회귀 방지와 코드 질 개선에 가깝다.

---

## 9. 최종 결론

이번 업로드본은 이전까지 남아 있던 큰 구조 이슈 대부분을 이미 닫았다. 그래서 이번 리뷰에서 남은 것은 "또 다른 설계 대수술"이 아니라, **마감 전 꼭 닫아야 하는 마지막 운영/CI/거버넌스/테스트 품질 이슈**다.

요약하면 남은 것은 여섯 가지다.

- OpenAPI export binary 부재
- migration ordering manifest 부재
- failing LOC gate 1건
- bundle/export/process drift
- cache mock default strictness 문제
- thin wrapper residue

이 여섯 가지를 정리하면, 지금 저장소는 더 이상 “구조적으로 덜 끝난 상태”가 아니라, 실제로 마감 가능한 수준에 들어간다.
