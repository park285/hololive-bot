# 최신 수정본 전체범위 추가 리뷰 및 diff 청사진
기준 아카이브: `hololive-bot-full-20260410T075710Z.tar.gz`

이번 문서는 **최신 업로드본 전체 범위**를 다시 본 뒤, **이미 닫힌 항목은 반복하지 않고 실제로 아직 남아 있는 잔존 이슈만** 정리한 보강 설계서입니다.
범위는 다음을 모두 포함합니다.

- Go 런타임 (`hololive-kakao-bot-go`, `hololive-llm-sched`, `hololive-stream-ingester`, `hololive-dispatcher-go`)
- `hololive-shared`, `shared-go`
- `admin-dashboard` backend / frontend
- build / review bundle / architecture / CI scripts
- repo root docs / 운영 compose / generated client pipeline

이번 문서는 **정적 코드 검토 기준**입니다. 실행 환경 제약 때문에 전체 런타임 테스트를 전부 재현한 결과물은 아니며, 코드 경로, 설정 전파, contract 의미, build/review/CI 정합성 중심으로 검토했습니다.

---

## 이번 업로드본에서 이미 해결된 것으로 확인되어 반복하지 않는 항목

아래 항목은 이번 문서에서 의도적으로 제외했습니다. 이전 버전에서 지적했지만 현재 업로드본에서는 실질적으로 닫혔거나, 더 이상 추가 지적사항으로 볼 필요가 없는 것들입니다.

- 유튜브 알람의 target minute 런타임 재동기화
- alarm crossed-window 판정 기본 구조
- scraper polling env 실제 wiring
- scraper scheduler의 worker channel full 시 임의 `+10초` 지연 제거
- `shared-go` 로깅 SSOT 정리
- `holoClient.ts` extra wrapper 제거
- settlement 제거 축의 큰 구조 문제

즉 아래 내용은 “예전 이슈 재탕”이 아니라, **지금 시점에 실제로 남아 있는 것만** 추린 것입니다.

---

## 1. Review bundle / 로컬 아티팩트 누수 정책이 아직 닫히지 않았음

### 문제

최신 업로드본에는 여전히 다음이 포함되어 있습니다.

- `.worktrees/`
- `.tasklists/`
- `admin-dashboard/frontend/.vscode/`
- `hololive/hololive-kakao-bot-go/.idea/`
- `hololive/hololive-kakao-bot-go/.omc/`
- repo root `BUNDLE_MANIFEST.txt`

반면 문서 `docs/current/review-bundles.md`는 full bundle에서 이런 로컬 오염물이 기본적으로 포함되지 않는다고 설명합니다.
또 `scripts/review/export-full-bundle.sh`는 현재 tracked file 기준 수집을 하면서 **tracked 상태로 들어와 버린 로컬 디렉터리 / IDE 메타데이터 / manifest 파일**을 필터링하지 않습니다.

즉 지금 상태는 다음 둘이 동시에 어긋난 상태입니다.

1. **정책 문서**: 안 들어가야 한다고 말함
2. **실제 export 경로**: tracked/local residue가 들어갈 수 있음

### 영향

- 리뷰 번들 스코프 오염
- `.worktrees` 내부의 또 다른 repo 스냅샷 누출
- IDE / local workflow metadata 유출
- `BUNDLE_MANIFEST.txt` 같은 generated artifact가 source-of-truth처럼 섞임
- build / audit / external review 시 “무엇이 실제 코드인가”를 흐림

### 수정 diff

### 1-1. `.gitignore`에 `BUNDLE_MANIFEST.txt`를 명시적으로 추가

```diff
diff --git a/.gitignore b/.gitignore
--- a/.gitignore
+++ b/.gitignore
@@
 # Compressed archives
 *.tar.gz
+BUNDLE_MANIFEST.txt
```

### 1-2. `scripts/review/export-full-bundle.sh`에 tracked file 필터를 추가

```diff
diff --git a/scripts/review/export-full-bundle.sh b/scripts/review/export-full-bundle.sh
--- a/scripts/review/export-full-bundle.sh
+++ b/scripts/review/export-full-bundle.sh
@@
 TMP_DIR="$(mktemp -d)"
 FILE_LIST="${TMP_DIR}/files.txt"
 MANIFEST="${TMP_DIR}/BUNDLE_MANIFEST.txt"
+
+BUNDLE_EXCLUDES=(
+  ".git"
+  ".worktrees"
+  ".tasklists"
+  ".runlogs"
+  ".codex"
+  ".claude"
+  ".serena"
+  ".gemini"
+  "artifacts"
+  "logs"
+  "node_modules"
+  "dist"
+  "coverage"
+  "*.tar.gz"
+  "BUNDLE_MANIFEST.txt"
+  ".idea"
+  ".vscode"
+  ".omc"
+)
@@
 trap cleanup EXIT
@@
 mkdir -p "${OUT_DIR}"
+
+should_exclude_path() {
+  local path="$1"
+  case "${path}" in
+    .git|.git/*|\
+    .worktrees|.worktrees/*|\
+    .tasklists|.tasklists/*|\
+    .runlogs|.runlogs/*|\
+    .codex|.codex/*|\
+    .claude|.claude/*|\
+    .serena|.serena/*|\
+    .gemini|.gemini/*|\
+    artifacts|artifacts/*|\
+    logs|logs/*|\
+    node_modules|node_modules/*|\
+    dist|dist/*|\
+    coverage|coverage/*|\
+    BUNDLE_MANIFEST.txt|\
+    *.tar.gz|\
+    .idea|.idea/*|\
+    .vscode|.vscode/*|\
+    .omc|.omc/*|\
+    */.idea|*/.idea/*|\
+    */.vscode|*/.vscode/*|\
+    */.omc|*/.omc/*)
+      return 0
+      ;;
+  esac
+  return 1
+}
+
+append_git_paths() {
+  while IFS= read -r -d '' path; do
+    [[ -e "${path}" ]] || continue
+    if should_exclude_path "${path}"; then
+      continue
+    fi
+    printf '%s\0' "${path}" >> "${FILE_LIST}"
+  done
+}

 (
   cd "${ROOT_DIR}"
-  while IFS= read -r -d '' path; do
-    [[ -e "${path}" ]] || continue
-    printf '%s\0' "${path}" >> "${FILE_LIST}"
-  done < <(git ls-files -z --cached)
+  append_git_paths < <(git ls-files -z --cached)
   if [[ "${INCLUDE_UNTRACKED}" == "true" ]]; then
-    while IFS= read -r -d '' path; do
-      [[ -e "${path}" ]] || continue
-      printf '%s\0' "${path}" >> "${FILE_LIST}"
-    done < <(git ls-files -z --others --exclude-standard)
+    append_git_paths < <(git ls-files -z --others --exclude-standard)
   fi
 )
+
+sort -zu "${FILE_LIST}" -o "${FILE_LIST}"

 cat > "${MANIFEST}" <<MANIFEST
 repo_root: ${ROOT_DIR}
 mode: full
 tracked_only: $([[ "${INCLUDE_UNTRACKED}" == "true" ]] && echo "false" || echo "true")
 generated_at: $(date -u +%FT%TZ)
 branch: $(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD)
 commit: $(git -C "${ROOT_DIR}" rev-parse HEAD)
 included_files: $(tr -cd '\0' < "${FILE_LIST}" | wc -c | tr -d ' ')
+excluded_patterns: $(IFS=,; echo "${BUNDLE_EXCLUDES[*]}")
 MANIFEST
```

### 1-3. `docs/current/review-bundles.md`를 실제 동작 기준으로 수정

```diff
diff --git a/docs/current/review-bundles.md b/docs/current/review-bundles.md
--- a/docs/current/review-bundles.md
+++ b/docs/current/review-bundles.md
@@
-### Full bundle policy
-
-- 기본값은 tracked file only 입니다.
-- `.worktrees`, `.tasklists`, agent 실행 디렉터리, `logs`, `artifacts`, 기존 tarball 같은 로컬 오염물은 기본적으로 포함되지 않습니다.
-- untracked 파일이 정말 필요할 때만 `INCLUDE_UNTRACKED=true`를 명시합니다.
+### Full bundle policy
+
+- 기본값은 tracked file only 입니다.
+- tracked 상태로 들어간 파일이라도 `.worktrees`, `.tasklists`, IDE metadata, agent 실행 디렉터리, `logs`, `artifacts`, 기존 tarball, `BUNDLE_MANIFEST.txt` 같은 로컬 오염물은 **항상 제외**합니다.
+- untracked 파일이 정말 필요할 때만 `INCLUDE_UNTRACKED=true`를 명시합니다.
+- full bundle의 `BUNDLE_MANIFEST.txt`는 export script가 생성하는 내부 manifest만 포함합니다.
```

### 1-4. tracked local artifact를 막는 architecture gate 추가

새 파일: `scripts/architecture/check-tracked-local-artifacts.sh`

```diff
diff --git a/scripts/architecture/check-tracked-local-artifacts.sh b/scripts/architecture/check-tracked-local-artifacts.sh
new file mode 100755
--- /dev/null
+++ b/scripts/architecture/check-tracked-local-artifacts.sh
@@
+#!/usr/bin/env bash
+set -euo pipefail
+
+SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
+ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
+
+violations=()
+
+while IFS= read -r path; do
+  case "${path}" in
+    .worktrees/*|\
+    .tasklists/*|\
+    .runlogs/*|\
+    BUNDLE_MANIFEST.txt|\
+    .idea/*|\
+    .vscode/*|\
+    .omc/*|\
+    */.idea/*|\
+    */.vscode/*|\
+    */.omc/*)
+      violations+=("${path}")
+      ;;
+  esac
+done < <(git -C "${ROOT_DIR}" ls-files)
+
+if (( ${#violations[@]} > 0 )); then
+  echo "FAIL: tracked local artifacts detected" >&2
+  for path in "${violations[@]}"; do
+    echo " - ${path}" >&2
+  done
+  exit 1
+fi
+
+echo "OK: no tracked local artifacts"
```

### 1-5. `ci-boundary-gate.sh`에 새 gate 연결

```diff
diff --git a/scripts/architecture/ci-boundary-gate.sh b/scripts/architecture/ci-boundary-gate.sh
--- a/scripts/architecture/ci-boundary-gate.sh
+++ b/scripts/architecture/ci-boundary-gate.sh
@@
 echo "[M0] removed runtime reference check"
 "${SCRIPT_DIR}/check-removed-runtime-references.sh"
 echo
+
+echo "[M0] tracked local artifact check"
+"${SCRIPT_DIR}/check-tracked-local-artifacts.sh"
+echo
```

### 1-6. 1회성 정리 커맨드

이건 diff라기보다 **반드시 같이 해야 하는 one-time cleanup** 입니다.

```bash
git rm --cached BUNDLE_MANIFEST.txt
git rm --cached -r .worktrees .tasklists
git rm --cached -r admin-dashboard/frontend/.vscode
git rm --cached -r hololive/hololive-kakao-bot-go/.idea
git rm --cached -r hololive/hololive-kakao-bot-go/.vscode
git rm --cached -r hololive/hololive-kakao-bot-go/.omc
```

### 의사결정 이유

이 문제는 “스크립트 한 줄”이 아니라 **정책-스크립트-CI-실제 tracked 상태**가 모두 엮인 문제입니다.
그래서 export script만 고치면 다시 들어오고, `.gitignore`만 고치면 이미 tracked 된 것은 계속 남습니다.
반드시 네 층을 같이 닫아야 합니다.

---

## 2. Admin Dashboard OpenAPI export 파이프라인이 아직 완결되지 않았고 문서가 stale 함

### 문제

현재 아래가 동시에 존재합니다.

- `admin-dashboard/frontend/package.json`는 `cargo run --bin export-openapi`를 호출
- `.github/workflows/admin-dashboard-frontend.yml`도 `backend/src/bin/export-openapi.rs`를 path trigger에 포함
- `admin-dashboard/README.md`, `admin-dashboard/docs/openapi-pipeline.md`도 `backend/src/bin/export-openapi.rs`가 있다고 가정
- **하지만 실제 파일은 없음**

게다가 문서에는 더 이상 존재하지 않는 `src/api/holoClient.ts`와, 이미 제거된 wildcard holo proxy fallback까지 계속 남아 있습니다.

즉 현재 상태는 “코드는 앞으로 갔는데 문서/파이프라인 일부는 예전 구조를 계속 말하고 있는 상태”입니다.

### 영향

- `npm run generate:api`가 환경에 따라 즉시 깨질 수 있음
- frontend CI 의미가 문서와 어긋남
- 팀원이 README만 믿고 작업하면 잘못된 경로를 따라감
- 이미 제거된 `holoClient.ts`, wildcard proxy fallback을 유지해야 하는 것처럼 오해하게 됨

### 수정 diff

### 2-1. OpenAPI export binary를 실제로 추가

새 파일: `admin-dashboard/backend/src/bin/export-openapi.rs`

```diff
diff --git a/admin-dashboard/backend/src/bin/export-openapi.rs b/admin-dashboard/backend/src/bin/export-openapi.rs
new file mode 100644
--- /dev/null
+++ b/admin-dashboard/backend/src/bin/export-openapi.rs
@@
+use admin_dashboard::openapi::ApiDoc;
+use utoipa::OpenApi;
+
+fn main() {
+    let document = ApiDoc::openapi();
+    let json = serde_json::to_string_pretty(&document)
+        .expect("serialize OpenAPI document");
+    println!("{json}");
+}
```

### 2-2. `admin-dashboard/README.md`에서 stale 경로를 정리

```diff
diff --git a/admin-dashboard/README.md b/admin-dashboard/README.md
--- a/admin-dashboard/README.md
+++ b/admin-dashboard/README.md
@@
-- wildcard proxy fallback: `ANY /admin/api/holo/{*path}` for websocket or 아직 이관하지 않은 compatibility 경로
+- typed holo contract: `GET/POST/PATCH/DELETE /admin/api/holo/*`
@@
-`src/api/adminClient.ts`가 generated `Admin` singleton의 단일 소유자입니다. `src/api/core.ts`와 `src/api/holoClient.ts`는 이 인스턴스만 공유해서 interceptor, base URL, 인증 동작이 갈라지지 않게 유지합니다.
+`src/api/adminClient.ts`가 generated `Admin` singleton의 단일 소유자입니다. transport source-of-truth는 이 인스턴스 하나만 사용합니다. `src/api/core.ts`는 compatibility wrapper이고, 별도의 `holoClient.ts` 레이어는 더 이상 존재하지 않습니다.
@@
-- wildcard proxy route는 websocket / 미이관 compatibility fallback 용도로만 유지
+- holo dashboard endpoint는 backend가 소유하는 typed contract만 유지합니다.
```

### 2-3. `admin-dashboard/docs/openapi-pipeline.md`도 현재 구조에 맞게 수정

```diff
diff --git a/admin-dashboard/docs/openapi-pipeline.md b/admin-dashboard/docs/openapi-pipeline.md
--- a/admin-dashboard/docs/openapi-pipeline.md
+++ b/admin-dashboard/docs/openapi-pipeline.md
@@
-- `frontend/src/api/adminClient.ts`가 generated `Admin` singleton을 소유하고, `core.ts`와 `holoClient.ts`는 이 인스턴스만 사용합니다.
-- `/admin/api/holo/*`는 backend가 소유하는 typed contract를 먼저 추가하고, wildcard proxy는 websocket/compatibility fallback으로만 남깁니다.
+- `frontend/src/api/adminClient.ts`가 generated `Admin` singleton을 소유합니다.
+- `frontend/src/api/core.ts`는 compatibility wrapper이며, 별도의 `holoClient.ts` 레이어는 사용하지 않습니다.
+- `/admin/api/holo/*`는 backend가 소유하는 typed contract만 유지합니다.
@@
-4. generated wrapper는 `src/api/core.ts`, `src/api/holoClient.ts`에서만 얇게 감쌉니다.
+4. generated wrapper는 `src/api/adminClient.ts`를 단일 transport entry로 사용하고, 필요 시 `src/api/core.ts`에서만 compatibility 레이어를 둡니다.
```

### 의사결정 이유

OpenAPI pipeline은 **“backend schema → export → frontend generated client”**가 한 줄로 닫혀야 합니다.
지금은 이 줄의 중간에 파일이 비어 있고, 문서는 이전 구조를 계속 가리킵니다.
이 상태는 코드보다 문서가 더 위험합니다. 팀이 잘못된 경로를 신뢰하게 만들기 때문입니다.

---

## 3. Swagger / OpenAPI 엔드포인트가 여전히 공개 라우터에 직접 노출됨

### 문제

`admin-dashboard/backend/src/routes.rs`는 현재 다음을 public router에 직접 merge 합니다.

```rust
SwaggerUi::new("/swagger-ui").url("/api-docs/openapi.json", crate::openapi::ApiDoc::openapi())
```

이 구조는 production 환경에서도 admin contract 문서를 **인증 없이** 노출하는 형태입니다.

frontend generated client는 runtime에 공개된 Swagger UI가 없어도 됩니다.
OpenAPI export는 build 시 `export-openapi` binary로 해결하면 됩니다.
즉 runtime 공개 docs는 필수 기능이 아니라 운영 표면만 넓히는 기능입니다.

### 영향

- admin API 표면이 외부에 불필요하게 노출됨
- production 환경에서 공격 표면 증가
- runtime docs 경로와 build-time schema export 경로가 혼동됨

### 수정 diff

### 3-1. `Config`에 docs 노출 플래그 추가

```diff
diff --git a/admin-dashboard/backend/src/config.rs b/admin-dashboard/backend/src/config.rs
--- a/admin-dashboard/backend/src/config.rs
+++ b/admin-dashboard/backend/src/config.rs
@@
 pub struct Config {
@@
     pub holo_bot_url: String,
     pub holo_bot_api_key: String,
+    pub enable_openapi: bool,
+    pub enable_swagger_ui: bool,
     pub log_dir: String,
     pub security: SecurityConfig,
     pub session: SessionConfig,
 }
@@
     pub fn load() -> Result<Self> {
         let environment = env_string("ENV", "production");
         let allow_localhost_in_prod = env_bool("ALLOW_LOCALHOST_IN_PROD", false);
+        let enable_swagger_ui = env_bool("ENABLE_SWAGGER_UI", environment != "production");
+        let enable_openapi = env_bool("ENABLE_OPENAPI", enable_swagger_ui || environment != "production");
@@
             holo_bot_url: env_string("HOLO_BOT_URL", "http://hololive-kakao-bot-go:30001"),
             holo_bot_api_key: env_string("HOLO_BOT_API_KEY", ""),
+            enable_openapi,
+            enable_swagger_ui,
             log_dir: env_string("LOG_DIR", "/app/logs"),
             security: SecurityConfig::load(&environment, allow_localhost_in_prod),
             session: SessionConfig::load(),
         })
     }
 }
```

### 3-2. config 테스트 추가

```diff
diff --git a/admin-dashboard/backend/src/config.rs b/admin-dashboard/backend/src/config.rs
--- a/admin-dashboard/backend/src/config.rs
+++ b/admin-dashboard/backend/src/config.rs
@@
     fn test_invalid_port_returns_error() {
@@
     }
+
+    #[test]
+    fn test_openapi_defaults_disabled_in_production() {
+        with_env_vars(
+            &[
+                ("ENV", Some("production")),
+                ("ADMIN_PASS_HASH", Some("hash-primary")),
+                ("ADMIN_PASS_BCRYPT", None),
+                ("SESSION_SECRET", Some("session-secret")),
+                ("ADMIN_SECRET_KEY", None),
+                ("ENABLE_OPENAPI", None),
+                ("ENABLE_SWAGGER_UI", None),
+            ],
+            || {
+                let cfg = Config::load().expect("config load");
+                assert!(!cfg.enable_openapi);
+                assert!(!cfg.enable_swagger_ui);
+            },
+        );
+    }
+
+    #[test]
+    fn test_swagger_flag_enables_openapi() {
+        with_env_vars(
+            &[
+                ("ENV", Some("production")),
+                ("ADMIN_PASS_HASH", Some("hash-primary")),
+                ("ADMIN_PASS_BCRYPT", None),
+                ("SESSION_SECRET", Some("session-secret")),
+                ("ADMIN_SECRET_KEY", None),
+                ("ENABLE_SWAGGER_UI", Some("true")),
+                ("ENABLE_OPENAPI", None),
+            ],
+            || {
+                let cfg = Config::load().expect("config load");
+                assert!(cfg.enable_swagger_ui);
+                assert!(cfg.enable_openapi);
+            },
+        );
+    }
 }
```

### 3-3. `routes.rs`에서 docs 라우터를 분리하고 인증 뒤로 이동

```diff
diff --git a/admin-dashboard/backend/src/routes.rs b/admin-dashboard/backend/src/routes.rs
--- a/admin-dashboard/backend/src/routes.rs
+++ b/admin-dashboard/backend/src/routes.rs
@@
 use crate::state::AppState;

+fn build_docs_router(state: Arc<AppState>) -> Router {
+    if !state.config.enable_openapi && !state.config.enable_swagger_ui {
+        return Router::new();
+    }
+
+    let auth_layer =
+        middleware::from_fn_with_state(state.clone(), crate::auth::middleware::auth_middleware);
+
+    let openapi_json = if state.config.enable_openapi {
+        Router::new().route(
+            "/admin/api/openapi.json",
+            get(|| async { Json(crate::openapi::ApiDoc::openapi()) }),
+        )
+    } else {
+        Router::new()
+    };
+
+    let swagger_ui = if state.config.enable_swagger_ui {
+        Router::new().merge(
+            SwaggerUi::new("/admin/docs")
+                .url("/admin/api/openapi.json", crate::openapi::ApiDoc::openapi()),
+        )
+    } else {
+        Router::new()
+    };
+
+    Router::new()
+        .merge(openapi_json)
+        .merge(swagger_ui)
+        .layer(auth_layer)
+}
+
 #[allow(clippy::too_many_lines)]
 pub fn build_router(state: Arc<AppState>) -> Router {
@@
     let spa = Router::new()
         .route("/favicon.svg", get(crate::static_files::serve_favicon))
         .route("/assets/{*path}", get(crate::static_files::serve_static))
         .fallback(get(crate::static_files::serve_index));
+
+    let docs_router = build_docs_router(state.clone());

     Router::new()
         .merge(public)
         .merge(authenticated)
-        .merge(
-            SwaggerUi::new("/swagger-ui")
-                .url("/api-docs/openapi.json", crate::openapi::ApiDoc::openapi()),
-        )
+        .merge(docs_router)
         .merge(api_fallback)
         .merge(spa)
         .layer(middleware::map_response(
             crate::auth::middleware::apply_security_headers,
         ))
```

### 의사결정 이유

build-time schema export와 runtime docs 노출은 **같은 문제처럼 보이지만 다른 문제**입니다.

- build-time export: CI / generated client 용
- runtime docs: 운영 표면

이 둘을 분리해야 보안과 개발 생산성이 동시에 깔끔해집니다.

---

## 4. System Stats contract가 아직 “고루틴”과 “스레드”를 섞어서 보여주고, 외부 서비스 조회도 순차 수행함

### 문제

`admin-dashboard/backend/src/status/system_stats.rs`는 admin-dashboard 자신의 프로세스 수치를 다음처럼 계산합니다.

```rust
sys.process(pid).and_then(|process| process.tasks()).map_or(0, HashSet::len)
```

이 값은 Rust 프로세스의 **task/thread 수**에 가깝습니다.
그런데 현재 contract와 UI는 이 값을 `goroutines`로 취급합니다. 외부 Go 서비스는 진짜 goroutine 수를 보내고, self는 thread 수를 같은 필드로 합산합니다.

또 외부 서비스 health 조회는 현재 순차 실행입니다. 서비스 수가 늘면 지연이 선형으로 증가합니다.

### 영향

- 관측 지표 의미가 틀림
- `totalGoroutines`가 실제로는 “Go goroutines + Rust thread”가 됨
- status websocket 갱신이 서비스 수에 비례해 느려짐
- parse error가 `available: true, goroutines: 0`로 뭉개져 contract drift를 숨김

### 수정 방향

이 문제는 단순 라벨 수정으로 끝나지 않습니다.
contract를 **runtime metric kind** 기준으로 다시 정의해야 합니다.

### 수정 diff

### 4-1. backend contract를 `count + metric_kind`로 재정의

```diff
diff --git a/admin-dashboard/backend/src/status/system_stats.rs b/admin-dashboard/backend/src/status/system_stats.rs
--- a/admin-dashboard/backend/src/status/system_stats.rs
+++ b/admin-dashboard/backend/src/status/system_stats.rs
@@
+use futures::future::join_all;
 use reqwest::Client;
 use serde::{Deserialize, Serialize};
@@
 #[derive(Debug, Clone, Serialize)]
 #[serde(rename_all = "camelCase")]
-pub struct ServiceRuntimeStats {
-    pub name: String,
-    pub goroutines: usize,
-    pub available: bool,
+pub enum RuntimeMetricKind {
+    Goroutine,
+    Thread,
+}
+
+#[derive(Debug, Clone, Serialize)]
+#[serde(rename_all = "camelCase")]
+pub struct ServiceRuntimeStats {
+    pub name: String,
+    pub count: usize,
+    pub metric_kind: RuntimeMetricKind,
+    pub available: bool,
+    #[serde(skip_serializing_if = "Option::is_none")]
+    pub error: Option<String>,
 }

 #[derive(Debug, Clone, Serialize)]
 #[serde(rename_all = "camelCase")]
 pub struct SystemStats {
@@
-    pub goroutines: usize,
-    pub total_goroutines: usize,
-    pub service_goroutines: Vec<ServiceRuntimeStats>,
+    pub thread_count: usize,
+    pub total_go_goroutines: usize,
+    pub total_runtime_units: usize,
+    pub service_runtime: Vec<ServiceRuntimeStats>,
     pub load_avg_1: f64,
     pub load_avg_5: f64,
     pub load_avg_15: f64,
 }
@@
-                        let goroutines = current_pid
+                        let thread_count = current_pid
                             .and_then(|pid| sys.process(pid))
                             .and_then(|process| process.tasks())
                             .map_or(0, std::collections::HashSet::len);
-                        let external_service_goroutines =
+                        let external_service_runtime =
                             fetch_service_runtime_stats(&http_client, &endpoints).await;
-                        let total_goroutines = goroutines
-                            + external_service_goroutines
+                        let total_go_goroutines = external_service_runtime
                                 .iter()
-                                .filter(|service| service.available)
-                                .map(|service| service.goroutines)
+                                .filter(|service| {
+                                    service.available
+                                        && matches!(service.metric_kind, RuntimeMetricKind::Goroutine)
+                                })
+                                .map(|service| service.count)
                                 .sum::<usize>();
-                        let mut service_goroutines =
-                            Vec::with_capacity(external_service_goroutines.len() + 1);
-                        service_goroutines.push(ServiceRuntimeStats {
+                        let total_runtime_units = thread_count
+                            + external_service_runtime
+                                .iter()
+                                .filter(|service| service.available)
+                                .map(|service| service.count)
+                                .sum::<usize>();
+                        let mut service_runtime =
+                            Vec::with_capacity(external_service_runtime.len() + 1);
+                        service_runtime.push(ServiceRuntimeStats {
                             name: "admin-dashboard".to_string(),
-                            goroutines,
+                            count: thread_count,
+                            metric_kind: RuntimeMetricKind::Thread,
                             available: true,
+                            error: None,
                         });
-                        service_goroutines.extend(external_service_goroutines);
+                        service_runtime.extend(external_service_runtime);
                         let stats = SystemStats {
@@
-                            goroutines,
-                            total_goroutines,
-                            service_goroutines,
+                            thread_count,
+                            total_go_goroutines,
+                            total_runtime_units,
+                            service_runtime,
                             load_avg_1: load.one,
                             load_avg_5: load.five,
                             load_avg_15: load.fifteen,
@@
 async fn fetch_service_runtime_stats(
     client: &Client,
     endpoints: &[ServiceEndpoint],
 ) -> Vec<ServiceRuntimeStats> {
-    let mut services = Vec::with_capacity(endpoints.len());
-
-    for endpoint in endpoints {
-        services.push(fetch_service_runtime_stat(client, endpoint).await);
-    }
-
-    services
+    join_all(
+        endpoints
+            .iter()
+            .map(|endpoint| fetch_service_runtime_stat(client, endpoint)),
+    )
+    .await
 }
@@
         Ok(resp) if resp.status().is_success() => match resp.json::<HealthResponse>().await {
             Ok(health) => ServiceRuntimeStats {
                 name: endpoint.name.clone(),
-                goroutines: extract_goroutines(&health),
+                count: extract_goroutines(&health),
+                metric_kind: RuntimeMetricKind::Goroutine,
                 available: true,
+                error: None,
             },
-            Err(_) => ServiceRuntimeStats {
+            Err(err) => ServiceRuntimeStats {
                 name: endpoint.name.clone(),
-                goroutines: 0,
-                available: true,
+                count: 0,
+                metric_kind: RuntimeMetricKind::Goroutine,
+                available: false,
+                error: Some(format!("invalid health payload: {err}")),
             },
         },
-        _ => ServiceRuntimeStats {
+        Err(err) => ServiceRuntimeStats {
             name: endpoint.name.clone(),
-            goroutines: 0,
+            count: 0,
+            metric_kind: RuntimeMetricKind::Goroutine,
             available: false,
+            error: Some(err.to_string()),
+        },
+        Ok(resp) => ServiceRuntimeStats {
+            name: endpoint.name.clone(),
+            count: 0,
+            metric_kind: RuntimeMetricKind::Goroutine,
+            available: false,
+            error: Some(format!("status: {}", resp.status())),
         },
     }
 }
```

### 4-2. backend 테스트도 새 contract 기준으로 수정

```diff
diff --git a/admin-dashboard/backend/src/status/system_stats.rs b/admin-dashboard/backend/src/status/system_stats.rs
--- a/admin-dashboard/backend/src/status/system_stats.rs
+++ b/admin-dashboard/backend/src/status/system_stats.rs
@@
     fn test_system_stats_serializes_frontend_contract() {
         let stats = SystemStats {
             cpu_usage: 12.5,
             memory_total: 1024,
             memory_used: 256,
             memory_usage: 25.0,
-            goroutines: 7,
-            total_goroutines: 7,
-            service_goroutines: vec![ServiceRuntimeStats {
+            thread_count: 7,
+            total_go_goroutines: 42,
+            total_runtime_units: 49,
+            service_runtime: vec![ServiceRuntimeStats {
                 name: "admin-dashboard".to_string(),
-                goroutines: 7,
+                count: 7,
+                metric_kind: RuntimeMetricKind::Thread,
                 available: true,
+                error: None,
             }],
             load_avg_1: 0.1,
             load_avg_5: 0.2,
             load_avg_15: 0.3,
         };
@@
-        assert_eq!(value["goroutines"], json!(7));
-        assert_eq!(value["totalGoroutines"], json!(7));
+        assert_eq!(value["threadCount"], json!(7));
+        assert_eq!(value["totalGoGoroutines"], json!(42));
+        assert_eq!(value["totalRuntimeUnits"], json!(49));
         assert_eq!(
-            value["serviceGoroutines"][0]["name"],
+            value["serviceRuntime"][0]["name"],
             json!("admin-dashboard")
         );
+        assert_eq!(
+            value["serviceRuntime"][0]["metricKind"],
+            json!("thread")
+        );
         assert!(value.get("cpu_usage").is_none());
         assert!(value.get("memory_usage_percent").is_none());
     }
```

### 4-3. frontend parser를 새 contract 기준으로 바꾸되 legacy payload도 읽도록 호환 유지

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/types.ts b/admin-dashboard/frontend/src/features/stats/types.ts
--- a/admin-dashboard/frontend/src/features/stats/types.ts
+++ b/admin-dashboard/frontend/src/features/stats/types.ts
@@
-export interface ServiceGoroutines {
+export type RuntimeMetricKind = "goroutine" | "thread";
+
+export interface ServiceRuntimeStat {
	name: string;
-	goroutines: number;
+	count: number;
+	metricKind: RuntimeMetricKind;
	available: boolean;
+	error?: string | null;
 }

 export interface SystemStats {
	cpuUsage: number;
	memoryUsage: number;
	memoryTotal: number;
	memoryUsed: number;
-	goroutines: number;
-	totalGoroutines: number;
-	serviceGoroutines: ServiceGoroutines[];
+	threadCount: number;
+	totalGoGoroutines: number;
+	totalRuntimeUnits: number;
+	serviceRuntime: ServiceRuntimeStat[];
 }
```

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/lib/systemStats.ts b/admin-dashboard/frontend/src/features/stats/lib/systemStats.ts
--- a/admin-dashboard/frontend/src/features/stats/lib/systemStats.ts
+++ b/admin-dashboard/frontend/src/features/stats/lib/systemStats.ts
@@
-	const goroutines = asNumber(record["goroutines"]);
-	const totalGoroutines = asNumber(
-		record["totalGoroutines"] ??
-			record["total_goroutines"] ??
-			record["goroutines"],
-	);
-	const serviceGoroutinesValue = Array.isArray(record["serviceGoroutines"])
-		? record["serviceGoroutines"]
-		: Array.isArray(record["service_goroutines"])
-			? record["service_goroutines"]
-			: [];
+	const threadCount = asNumber(
+		record["threadCount"] ??
+			record["thread_count"] ??
+			record["goroutines"],
+	);
+	const totalGoGoroutines = asNumber(
+		record["totalGoGoroutines"] ??
+			record["total_go_goroutines"] ??
+			record["totalGoroutines"] ??
+			record["total_goroutines"] ??
+			record["goroutines"],
+	);
+	const totalRuntimeUnits = asNumber(
+		record["totalRuntimeUnits"] ??
+			record["total_runtime_units"] ??
+			record["totalGoroutines"] ??
+			record["total_goroutines"],
+	);
+	const serviceRuntimeValue = Array.isArray(record["serviceRuntime"])
+		? record["serviceRuntime"]
+		: Array.isArray(record["service_runtime"])
+			? record["service_runtime"]
+			: Array.isArray(record["serviceGoroutines"])
+				? record["serviceGoroutines"]
+				: Array.isArray(record["service_goroutines"])
+					? record["service_goroutines"]
+					: [];
@@
-		goroutines === null ||
-		totalGoroutines === null
+		threadCount === null ||
+		totalGoGoroutines === null ||
+		totalRuntimeUnits === null
	) {
		return null;
	}

-	const serviceGoroutines = serviceGoroutinesValue
+	const serviceRuntime = serviceRuntimeValue
		.map((entry) => {
			const item = asRecord(entry);
			if (!item || typeof item["name"] !== "string") return null;

-			const itemGoroutines = asNumber(item["goroutines"]);
-			if (itemGoroutines === null || typeof item["available"] !== "boolean") {
+			const count = asNumber(item["count"] ?? item["goroutines"]);
+			if (count === null || typeof item["available"] !== "boolean") {
				return null;
			}
+
+			const metricKind =
+				item["metricKind"] === "thread" || item["metric_kind"] === "thread"
+					? "thread"
+					: "goroutine";

			return {
				name: item["name"],
-				goroutines: itemGoroutines,
+				count,
+				metricKind,
				available: item["available"],
+				error:
+					typeof item["error"] === "string"
+						? item["error"]
+						: null,
			};
		})
		.filter(
-			(entry): entry is SystemStats["serviceGoroutines"][number] =>
+			(entry): entry is SystemStats["serviceRuntime"][number] =>
				entry !== null,
		);

	return {
		cpuUsage,
		memoryUsage,
		memoryTotal,
		memoryUsed,
-		goroutines,
-		totalGoroutines,
-		serviceGoroutines,
+		threadCount,
+		totalGoGoroutines,
+		totalRuntimeUnits,
+		serviceRuntime,
	};
 };
```

### 4-4. history hook / chart / badge도 새 contract로 업데이트

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/hooks/useSystemStatsHistory.ts b/admin-dashboard/frontend/src/features/stats/hooks/useSystemStatsHistory.ts
--- a/admin-dashboard/frontend/src/features/stats/hooks/useSystemStatsHistory.ts
+++ b/admin-dashboard/frontend/src/features/stats/hooks/useSystemStatsHistory.ts
@@
-			const serviceValues = data.serviceGoroutines.reduce<Record<string, number>>(
+			const serviceValues = data.serviceRuntime.reduce<Record<string, number>>(
				(acc, service) => {
-					acc[service.name] = service.available ? service.goroutines : 0;
+					acc[service.name] = service.available ? service.count : 0;
					return acc;
				},
				{},
			);
@@
-		currentStats?.serviceGoroutines.forEach((service) => {
+		currentStats?.serviceRuntime.forEach((service) => {
			names.add(service.name);
		});
```

```diff
diff --git a/admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx b/admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx
--- a/admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx
+++ b/admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx
@@
-import { GoroutineChart } from "@/features/stats/components/GoroutineChart";
+import { RuntimeUnitsChart } from "@/features/stats/components/RuntimeUnitsChart";
@@
-								{currentStats.totalGoroutines} Goroutines
+								Go {currentStats.totalGoGoroutines}
							</span>
						</div>
+						<div className="hidden items-center gap-1.5 rounded border border-slate-100 bg-white px-2 py-1 shadow-sm sm:flex">
+							<CircuitBoard size={14} className="text-slate-400" />
+							<span className="font-bold text-slate-500">
+								Threads {currentStats.threadCount}
+							</span>
+						</div>
					</div>
				)}
@@
-								서비스별 고루틴
+								서비스별 런타임 단위
@@
-					<GoroutineChart history={statsHistory} serviceNames={serviceNames} />
+					<RuntimeUnitsChart history={statsHistory} serviceNames={serviceNames} />
@@
-						services={currentStats.serviceGoroutines}
+						services={currentStats.serviceRuntime}
```

새 파일: `admin-dashboard/frontend/src/features/stats/components/RuntimeUnitsChart.tsx`

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/components/RuntimeUnitsChart.tsx b/admin-dashboard/frontend/src/features/stats/components/RuntimeUnitsChart.tsx
new file mode 100644
--- /dev/null
+++ b/admin-dashboard/frontend/src/features/stats/components/RuntimeUnitsChart.tsx
@@
+import { cn } from "@/lib/utils";
+import {
+	CHART_PADDING_X,
+	CHART_PADDING_Y,
+	CHART_WIDTH,
+	GOROUTINE_CHART_HEIGHT,
+	getChartLabels,
+	getServiceColor,
+	type SystemStatsPoint,
+} from "../lib/systemStats";
+
+interface RuntimeUnitsChartProps {
+	history: SystemStatsPoint[];
+	serviceNames: string[];
+}
+
+export const RuntimeUnitsChart = ({
+	history,
+	serviceNames,
+}: RuntimeUnitsChartProps) => {
+	const maxValue = Math.max(1, ...history.map((point) => point.totalRuntimeUnits));
+	const labels = getChartLabels(history);
+	const innerHeight = GOROUTINE_CHART_HEIGHT - CHART_PADDING_Y * 2;
+	const innerWidth = CHART_WIDTH - CHART_PADDING_X * 2;
+	const columnWidth =
+		history.length > 0 ? innerWidth / history.length : innerWidth;
+	const barWidth = Math.max(6, columnWidth - 4);
+
+	return (
+		<div className="w-full">
+			<div className="relative h-[160px] w-full overflow-hidden rounded-lg border border-slate-100 bg-white">
+				<svg
+					viewBox={`0 0 ${String(CHART_WIDTH)} ${String(GOROUTINE_CHART_HEIGHT)}`}
+					className="h-full w-full"
+					preserveAspectRatio="none"
+					aria-label="서비스별 런타임 단위 추이"
+				>
+					{[0, 0.5, 1].map((ratio) => {
+						const y = CHART_PADDING_Y + innerHeight * ratio;
+						const labelValue = Math.round(maxValue * (1 - ratio));
+						return (
+							<g key={ratio}>
+								<line
+									x1={CHART_PADDING_X}
+									y1={y}
+									x2={CHART_WIDTH - CHART_PADDING_X}
+									y2={y}
+									stroke="#e2e8f0"
+									strokeDasharray="4 4"
+								/>
+								<text x={6} y={y + 4} fill="#94a3b8" fontSize="10">
+									{labelValue}
+								</text>
+							</g>
+						);
+					})}
+
+					{history.map((point, pointIndex) => {
+						const x =
+							CHART_PADDING_X +
+							pointIndex * columnWidth +
+							Math.max((columnWidth - barWidth) / 2, 1);
+						let stackOffset = 0;
+
+						return serviceNames.map((serviceName) => {
+							const value = point.serviceValues[serviceName] ?? 0;
+							if (value <= 0) {
+								return null;
+							}
+
+							const height = (value / maxValue) * innerHeight;
+							const y =
+								GOROUTINE_CHART_HEIGHT - CHART_PADDING_Y - stackOffset - height;
+							stackOffset += height;
+
+							return (
+								<rect
+									key={`${String(point.timestamp)}-${serviceName}`}
+									x={x}
+									y={y}
+									width={barWidth}
+									height={height}
+									rx={Math.min(3, barWidth / 3)}
+									fill={getServiceColor(serviceName)}
+									opacity="0.88"
+								>
+									<title>{`${serviceName}: ${String(value)} (${point.time})`}</title>
+								</rect>
+							);
+						});
+					})}
+				</svg>
+			</div>
+
+			<div className="mt-3 flex items-center justify-between font-mono text-[11px] text-slate-400">
+				{labels.map((label) => (
+					<span
+						key={label.key}
+						className={cn(
+							label.align === "start" && "text-left",
+							label.align === "middle" && "text-center",
+							label.align === "end" && "text-right",
+						)}
+						style={{ width: "33%" }}
+					>
+						{label.label}
+					</span>
+				))}
+			</div>
+		</div>
+	);
+}
```

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/components/GoroutineChart.tsx b/admin-dashboard/frontend/src/features/stats/components/GoroutineChart.tsx
deleted file mode 100644
--- a/admin-dashboard/frontend/src/features/stats/components/GoroutineChart.tsx
+++ /dev/null
```

```diff
diff --git a/admin-dashboard/frontend/src/components/dashboard/SystemServiceStatusBadges.tsx b/admin-dashboard/frontend/src/components/dashboard/SystemServiceStatusBadges.tsx
--- a/admin-dashboard/frontend/src/components/dashboard/SystemServiceStatusBadges.tsx
+++ b/admin-dashboard/frontend/src/components/dashboard/SystemServiceStatusBadges.tsx
@@
 interface SystemServiceStatusBadgesProps {
-	services: SystemStats["serviceGoroutines"];
+	services: SystemStats["serviceRuntime"];
	getServiceColor: (name: string) => string;
 }
@@
+const formatRuntimeLabel = (
+	count: number,
+	metricKind: "goroutine" | "thread",
+) => `${count} ${metricKind === "thread" ? "threads" : "goroutines"}`;
+
 export const SystemServiceStatusBadges = ({
	services,
	getServiceColor,
 }: SystemServiceStatusBadgesProps) => (
@@
					<span className="text-slate-600 ml-1">
-						: {service.available ? service.goroutines : "OFFLINE"}
+						: {service.available
+							? formatRuntimeLabel(service.count, service.metricKind)
+							: service.error
+								? "ERROR"
+								: "OFFLINE"}
					</span>
				</Badge>
			))}
```

### 4-5. parser 호환성 테스트 추가

새 파일: `admin-dashboard/frontend/src/features/stats/lib/systemStats.test.ts`

```diff
diff --git a/admin-dashboard/frontend/src/features/stats/lib/systemStats.test.ts b/admin-dashboard/frontend/src/features/stats/lib/systemStats.test.ts
new file mode 100644
--- /dev/null
+++ b/admin-dashboard/frontend/src/features/stats/lib/systemStats.test.ts
@@
+import test from "node:test";
+import assert from "node:assert/strict";
+import { parseSystemStats } from "./systemStats";
+
+test("parseSystemStats supports new runtime contract", () => {
+	const parsed = parseSystemStats({
+		cpuUsage: 10,
+		memoryUsage: 20,
+		memoryTotal: 100,
+		memoryUsed: 20,
+		threadCount: 7,
+		totalGoGoroutines: 42,
+		totalRuntimeUnits: 49,
+		serviceRuntime: [
+			{ name: "admin-dashboard", count: 7, metricKind: "thread", available: true },
+			{ name: "hololive-bot", count: 42, metricKind: "goroutine", available: true },
+		],
+	});
+
+	assert.ok(parsed);
+	assert.equal(parsed?.threadCount, 7);
+	assert.equal(parsed?.totalGoGoroutines, 42);
+	assert.equal(parsed?.serviceRuntime[0]?.metricKind, "thread");
+});
+
+test("parseSystemStats still accepts legacy goroutine payload", () => {
+	const parsed = parseSystemStats({
+		cpuUsage: 10,
+		memoryUsage: 20,
+		memoryTotal: 100,
+		memoryUsed: 20,
+		goroutines: 7,
+		totalGoroutines: 42,
+		serviceGoroutines: [
+			{ name: "admin-dashboard", goroutines: 7, available: true },
+		],
+	});
+
+	assert.ok(parsed);
+	assert.equal(parsed?.threadCount, 7);
+	assert.equal(parsed?.serviceRuntime[0]?.count, 7);
+});
```

테스트 스크립트에도 추가합니다.

```diff
diff --git a/admin-dashboard/frontend/package.json b/admin-dashboard/frontend/package.json
--- a/admin-dashboard/frontend/package.json
+++ b/admin-dashboard/frontend/package.json
@@
-    "test": "npx tsx --test src/features/alarms/selectors.test.ts src/features/members/selectors.test.ts src/features/stats/selectors.test.ts src/components/streams/media.test.mjs src/api/blueprint-section-8.test.ts src/routes/route-definitions.test.ts",
+    "test": "npx tsx --test src/features/alarms/selectors.test.ts src/features/members/selectors.test.ts src/features/stats/selectors.test.ts src/features/stats/lib/systemStats.test.ts src/components/streams/media.test.mjs src/api/blueprint-section-8.test.ts src/routes/route-definitions.test.ts",
```

### 의사결정 이유

이 문제는 단순히 “이름만 잘못 붙었다”가 아닙니다.
현재 수치는 서로 다른 runtime model의 단위를 한데 합쳐서 보여주기 때문에, **운영 대시보드가 잘못된 의미의 숫자를 정확하게 그려 주는** 상태입니다.
이건 가장 위험한 종류의 observability 오류입니다.

---

## 5. frontend exact duplicate 코드가 아직 1개 남아 있음 (`media.ts`)

### 문제

다음 두 파일이 동일합니다.

- `admin-dashboard/frontend/src/components/streams/media.ts`
- `admin-dashboard/frontend/src/features/streams/lib/media.ts`

실제 import는 feature 쪽을 사용하고 있고, component 쪽 파일은 사실상 stale duplicate 입니다.

### 영향

- 수정 시 어느 파일이 source-of-truth인지 불명확
- dead code처럼 보여도 남아 있어 drift 가능
- AI-generated split residue 느낌이 남음

### 수정 diff

```diff
diff --git a/admin-dashboard/frontend/src/components/streams/media.ts b/admin-dashboard/frontend/src/components/streams/media.ts
deleted file mode 100644
--- a/admin-dashboard/frontend/src/components/streams/media.ts
+++ /dev/null
```

테스트 파일도 feature 경로 기준으로 명확히 남깁니다. 현재 테스트는 이미 `features/streams/lib/media.ts`를 보고 있으므로, 삭제만으로 충분합니다.

필요하면 테스트 파일 위치도 맞춥니다.

```diff
diff --git a/admin-dashboard/frontend/src/components/streams/media.test.mjs b/admin-dashboard/frontend/src/features/streams/lib/media.test.mjs
similarity index 100%
rename from admin-dashboard/frontend/src/components/streams/media.test.mjs
rename to admin-dashboard/frontend/src/features/streams/lib/media.test.mjs
```

그리고 `package.json` 테스트 스크립트 경로를 맞춥니다.

```diff
diff --git a/admin-dashboard/frontend/package.json b/admin-dashboard/frontend/package.json
--- a/admin-dashboard/frontend/package.json
+++ b/admin-dashboard/frontend/package.json
@@
-    "test": "npx tsx --test src/features/alarms/selectors.test.ts src/features/members/selectors.test.ts src/features/stats/selectors.test.ts src/features/stats/lib/systemStats.test.ts src/components/streams/media.test.mjs src/api/blueprint-section-8.test.ts src/routes/route-definitions.test.ts",
+    "test": "npx tsx --test src/features/alarms/selectors.test.ts src/features/members/selectors.test.ts src/features/stats/selectors.test.ts src/features/stats/lib/systemStats.test.ts src/features/streams/lib/media.test.mjs src/api/blueprint-section-8.test.ts src/routes/route-definitions.test.ts",
```

### 의사결정 이유

중복 파일은 “지금은 harmless”해 보여도, 다음 수정 때 거의 반드시 drift를 만듭니다.
특히 utility/helper 성격 파일은 한쪽만 고쳐도 바로 의미 차이가 납니다. 이런 파일은 남겨둘 이유가 없습니다.

---

## 6. cache mock의 strict / lenient 정책이 아직 일관되지 않음

### 문제

`hololive/hololive-shared/pkg/service/cache/mocks/client.go`에는 `Strict` 모드와 `NewLenientClient()`가 있지만, 실제 구현은 일관되지 않습니다.

예를 들어 아래 메서드는 lenient 모드에서도 여전히 무조건 panic 합니다.

- `Set`
- `MSet`
- `Del`
- `DelMany`
- `SAdd`
- `SRem`
- `HSet`
- `HMSet`
- `HDel`
- `Expire`

반면 다른 메서드는 lenient에서 zero-value / nil 반환을 합니다.

### 영향

- 테스트 작성자가 lenient mock을 믿고 써도 특정 write path에서 갑자기 panic
- mock semantics가 메서드별로 달라 예측 가능성이 떨어짐
- 테스트가 기능 실패가 아니라 mock 구현 디테일 때문에 깨짐

### 수정 diff

### 6-1. unset helper를 일관화

```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/mocks/client.go b/hololive/hololive-shared/pkg/service/cache/mocks/client.go
--- a/hololive/hololive-shared/pkg/service/cache/mocks/client.go
+++ b/hololive/hololive-shared/pkg/service/cache/mocks/client.go
@@
 func (m *Client) panicIfUnset(name string) {
	if m != nil && m.Strict {
		panic("cache mock: " + name + " not set")
	}
 }
+
+func (m *Client) unsetError(name string) error {
+	m.panicIfUnset(name)
+	return nil
+}
+
+func (m *Client) unsetInt64(name string) (int64, error) {
+	m.panicIfUnset(name)
+	return 0, nil
+}
```

### 6-2. write method 전체를 helper 사용으로 통일

```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/mocks/client.go b/hololive/hololive-shared/pkg/service/cache/mocks/client.go
--- a/hololive/hololive-shared/pkg/service/cache/mocks/client.go
+++ b/hololive/hololive-shared/pkg/service/cache/mocks/client.go
@@
 func (m *Client) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m.SetFunc != nil {
		return m.SetFunc(ctx, key, value, ttl)
	}
-	panic("cache mock: SetFunc not set")
+	return m.unsetError("SetFunc")
 }
@@
 func (m *Client) MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error {
	if m.MSetFunc != nil {
		return m.MSetFunc(ctx, pairs, ttl)
	}
-	panic("cache mock: MSetFunc not set")
+	return m.unsetError("MSetFunc")
 }
@@
 func (m *Client) Del(ctx context.Context, key string) error {
	if m.DelFunc != nil {
		return m.DelFunc(ctx, key)
	}
-	panic("cache mock: DelFunc not set")
+	return m.unsetError("DelFunc")
 }
@@
 func (m *Client) DelMany(ctx context.Context, keys []string) (int64, error) {
	if m.DelManyFunc != nil {
		return m.DelManyFunc(ctx, keys)
	}
-	panic("cache mock: DelManyFunc not set")
+	return m.unsetInt64("DelManyFunc")
 }
@@
 func (m *Client) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if m.SAddFunc != nil {
		return m.SAddFunc(ctx, key, members)
	}
-	panic("cache mock: SAddFunc not set")
+	return m.unsetInt64("SAddFunc")
 }
@@
 func (m *Client) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if m.SRemFunc != nil {
		return m.SRemFunc(ctx, key, members)
	}
-	panic("cache mock: SRemFunc not set")
+	return m.unsetInt64("SRemFunc")
 }
@@
 func (m *Client) HSet(ctx context.Context, key, field, value string) error {
	if m.HSetFunc != nil {
		return m.HSetFunc(ctx, key, field, value)
	}
-	panic("cache mock: HSetFunc not set")
+	return m.unsetError("HSetFunc")
 }
@@
 func (m *Client) HMSet(ctx context.Context, key string, fields map[string]any) error {
	if m.HMSetFunc != nil {
		return m.HMSetFunc(ctx, key, fields)
	}
-	panic("cache mock: HMSetFunc not set")
+	return m.unsetError("HMSetFunc")
 }
@@
 func (m *Client) HDel(ctx context.Context, key string, fields ...string) error {
	if m.HDelFunc != nil {
		return m.HDelFunc(ctx, key, fields...)
	}
-	panic("cache mock: HDelFunc not set")
+	return m.unsetError("HDelFunc")
 }
@@
 func (m *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if m.ExpireFunc != nil {
		return m.ExpireFunc(ctx, key, ttl)
	}
-	panic("cache mock: ExpireFunc not set")
+	return m.unsetError("ExpireFunc")
 }
```

### 6-3. write-path 테스트 추가

```diff
diff --git a/hololive/hololive-shared/pkg/service/cache/mocks/client_test.go b/hololive/hololive-shared/pkg/service/cache/mocks/client_test.go
--- a/hololive/hololive-shared/pkg/service/cache/mocks/client_test.go
+++ b/hololive/hololive-shared/pkg/service/cache/mocks/client_test.go
@@
 func TestClientReadMethodsPanicWhenStrict(t *testing.T) {
@@
 }
+
+func TestClientWriteMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
+	client := NewLenientClient()
+
+	if err := client.Set(context.Background(), "k", "v", time.Second); err != nil {
+		t.Fatalf("Set() error = %v, want nil", err)
+	}
+	if err := client.Del(context.Background(), "k"); err != nil {
+		t.Fatalf("Del() error = %v, want nil", err)
+	}
+	if added, err := client.SAdd(context.Background(), "rooms", []string{"r1"}); err != nil || added != 0 {
+		t.Fatalf("SAdd() = (%v, %v), want (0, nil)", added, err)
+	}
+	if removed, err := client.DelMany(context.Background(), []string{"k1", "k2"}); err != nil || removed != 0 {
+		t.Fatalf("DelMany() = (%v, %v), want (0, nil)", removed, err)
+	}
+}
```

### 의사결정 이유

mock은 production correctness보다 **테스트 semantics consistency**가 더 중요합니다.
lenient 모드라면 “안 설정된 메서드는 panic이 아니라 no-op / zero-value”라는 규칙이 모든 메서드에 동일하게 적용되어야 합니다.

---

## 7. Admin Dashboard와 Bot 사이의 API key wiring이 아직 조용히 어긋날 수 있음

### 문제

`docker-compose.prod.yml`에서 admin-dashboard는 `HOLO_BOT_API_KEY`를 별도로 받습니다.

```yaml
HOLO_BOT_API_KEY: ${HOLO_BOT_API_KEY:-}
```

하지만 bot 쪽 보호 API는 `API_SECRET_KEY`(server API key) 기준으로 동작합니다.
현재 admin-dashboard backend `Config::load()`도 `HOLO_BOT_API_KEY`만 보고 `API_SECRET_KEY` alias를 보지 않습니다.

즉 `.env`에 bot용 `API_SECRET_KEY`만 있고 `HOLO_BOT_API_KEY`가 비어 있으면, dashboard ↔ bot 통신이 조용히 깨질 수 있습니다.

### 영향

- 운영자가 bot API key를 설정했는데 dashboard만 401/403
- compose는 정상처럼 보여도 내부 인증만 실패
- same secret를 두 이름으로 관리해야 해서 drift 가능성 증가

### 수정 diff

### 7-1. backend config에 optional alias helper 추가

```diff
diff --git a/admin-dashboard/backend/src/config/env.rs b/admin-dashboard/backend/src/config/env.rs
--- a/admin-dashboard/backend/src/config/env.rs
+++ b/admin-dashboard/backend/src/config/env.rs
@@
 pub fn required_alias(keys: &[&str]) -> Result<String> {
@@
 }
+
+pub fn optional_alias(keys: &[&str]) -> Option<String> {
+    keys.iter().find_map(|key| {
+        env::var(key)
+            .ok()
+            .map(|value| value.trim().to_string())
+            .filter(|value| !value.is_empty())
+    })
+}
```

### 7-2. `Config::load()`에서 `API_SECRET_KEY` fallback을 허용

```diff
diff --git a/admin-dashboard/backend/src/config.rs b/admin-dashboard/backend/src/config.rs
--- a/admin-dashboard/backend/src/config.rs
+++ b/admin-dashboard/backend/src/config.rs
@@
-use self::env::{env_bool, env_int, env_string, required_alias};
+use self::env::{env_bool, env_int, env_string, optional_alias, required_alias};
@@
-            holo_bot_api_key: env_string("HOLO_BOT_API_KEY", ""),
+            holo_bot_api_key: optional_alias(&["HOLO_BOT_API_KEY", "API_SECRET_KEY"])
+                .unwrap_or_default(),
```

### 7-3. alias 테스트 추가

```diff
diff --git a/admin-dashboard/backend/src/config.rs b/admin-dashboard/backend/src/config.rs
--- a/admin-dashboard/backend/src/config.rs
+++ b/admin-dashboard/backend/src/config.rs
@@
     fn test_openapi_defaults_disabled_in_production() {
@@
     }
+
+    #[test]
+    fn test_holo_bot_api_key_falls_back_to_api_secret_key() {
+        with_env_vars(
+            &[
+                ("ADMIN_PASS_HASH", Some("hash-primary")),
+                ("ADMIN_PASS_BCRYPT", None),
+                ("SESSION_SECRET", Some("session-secret")),
+                ("ADMIN_SECRET_KEY", None),
+                ("HOLO_BOT_API_KEY", None),
+                ("API_SECRET_KEY", Some("shared-secret")),
+            ],
+            || {
+                let cfg = Config::load().expect("config load");
+                assert_eq!(cfg.holo_bot_api_key, "shared-secret");
+            },
+        );
+    }
 }
```

### 7-4. docs도 같이 수정

```diff
diff --git a/admin-dashboard/README.md b/admin-dashboard/README.md
--- a/admin-dashboard/README.md
+++ b/admin-dashboard/README.md
@@
-| `HOLO_BOT_API_KEY` | 업스트림 내부 인증 헤더 값 | 빈 값 |
+| `HOLO_BOT_API_KEY` | 업스트림 내부 인증 헤더 값 (`API_SECRET_KEY` fallback 지원) | 빈 값 |
```

### 의사결정 이유

운영 시크릿은 이름이 둘 이상이면 거의 반드시 drift가 납니다.
특히 이번 경우는 두 값이 보안적으로 같은 의미를 갖는데 이름만 다릅니다.
alias fallback을 둬서 운영 실수를 흡수하는 것이 맞습니다.

---

## 8. LOC gate는 생겼지만, 아직도 “새 대형 파일이 threshold 밖에서 자라는 문제”를 완전히 막지 못함

### 문제

`check-file-loc.sh` / `check-go-module-loc.sh`는 **threshold 파일에 적힌 파일만 검사**합니다.
즉 threshold에 아직 등록되지 않은 대형 파일은 계속 커져도 gate가 감지하지 못합니다.

현재 non-test 기준으로 threshold 밖에 남아 있는 큰 파일 예시는 아래입니다.

- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt.go` (710)
- `hololive/hololive-llm-sched/internal/service/majorevent/repository.go` (629)
- `hololive/hololive-shared/pkg/service/holodex/service_channels.go` (598)
- `hololive/hololive-shared/pkg/service/auth/service.go` (594)
- `hololive/hololive-kakao-bot-go/internal/adapter/formatter_alarm.go` (547)
- `hololive/hololive-shared/pkg/server/stream_handler.go` (540)
- `hololive/hololive-kakao-bot-go/scripts/bot.sh` (528)
- `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go` (497)
- `hololive/hololive-shared/pkg/domain/template_sample_data.go` (497)
- `hololive/hololive-kakao-bot-go/internal/service/acl/service.go` (495)
- `hololive/hololive-llm-sched/internal/service/membernews/summarizer/summarizer.go` (477)
- `hololive/hololive-shared/pkg/service/holodex/api_client.go` (475)

### 영향

- governance가 “일부 파일만 관리하는 문서”로 남음
- 새로 커진 파일이 CI를 통과
- 구조 개선이 아니라 threshold 등록 누락 싸움이 됨

### 수정 diff

### 8-1. `check-file-loc.sh`를 기본 ceiling + explicit threshold 방식으로 강화

```diff
diff --git a/scripts/architecture/check-file-loc.sh b/scripts/architecture/check-file-loc.sh
--- a/scripts/architecture/check-file-loc.sh
+++ b/scripts/architecture/check-file-loc.sh
@@
 THRESHOLD_FILE="${1:-${ROOT_DIR}/docs/architecture/file-loc-thresholds.txt}"
+GO_THRESHOLD_FILE="${ROOT_DIR}/docs/architecture/go-module-loc-thresholds.txt"
+DEFAULT_MAX_LINES="${DEFAULT_MAX_LINES:-400}"
@@
-violations=()
-reports=()
+declare -A configured=()
+violations=()
+reports=()
+
+load_threshold_file() {
+  local threshold_file="$1"
+  while IFS= read -r line || [[ -n "${line}" ]]; do
+    trimmed="$(echo "${line}" | sed 's/[[:space:]]*$//')"
+    if [[ -z "${trimmed}" || "${trimmed}" =~ ^[[:space:]]*# ]]; then
+      continue
+    fi
+
+    path="${trimmed%%:*}"
+    max="${trimmed##*:}"
+    path="$(echo "${path}" | xargs)"
+    max="$(echo "${max}" | xargs)"
+
+    if [[ -z "${path}" || -z "${max}" || ! "${max}" =~ ^[0-9]+$ ]]; then
+      echo "error: invalid threshold line: ${line}" >&2
+      exit 1
+    fi
+
+    configured["${path}"]="${max}"
+  done < "${threshold_file}"
+}
+
+load_threshold_file "${THRESHOLD_FILE}"
+if [[ -f "${GO_THRESHOLD_FILE}" ]]; then
+  load_threshold_file "${GO_THRESHOLD_FILE}"
+fi
-
-while IFS= read -r line || [[ -n "${line}" ]]; do
-  trimmed="$(echo "${line}" | sed 's/[[:space:]]*$//')"
-  if [[ -z "${trimmed}" || "${trimmed}" =~ ^[[:space:]]*# ]]; then
-    continue
-  fi
-
-  path="${trimmed%%:*}"
-  max="${trimmed##*:}"
-  path="$(echo "${path}" | xargs)"
-  max="$(echo "${max}" | xargs)"
-
-  if [[ -z "${path}" || -z "${max}" || ! "${max}" =~ ^[0-9]+$ ]]; then
-    echo "error: invalid threshold line: ${line}" >&2
-    exit 1
-  fi
-
-  abs_path="${ROOT_DIR}/${path}"
+for path in "${!configured[@]}"; do
+  max="${configured[${path}]}"
+  abs_path="${ROOT_DIR}/${path}"
   if [[ ! -f "${abs_path}" ]]; then
     violations+=("missing:${path}")
     continue
@@
   if (( count > max )); then
     violations+=("exceeded:${path}:${count}>${max}")
   fi
-done < "${THRESHOLD_FILE}"
+done
+
+while IFS= read -r file; do
+  rel="${file#${ROOT_DIR}/}"
+
+  case "${rel}" in
+    .worktrees/*|artifacts/*|logs/*|*/node_modules/*|*/dist/*|*/coverage/*|*/target/*|*/generated/*|*.test.ts|*.test.tsx|*.test.mjs|*_test.go)
+      continue
+      ;;
+  esac
+
+  if [[ -n "${configured[${rel}]:-}" ]]; then
+    continue
+  fi
+
+  count="$(wc -l < "${file}" | tr -d '[:space:]')"
+  if (( count > DEFAULT_MAX_LINES )); then
+    violations+=("missing-threshold:${rel}:${count}>${DEFAULT_MAX_LINES}")
+  fi
+done < <(find "${ROOT_DIR}" \
+  -type f \( -name '*.go' -o -name '*.rs' -o -name '*.ts' -o -name '*.tsx' -o -name '*.sh' \))
```


### 8-2. 현재 이미 큰 파일들은 threshold에 즉시 등록

```diff
diff --git a/docs/architecture/go-module-loc-thresholds.txt b/docs/architecture/go-module-loc-thresholds.txt
--- a/docs/architecture/go-module-loc-thresholds.txt
+++ b/docs/architecture/go-module-loc-thresholds.txt
@@
 hololive/hololive-shared/pkg/service/youtube/scheduler.go:1150
 hololive/hololive-shared/pkg/service/youtube/service.go:950
 hololive/hololive-shared/pkg/service/member/repository.go:850
 hololive/hololive-shared/pkg/service/holodex/scraper.go:750
 hololive/hololive-shared/pkg/service/youtube/poller/pollers.go:700
+hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_prompt.go:720
+hololive/hololive-llm-sched/internal/service/majorevent/repository.go:650
+hololive/hololive-shared/pkg/service/holodex/service_channels.go:620
+hololive/hololive-shared/pkg/service/auth/service.go:620
+hololive/hololive-kakao-bot-go/internal/adapter/formatter_alarm.go:580
+hololive/hololive-shared/pkg/server/stream_handler.go:560
+hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go:520
+hololive/hololive-shared/pkg/domain/template_sample_data.go:520
+hololive/hololive-kakao-bot-go/internal/service/acl/service.go:520
+hololive/hololive-llm-sched/internal/service/membernews/summarizer/summarizer.go:500
+hololive/hololive-shared/pkg/service/holodex/api_client.go:500
```

```diff
diff --git a/docs/architecture/file-loc-thresholds.txt b/docs/architecture/file-loc-thresholds.txt
--- a/docs/architecture/file-loc-thresholds.txt
+++ b/docs/architecture/file-loc-thresholds.txt
@@
 scripts/logs/logs.sh:760
+hololive/hololive-kakao-bot-go/scripts/bot.sh:560
```

### 8-3. 구조 분리 우선순위도 같이 명시

이건 threshold만 두고 끝내지 말고, 아래 순서로 파일을 쪼개는 것이 맞습니다.

1. `auth/service.go`
   - `service_user.go` (`Register`, user query)
   - `service_session.go` (`Login`, `Logout`, `Refresh`, `validateSession`, `createSession`, `revokeAllSessions`)
   - `service_password_reset.go`
   - `service_rate_limit.go`

2. `majorevent/repository.go`
   - `repository_subscription.go`
   - `repository_schema.go`
   - `repository_events.go`

3. `summarizer_prompt.go`
   - `prompt_context.go`
   - `prompt_schema.go`
   - `prompt_render.go`

4. `stream_handler.go`
   - `stream_handler_streams.go`
   - `stream_handler_stats.go`
   - `stream_handler_members.go`
   - `stream_handler_channels.go`

5. `formatter_alarm.go`
   - `formatter_alarm_list.go`
   - `formatter_alarm_notification.go`
   - `formatter_milestone.go`

### 의사결정 이유

여기서 핵심은 “큰 파일이 있다는 사실”보다, **큰 파일이 threshold 밖에서 자랄 수 있다는 governance 구멍**입니다.
즉 immediate fix는 gate 강화이고, 그 위에서 실제 분해를 진행해야 합니다.

---

## 9. thin wrapper / trivial provider residue가 아직 일부 남아 있음

### 문제

현재 코드베이스는 많이 좋아졌지만, 아직 몇몇 provider/wrapper는 사실상 의미가 없습니다.

대표 예시:

- `hololive/hololive-shared/pkg/providers/infra_providers.go`
  - `ProvideValkeyConfig`
  - `ProvidePostgresConfig`
  - `ProvideCacheService`
  - `ProvidePostgresService`
- `hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go`
  - `ProvideFetchProfilesLogger`
  - `ProvideFetchProfilesHTTPClient`

이 함수들은 조립 복잡도를 낮추지 않고, 오히려 “값 꺼내기 / 그대로 넘기기”를 provider 이름으로 포장합니다.

### 영향

- composition root 가독성 저하
- 함수 개수는 늘지만 의미는 거의 늘지 않음
- AI-generated provider ceremony 느낌이 남음

### 수정 diff

### 9-1. 불필요한 config/service extractor 삭제

```diff
diff --git a/hololive/hololive-shared/pkg/providers/infra_providers.go b/hololive/hololive-shared/pkg/providers/infra_providers.go
--- a/hololive/hololive-shared/pkg/providers/infra_providers.go
+++ b/hololive/hololive-shared/pkg/providers/infra_providers.go
@@
-// ProvideValkeyConfig - 설정에서 Valkey 캐시 설정 추출
-func ProvideValkeyConfig(cfg *config.Config) config.ValkeyConfig {
-	return cfg.Valkey
-}
-
-// ProvidePostgresConfig - 설정에서 PostgreSQL 설정 추출
-func ProvidePostgresConfig(cfg *config.Config) config.PostgresConfig {
-	return cfg.Postgres
-}
@@
-// ProvideCacheService - 캐시 리소스에서 서비스 추출
-func ProvideCacheService(resources *CacheResources) cache.Client {
-	return resources.Service
-}
@@
-// ProvidePostgresService - 데이터베이스 리소스에서 서비스 추출
-func ProvidePostgresService(resources *DatabaseResources) database.Client {
-	return resources.Service
-}
```

> 적용 시 `config` import가 사용되지 않으면 정리하십시오.

### 9-2. caller를 직접 참조로 바꿈

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
@@
-	postgresConfig := providers.ProvidePostgresConfig(cfg)
-
-	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, postgresConfig, logger)
+	databaseResources, cleanupDB, err := providers.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
@@
-	postgresService := providers.ProvidePostgresService(databaseResources)
+	postgresService := databaseResources.Service
	memberRepository := providers.ProvideMemberRepository(postgresService, logger)

-	valkeyConfig := providers.ProvideValkeyConfig(cfg)
-
-	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, valkeyConfig, logger)
+	cacheResources, cleanupCache, err := providers.ProvideCacheResources(ctx, cfg.Valkey, logger)
@@
-	cacheService := providers.ProvideCacheService(cacheResources)
+	cacheService := cacheResources.Service
@@
-	postgresService := providers.ProvidePostgresService(databaseResources)
+	postgresService := databaseResources.Service
```

```diff
diff --git a/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go b/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go
--- a/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go
+++ b/hololive/hololive-llm-sched/internal/app/bootstrap_llm_scheduler.go
@@
-	cacheService := providers.ProvideCacheService(cacheResources)
+	cacheService := cacheResources.Service
@@
-	postgresService := providers.ProvidePostgresService(databaseResources)
+	postgresService := databaseResources.Service
```

### 9-3. single-consumer provider도 제거

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go b/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go
--- a/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer.go
@@
-// ProvideFetchProfilesLogger - fetch_profiles 전용 로거.
-func ProvideFetchProfilesLogger() (*slog.Logger, func(), error) {
-	logger := slog.Default()
-	cleanup := func() {} // slog는 Sync 필요 없음
-
-	return logger, cleanup, nil
-}
-
-// ProvideFetchProfilesHTTPClient - fetch_profiles 전용 HTTP 클라이언트.
-func ProvideFetchProfilesHTTPClient() *http.Client {
-	return httputil.NewExternalAPIClient(constants.OfficialProfileConfig.RequestTimeout)
-}
```

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core_tools.go
@@
 func InitializeFetchProfilesRuntime(_ context.Context) (*FetchProfilesRuntime, func(), error) {
-	logger, cleanupLogger, err := ProvideFetchProfilesLogger()
-	if err != nil {
-		return nil, nil, fmt.Errorf("provide fetch profiles logger: %w", err)
-	}
-
-	httpClient := ProvideFetchProfilesHTTPClient()
+	logger := slog.Default()
+	cleanupLogger := func() {}
+	httpClient := httputil.NewExternalAPIClient(constants.OfficialProfileConfig.RequestTimeout)

	runtime := &FetchProfilesRuntime{
		Logger:     logger,
		HTTPClient: httpClient,
	}
```

그리고 관련 테스트도 정리합니다.

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer_additional_test.go b/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer_additional_test.go
deleted file mode 100644
--- a/hololive/hololive-kakao-bot-go/internal/app/providers_single_consumer_additional_test.go
+++ /dev/null
```

대신 `bootstrap_core_tools_test.go` 성격의 테스트로 옮깁니다.

### 의사결정 이유

DI 라이브러리를 안 넣고도 조립이 읽히려면, provider는 “실제 조립 의미가 있는 경우”에만 남아야 합니다.
지금 남은 residue는 그 기준에 못 미칩니다.

---

## 10. 적용 우선순위

이번 추가 항목은 아래 순서로 닫는 것이 가장 안전합니다.

1. **review bundle / tracked local artifact 차단**
   - 외부 리뷰/배포 오염을 바로 막아야 함

2. **OpenAPI export binary 복구 + stale docs 정리**
   - frontend CI / generated client 파이프라인이 다시 한 줄로 닫힘

3. **Swagger 공개 노출 차단**
   - 보안 표면 축소

4. **system stats contract 정정**
   - observability 의미론 복구

5. **duplicate media helper 삭제**
   - 작은 비용으로 stale duplicate 제거

6. **cache mock strict/lenient 일관화**
   - 테스트 신뢰도 회복

7. **bot API key alias fallback**
   - 운영 drift 방지

8. **LOC gate 강화**
   - 구조 퇴행 방지

9. **thin wrapper residue 제거**
   - AI smell 후처리

---

## 최종 평가

이번 업로드본은 이전보다 분명히 좋아졌습니다.
하지만 지금 남아 있는 문제는 더 이상 “핵심 기능이 아예 틀렸다” 수준은 아니고, 다음 단계의 문제들입니다.

- review / CI / docs / generated pipeline 정합성
- observability contract 의미론
- 남은 thin-wrapper / duplicate residue
- governance의 마지막 구멍

즉 이제는 “대형 구조 실패”보다는 **마감 직전의 운영/정합성/유지보수성 문제**가 중심입니다.
이번 문서는 바로 그 잔존 항목만 diff 수준으로 닫는 보강안입니다.
