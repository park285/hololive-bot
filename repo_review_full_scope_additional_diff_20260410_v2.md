# Repo-wide 추가 리뷰 및 잔존 이슈 diff 청사진 (2026-04-10, 최신 업로드본 기준)

## 전제

이번 문서는 최신 업로드본(`hololive-bot-full-20260410T003842Z.tar.gz` 계열) 기준의 **repo-wide 정적 코드 검토** 결과입니다. 스코프를 줄이지 않았습니다. Go 런타임, `hololive-shared`, `shared-go`, `admin-dashboard`(backend/frontend), CI·architecture scripts, 문서·가드 스크립트까지 전부 다시 확인했습니다.

이번 문서에는 **이미 해결된 항목을 반복해서 넣지 않았습니다.** 즉, 아래 내용은 “이전 리뷰에서 지적했지만 지금은 해결된 것”이 아니라, **이번 업로드본에도 실제로 남아 있는 잔존 이슈 전부**입니다.

## 재검증 결과, 이번 문서에서 제외한 항목

아래 항목은 최신 업로드본에서 해결된 것으로 재확인했습니다.

- 알람 target minute 동적 재주입
- scraper polling env 전파
- worker channel full 시 임의 `+10초` 지연
- settlement runtime 제거
- `.dockerignore`의 `.worktrees/` 누락
- `MemberDataProvider` multi-result stub
- scraper budget warning 부재
- admin-dashboard blind proxy 라우트
- OpenAPI drift CI 미강제
- `hololive-shared/internal/logging` 이중화

즉, 이번 문서는 위 항목을 다시 반복하지 않습니다.

---

## 최종 결론

이번 수정본은 이전보다 분명히 좋아졌습니다. 다만 아직 저장소 전체 기준으로는 다음 네 축의 잔존 문제가 남아 있습니다.

1. **계약 의미 보존 미완료**: admin-dashboard backend가 typed contract를 도입했지만, upstream 4xx/5xx를 충분히 구분하지 못합니다.
2. **죽은 코드/잔존 어댑터**: SSR 주입 레이어, `holoClient.ts`, thin alias/wrapper가 여전히 남아 있습니다.
3. **SSOT 단일화 미완료**: `envutil`, `logging`, 일부 alarm helper가 아직 중복 또는 compatibility wrapper를 유지합니다.
4. **거버넌스/운영 품질 미완료**: project-map gate 미연결, Go 전용 LOC gate, Rust workflow 중복 path, panic 기반 startup이 남아 있습니다.

이 문서는 위 네 축을 **파일별 diff 수준**으로 닫는 청사진입니다.

---

# 1. Admin dashboard backend: upstream 4xx가 502로 붕괴되는 문제

## 문제

현재 `admin-dashboard/backend/src/holo/client.rs`의 `HoloApiClient::request()`는 다음과 같이 동작합니다.

- 네트워크 실패 → `ProxyError::Unavailable` → 502
- upstream 5xx → `ProxyError::Unavailable` → 502
- 그 외 모든 상태코드 → 응답 바디를 성공 타입 `T`로 역직렬화 시도
- 역직렬화 실패 → `ProxyError::Unavailable` → 502

즉 upstream이 400/404 같은 **정상적인 client error**를 JSON 에러 바디로 돌려줘도, backend는 성공 응답 타입으로 파싱하려다가 실패하고 **결국 502**로 바꿔버립니다.

typed holo contract를 도입했는데, 정작 에러 의미가 사라집니다.

## 영향

- 프런트엔드는 사용자 입력 오류와 upstream 장애를 구분할 수 없습니다.
- OpenAPI는 success-biased contract가 됩니다.
- 운영 시 4xx/5xx 관찰성이 나빠집니다.

## 수정 파일

- `admin-dashboard/backend/src/error.rs`
- `admin-dashboard/backend/src/holo/client.rs`
- `admin-dashboard/backend/src/holo/handlers.rs` 또는 분리 후 각 handler 모듈
- `admin-dashboard/backend/src/openapi.rs`
- `admin-dashboard/backend/src/holo/client.rs` 테스트

## 코드 개선안

### 1-1. 공통 에러 DTO 추가

```diff
--- a/admin-dashboard/backend/src/error.rs
+++ b/admin-dashboard/backend/src/error.rs
@@
 use axum::Json;
 use axum::http::StatusCode;
 use axum::response::{IntoResponse, Response};
+use serde::{Deserialize, Serialize};
 use serde_json::json;
@@
 #[derive(Debug, thiserror::Error)]
 pub enum ProxyError {
     #[error("upstream unavailable")]
     Unavailable,
+
+    #[error("upstream returned {status}")]
+    Upstream {
+        status: StatusCode,
+        body: ErrorResponse,
+    },
 }
+
+#[derive(Debug, Clone, Serialize, Deserialize, utoipa::ToSchema)]
+pub struct ErrorResponse {
+    pub error: String,
+    #[serde(skip_serializing_if = "Option::is_none")]
+    pub code: Option<String>,
+    #[serde(skip_serializing_if = "Option::is_none")]
+    pub details: Option<serde_json::Value>,
+}
+
+impl ErrorResponse {
+    pub fn simple(message: impl Into<String>) -> Self {
+        Self {
+            error: message.into(),
+            code: None,
+            details: None,
+        }
+    }
+}
@@
             Self::Proxy(e) => match e {
                 ProxyError::Unavailable => (
                     StatusCode::BAD_GATEWAY,
                     json!({"error": "Service unavailable"}),
                 ),
+                ProxyError::Upstream { status, body } => {
+                    return (*status, Json(body)).into_response();
+                }
             },
```

### 1-2. `HoloApiClient`가 non-success를 보존하도록 수정

```diff
--- a/admin-dashboard/backend/src/holo/client.rs
+++ b/admin-dashboard/backend/src/holo/client.rs
@@
-use crate::error::{AppError, ProxyError};
+use crate::error::{AppError, ErrorResponse, ProxyError};
@@
         let response = request
             .send()
             .await
             .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
         let status = response.status();
         if status.is_server_error() {
             return Err(AppError::Proxy(ProxyError::Unavailable));
         }
 
         let bytes = response
             .bytes()
             .await
             .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
+
+        if !status.is_success() {
+            let body = serde_json::from_slice::<ErrorResponse>(&bytes)
+                .unwrap_or_else(|_| ErrorResponse::simple(status.canonical_reason().unwrap_or("Upstream request failed")));
+
+            return Err(AppError::Proxy(ProxyError::Upstream { status, body }));
+        }
+
         let parsed = serde_json::from_slice::<T>(&bytes)
             .map_err(|_| AppError::Proxy(ProxyError::Unavailable))?;
 
         Ok((status, parsed))
     }
 }
```

### 1-3. 테스트 추가

```diff
--- a/admin-dashboard/backend/src/holo/client.rs
+++ b/admin-dashboard/backend/src/holo/client.rs
@@
     use serde_json::{Value, json};
@@
     async fn spawn_server(capture: Arc<Mutex<Capture>>) -> String {
         let app = Router::new()
@@
             .route(
+                "/api/holo/members/bad-request",
+                get(|| async {
+                    (
+                        reqwest::StatusCode::BAD_REQUEST,
+                        Json(json!({"error": "invalid member id", "code": "INVALID_MEMBER_ID"})),
+                    )
+                }),
+            )
+            .route(
+                "/api/holo/members/not-found-invalid-body",
+                get(|| async {
+                    (
+                        reqwest::StatusCode::NOT_FOUND,
+                        Json(json!({"unexpected": true})),
+                    )
+                }),
+            )
+            .route(
                 "/api/holo/rooms",
@@
     async fn test_send_serializes_json_body() {
@@
     }
+
+    #[tokio::test]
+    async fn test_get_preserves_upstream_bad_request() {
+        let capture = Arc::new(Mutex::new(Capture::default()));
+        let base_url = spawn_server(Arc::clone(&capture)).await;
+        let client = HoloApiClient::new(&base_url, None).unwrap();
+
+        let err = client
+            .get::<serde_json::Value>("/api/holo/members/bad-request", None)
+            .await
+            .expect_err("expected bad request passthrough");
+
+        match err {
+            AppError::Proxy(ProxyError::Upstream { status, body }) => {
+                assert_eq!(status, reqwest::StatusCode::BAD_REQUEST);
+                assert_eq!(body.error, "invalid member id");
+                assert_eq!(body.code.as_deref(), Some("INVALID_MEMBER_ID"));
+            }
+            other => panic!("unexpected error: {other:?}"),
+        }
+    }
+
+    #[tokio::test]
+    async fn test_get_non_success_with_unknown_body_falls_back_to_generic_error() {
+        let capture = Arc::new(Mutex::new(Capture::default()));
+        let base_url = spawn_server(Arc::clone(&capture)).await;
+        let client = HoloApiClient::new(&base_url, None).unwrap();
+
+        let err = client
+            .get::<serde_json::Value>("/api/holo/members/not-found-invalid-body", None)
+            .await
+            .expect_err("expected not found passthrough");
+
+        match err {
+            AppError::Proxy(ProxyError::Upstream { status, body }) => {
+                assert_eq!(status, reqwest::StatusCode::NOT_FOUND);
+                assert!(!body.error.trim().is_empty());
+            }
+            other => panic!("unexpected error: {other:?}"),
+        }
+    }
 }
```

### 1-4. OpenAPI 에러 응답 추가

`utoipa::path`에 공통 에러 응답을 넣어야 합니다. 이 부분은 아래 10번의 handler 분리와 함께 적용하는 것이 가장 깔끔합니다.

예시:

```rust
responses(
    (status = 200, description = "Members list", body = MembersResponse),
    (status = 401, description = "Unauthorized", body = ErrorResponse),
    (status = 400, description = "Bad request", body = ErrorResponse),
    (status = 502, description = "Upstream unavailable", body = ErrorResponse),
)
```

---

# 2. Admin dashboard SSR/injector 레이어는 되살릴 대상이 아니라 삭제 대상

## 문제

현재 backend에는 `admin-dashboard/backend/src/ssr/injector.rs`가 남아 있고, frontend에는 `window.__SSR_DATA__` 기반 유틸과 훅이 남아 있습니다.

하지만 실제 라우팅 경로를 보면, backend는 static `index.html`만 서빙하며 SSR injector를 쓰지 않습니다. 즉 **backend SSR 코드는 죽은 코드**이고, frontend의 SSR 초기 데이터 경로도 실제로는 공급되지 않습니다.

## 영향

- backend에 쓰이지 않는 HTML 조작 코드가 남습니다.
- frontend hook이 실제 공급되지 않는 초기 데이터를 기대합니다.
- 문서/AGENTS가 잘못된 구조를 설명하게 됩니다.

## 수정 파일

- 삭제: `admin-dashboard/backend/src/ssr/mod.rs`
- 삭제: `admin-dashboard/backend/src/ssr/injector.rs`
- 수정: `admin-dashboard/backend/src/lib.rs`
- 수정: `admin-dashboard/backend/src/main.rs`
- 삭제: `admin-dashboard/frontend/src/utils/ssr.ts`
- 삭제: `admin-dashboard/frontend/src/hooks/useSSRData.ts`
- 수정: `admin-dashboard/frontend/src/hooks/index.ts`
- 수정: `admin-dashboard/frontend/src/features/members/hooks/useMembersPage.ts`
- 수정: `admin-dashboard/frontend/src/features/settings/pages/SettingsPage.tsx`
- 수정: `admin-dashboard/AGENTS.md`

## 코드 개선안

### 2-1. backend SSR 모듈 삭제

```diff
--- a/admin-dashboard/backend/src/lib.rs
+++ b/admin-dashboard/backend/src/lib.rs
@@
-pub mod ssr;
```

```diff
--- a/admin-dashboard/backend/src/main.rs
+++ b/admin-dashboard/backend/src/main.rs
@@
-mod ssr;
```

그리고 파일 삭제:

```text
DELETE admin-dashboard/backend/src/ssr/mod.rs
DELETE admin-dashboard/backend/src/ssr/injector.rs
```

### 2-2. frontend SSR 훅 삭제

```diff
--- a/admin-dashboard/frontend/src/hooks/index.ts
+++ b/admin-dashboard/frontend/src/hooks/index.ts
@@
-export { useSSRData } from "@/hooks/useSSRData";
```

```text
DELETE admin-dashboard/frontend/src/hooks/useSSRData.ts
DELETE admin-dashboard/frontend/src/utils/ssr.ts
```

### 2-3. Members 페이지에서 SSR initialData 제거

```diff
--- a/admin-dashboard/frontend/src/features/members/hooks/useMembersPage.ts
+++ b/admin-dashboard/frontend/src/features/members/hooks/useMembersPage.ts
@@
-import { useSSRData } from "@/hooks/useSSRData";
@@
-    const ssrInitialData = useSSRData("members", (data) =>
-        data?.status === "ok" && data.members
-            ? (data as MembersResponse)
-            : undefined,
-    );
-
     const query = useQuery({
         queryKey: queryKeys.members.all,
         queryFn: membersApi.getAll,
-        initialData: ssrInitialData,
     });
```

불필요해진 import도 제거합니다.

### 2-4. Settings 페이지에서 SSR prop 제거

```diff
--- a/admin-dashboard/frontend/src/features/settings/pages/SettingsPage.tsx
+++ b/admin-dashboard/frontend/src/features/settings/pages/SettingsPage.tsx
@@
 import { DockerContainersSection } from "@/features/settings/components/DockerContainersSection";
 import { SettingsFormSection } from "@/features/settings/components/SettingsFormSection";
-import type { SettingsResponse } from "@/features/settings/types";
-import { useSSRData } from "@/hooks/useSSRData";
 
 export const SettingsPage = () => {
-    const ssrSettingsData = useSSRData("settings", (data) =>
-        data?.status === "ok" && data.settings
-            ? (data as SettingsResponse)
-            : undefined,
-    );
-
-    const ssrDockerHealthData = useSSRData("docker", (data) =>
-        data?.status === "ok"
-            ? { status: data.status, available: data.available }
-            : undefined,
-    );
-
-    const ssrContainersData = useSSRData("containers", (data) =>
-        data?.status === "ok" && data.containers
-            ? { status: data.status, containers: data.containers }
-            : undefined,
-    );
-
     return (
         <div className="max-w-4xl mx-auto space-y-6">
-            <SettingsFormSection initialData={ssrSettingsData} />
-            <DockerContainersSection
-                initialHealth={ssrDockerHealthData}
-                initialContainers={ssrContainersData}
-            />
+            <SettingsFormSection />
+            <DockerContainersSection />
         </div>
     );
 };
```

### 2-5. stale 문서 업데이트

`admin-dashboard/AGENTS.md`에서 아래 문구를 제거하십시오.

```diff
--- a/admin-dashboard/AGENTS.md
+++ b/admin-dashboard/AGENTS.md
@@
-- **SSR Data**: Inject initial data via `window.__SSR_DATA__`
```

---

# 3. Frontend transport SSOT: `holoClient.ts`는 제거 대상

## 문제

현재 frontend는 이미 `adminClient` 하나를 generated `Admin` 인스턴스의 SSOT로 갖고 있습니다. 그런데 `src/api/holoClient.ts`가 이 위에 **아무 의미 없는 얇은 래퍼 층**으로 한 겹 더 있습니다.

이 파일은 값 변환이나 오류 정규화도 하지 않고, 그저 `await adminClient.holoX().data`만 반복합니다. 이런 레이어는 transport SSOT를 흐리고, generated client 변경 시 diff surface만 키웁니다.

## 수정 파일

- 삭제: `admin-dashboard/frontend/src/api/holoClient.ts`
- 수정: 각 feature API (`alarms`, `members`, `rooms`, `stats`, `streams`, `milestones`, `settings`)
- 수정: `admin-dashboard/frontend/src/api/blueprint-section-8.test.ts`

## 코드 개선안

### 3-1. `holoClient.ts` 삭제

```text
DELETE admin-dashboard/frontend/src/api/holoClient.ts
```

### 3-2. feature API를 `adminClient` 직접 사용으로 치환

#### alarms

```diff
--- a/admin-dashboard/frontend/src/features/alarms/api.ts
+++ b/admin-dashboard/frontend/src/features/alarms/api.ts
@@
 import type { DeleteAlarmRequest } from "@/api/generated/data-contracts";
-import { holoClient } from "@/api/holoClient";
+import { adminClient } from "@/api/adminClient";
 
 export const alarmsApi = {
-	getAll: holoClient.getAlarms,
-	delete: (request: DeleteAlarmRequest) => holoClient.deleteAlarm(request),
+	getAll: async () => (await adminClient.holoGetAlarms()).data,
+	delete: async (request: DeleteAlarmRequest) =>
+		(await adminClient.holoDeleteAlarm(request)).data,
 };
 
 export const namesApi = {
 	setRoomName: (roomId: string, roomName: string) =>
-		holoClient.setRoomName({ roomId, roomName }),
+		adminClient.holoSetRoomName({ roomId, roomName }).then((r) => r.data),
 	setUserName: (userId: string, userName: string) =>
-		holoClient.setUserName({ userId, userName }),
+		adminClient.holoSetUserName({ userId, userName }).then((r) => r.data),
 };
```

#### members

```diff
--- a/admin-dashboard/frontend/src/features/members/api.ts
+++ b/admin-dashboard/frontend/src/features/members/api.ts
@@
-import { holoClient } from "@/api/holoClient";
+import { adminClient } from "@/api/adminClient";
@@
 export const membersApi = {
-	getAll: holoClient.getMembers,
+	getAll: async () => (await adminClient.holoGetMembers()).data,
 	add: async (member: Partial<Member>) => {
@@
-		return holoClient.addMember(request);
+		return (await adminClient.holoAddMember(request)).data;
 	},
 	addAlias: (memberId: number, request: AddAliasRequest) =>
-		holoClient.addAlias(memberId, request),
+		adminClient.holoAddAlias(memberId, request).then((r) => r.data),
 	removeAlias: (memberId: number, request: RemoveAliasRequest) =>
-		holoClient.removeAlias(memberId, request),
+		adminClient.holoRemoveAlias(memberId, request).then((r) => r.data),
 	setGraduation: (memberId: number, request: SetGraduationRequest) =>
-		holoClient.setGraduation(memberId, request),
+		adminClient.holoSetGraduation(memberId, request).then((r) => r.data),
 	updateChannel: (memberId: number, request: UpdateChannelRequest) =>
-		holoClient.updateChannel(memberId, request),
+		adminClient.holoUpdateChannel(memberId, request).then((r) => r.data),
 	updateName: (memberId: number, name: string) =>
-		holoClient.updateMemberName(memberId, { name }),
+		adminClient.holoUpdateMemberName(memberId, { name }).then((r) => r.data),
 };
```

나머지 `rooms/api.ts`, `stats/api.ts`, `streams/api.ts`, `milestones/api.ts`, `settings/api.ts`도 동일 원칙으로 치환합니다.

### 3-3. blueprint 테스트를 “wrapper 제거” 기준으로 변경

```diff
--- a/admin-dashboard/frontend/src/api/blueprint-section-8.test.ts
+++ b/admin-dashboard/frontend/src/api/blueprint-section-8.test.ts
@@
-test("api wrappers import the shared adminClient singleton", () => {
+test("legacy holoClient wrapper no longer exists", () => {
+	assert.equal(() => readSource("holoClient.ts"), undefined);
+});
+
+test("feature APIs import the shared adminClient singleton directly", () => {
 	const coreSource = readSource("core.ts");
-	const holoClientSource = readSource("holoClient.ts");
+	const membersSource = readSource("../features/members/api.ts");
 
 	assert.match(coreSource, /from ["']@\/api\/adminClient["']/);
-	assert.match(holoClientSource, /from ["']@\/api\/adminClient["']/);
+	assert.match(membersSource, /from ["']@\/api\/adminClient["']/);
 });
```

실제로는 `readSource`가 디렉터리 상대경로도 읽을 수 있게 약간 손봐야 합니다.

---

# 4. `shared-go` SSOT 미완료: `envutil` 복제와 `logging` compatibility wrapper 제거

## 문제

최신 업로드본에서도 다음 두 문제가 남아 있습니다.

1. `shared-go/pkg/envutil/env.go`와 `hololive/hololive-shared/internal/envutil/env.go`가 사실상 동일 구현입니다.
2. `hololive/hololive-shared/pkg/logging/logging.go`는 여전히 `shared-go/pkg/logging`을 감싼 compatibility wrapper입니다.

이는 “이제 shared-go로 통일했다”는 저장소 방향과 맞지 않습니다.

## 수정 파일

- 수정: `hololive/hololive-shared/pkg/config/*.go`, `internal/dbx/config.go`
- 삭제: `hololive/hololive-shared/internal/envutil/env.go`
- 삭제: `hololive/hololive-shared/internal/envutil/env_test.go` (존재 시)
- 수정: 모든 `github.com/kapu/hololive-shared/pkg/logging` import
- 삭제: `hololive/hololive-shared/pkg/logging/logging.go`
- 삭제: `hololive/hololive-shared/pkg/logging/logging_test.go`
- 수정: `scripts/architecture/check-go-compat-adapters.sh`

## 코드 개선안

### 4-1. envutil import 전면 치환

```diff
--- a/hololive/hololive-shared/pkg/config/config.go
+++ b/hololive/hololive-shared/pkg/config/config.go
@@
-import "github.com/kapu/hololive-shared/internal/envutil"
+import "github.com/park285/llm-kakao-bots/shared-go/pkg/envutil"
```

동일 치환 대상:

- `hololive/hololive-shared/pkg/config/llm_scheduler.go`
- `hololive/hololive-shared/pkg/config/config_parsers.go`
- `hololive/hololive-shared/pkg/config/admin_api.go`
- `hololive/hololive-shared/pkg/config/config_env_loaders.go`
- `hololive/hololive-shared/internal/dbx/config.go`

### 4-2. internal envutil 삭제

```text
DELETE hololive/hololive-shared/internal/envutil/env.go
```

### 4-3. logging wrapper 제거

#### 예시: bot main

```diff
--- a/hololive/hololive-kakao-bot-go/cmd/bot/main.go
+++ b/hololive/hololive-kakao-bot-go/cmd/bot/main.go
@@
-import sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
+import sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
```

동일 치환 대상은 ripgrep 결과 기준으로 다음 파일들입니다.

- `hololive/hololive-stream-ingester/cmd/stream-ingester/main.go`
- `hololive/hololive-stream-ingester/cmd/youtube-scraper/main.go`
- `hololive/hololive-dispatcher-go/cmd/dispatcher/main.go`
- `hololive/hololive-llm-sched/cmd/llm-scheduler/main.go`
- 관련 테스트 파일들

그 후 wrapper 삭제:

```text
DELETE hololive/hololive-shared/pkg/logging/logging.go
DELETE hololive/hololive-shared/pkg/logging/logging_test.go
```

### 4-4. adapter 재도입 방지 gate 강화

```diff
--- a/scripts/architecture/check-go-compat-adapters.sh
+++ b/scripts/architecture/check-go-compat-adapters.sh
@@
 forbidden_files=(
   "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server/shared_compat.go"
   "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal/server/api_trigger_compat.go"
+  "${ROOT_DIR}/hololive/hololive-shared/internal/envutil/env.go"
+  "${ROOT_DIR}/hololive/hololive-shared/pkg/logging/logging.go"
 )
```

---

# 5. Go thin wrapper / alias residue 정리

## 문제

큰 덩어리의 wrapper는 많이 정리됐지만, 최신 업로드본에도 다음 얇은 ceremony가 남아 있습니다.

- `hololive-kakao-bot-go/internal/server/settings_types.go`
- `hololive-kakao-bot-go/internal/server/settings_result.go`
- `hololive-kakao-bot-go/internal/server/api_response.go`
- `hololive-kakao-bot-go/internal/app/bootstrap_bot.go`의 `ProvideBot`, `ProvideYouTubeScheduler`
- `hololive-llm-sched/internal/app/api_router.go`의 `ProvideHealthOnlyRouter`, `ProvideTriggerRouter`
- `hololive-llm-sched/internal/app/delivery_providers_local.go`의 1-line provider 다수

이 계열은 기능 버그는 아니지만, 저장소 전반에 “의미보다 조립 문법이 많은 느낌”을 남깁니다.

## 수정 파일

- 삭제: `hololive/hololive-kakao-bot-go/internal/server/settings_types.go`
- 삭제: `hololive/hololive-kakao-bot-go/internal/server/settings_result.go`
- 삭제: `hololive/hololive-kakao-bot-go/internal/server/api_response.go`
- 수정: `hololive/hololive-kakao-bot-go/internal/server/*.go`
- 수정: `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot.go`
- 수정: 관련 테스트
- 수정: `hololive/hololive-llm-sched/internal/app/api_router.go`
- 수정: `hololive/hololive-llm-sched/internal/app/delivery_providers_local.go`
- 수정: `scripts/architecture/check-go-compat-adapters.sh`

## 코드 개선안

### 5-1. server settings alias 파일 제거

`type SettingsApplier = ...`, `type ScraperProxyApplyResult = ...` 계열은 모두 삭제하고 사용처에서 직접 shared type을 import하십시오.

```text
DELETE hololive/hololive-kakao-bot-go/internal/server/settings_types.go
DELETE hololive/hololive-kakao-bot-go/internal/server/settings_result.go
```

사용처 예시:

```diff
--- a/hololive/hololive-kakao-bot-go/internal/server/api_settings.go
+++ b/hololive/hololive-kakao-bot-go/internal/server/api_settings.go
@@
-import "github.com/kapu/hololive-kakao-bot-go/internal/server"
+import sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
@@
-    var result ScraperProxyApplyResult
+    var result sharedsettings.ScraperProxyApplyResult
```

### 5-2. `api_response.go` 삭제

`respondError`/`respondInternalError`는 의미 없는 forwarding입니다.

```diff
--- a/hololive/hololive-kakao-bot-go/internal/server/handlers_x.go
+++ b/hololive/hololive-kakao-bot-go/internal/server/handlers_x.go
@@
-    h.respondError(c, http.StatusBadRequest, "invalid request", nil)
+    sharedserver.RespondError(c, http.StatusBadRequest, "invalid request", nil)
@@
-    h.respondInternalError(c, "internal error", "failed to ...", err)
+    sharedserver.RespondInternalError(h.logger, c, "internal error", "failed to ...", err)
```

그 후 삭제:

```text
DELETE hololive/hololive-kakao-bot-go/internal/server/api_response.go
```

### 5-3. `ProvideBot`, `ProvideYouTubeScheduler` 인라인화

```diff
--- a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go
+++ b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_runtime_orchestration.go
@@
-    botBot, err := ProvideBot(runtimeViews.botDeps)
+    botBot, err := bot.NewBot(runtimeViews.botDeps)
     if err != nil {
-        return nil, fmt.Errorf("provide bot: %w", err)
+        return nil, fmt.Errorf("new bot: %w", err)
     }
@@
-    youtubeScheduler := ProvideYouTubeScheduler(runtimeViews.botDeps)
+    var youtubeScheduler youtube.Scheduler
+    if runtimeViews.botDeps != nil {
+        youtubeScheduler = runtimeViews.botDeps.YouTubeScheduler
+    }
```

그 후 `bootstrap_bot.go`의 해당 두 helper 삭제 및 테스트 수정.

### 5-4. llm router helper는 `Provide*`가 아니라 내부 builder로 축소

```diff
--- a/hololive/hololive-llm-sched/internal/app/api_router.go
+++ b/hololive/hololive-llm-sched/internal/app/api_router.go
@@
-func ProvideHealthOnlyRouter(ctx context.Context, logger *slog.Logger, apiKey string) (*gin.Engine, error) {
+func buildHealthOnlyRouter(ctx context.Context, logger *slog.Logger, apiKey string) (*gin.Engine, error) {
@@
-func ProvideTriggerRouter(
+func buildTriggerRouter(
```

이 함수들은 외부 모듈에 노출할 필요가 없으므로 exported symbol일 이유가 없습니다.

### 5-5. delivery provider thin wrapper는 module builder로 통합

```diff
--- a/hololive/hololive-llm-sched/internal/app/delivery_providers_local.go
+++ b/hololive/hololive-llm-sched/internal/app/delivery_module.go
@@
-type irisDeliverySender struct {
-    client iris.Sender
-}
+type DeliveryModule struct {
+    Locker     delivery.NotificationLocker
+    Repository *delivery.OutboxRepository
+    Sender     delivery.MessageSender
+    Dispatcher *delivery.Dispatcher
+}
@@
-func ProvideDeliveryLocker(cacheSvc cache.Client, logger *slog.Logger) delivery.NotificationLocker {
-    return delivery.NewLocker(cacheSvc, logger)
-}
-
-func ProvideOutboxRepository(postgres database.Client, logger *slog.Logger) *delivery.OutboxRepository {
-    return delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
-}
-
-func ProvideDeliverySender(client iris.Sender) delivery.MessageSender {
-    return irisDeliverySender{client: client}
-}
-
-func ProvideDeliveryDispatcher(repo *delivery.OutboxRepository, sender delivery.MessageSender, logger *slog.Logger) *delivery.Dispatcher {
-    return delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
+func BuildDeliveryModule(
+    cacheSvc cache.Client,
+    postgres database.Client,
+    irisClient iris.Sender,
+    logger *slog.Logger,
+) *DeliveryModule {
+    locker := delivery.NewLocker(cacheSvc, logger)
+    repo := delivery.NewOutboxRepository(postgres.GetGormDB(), logger)
+    sender := irisDeliverySender{client: irisClient}
+    dispatcher := delivery.NewDispatcher(repo, sender, logger, delivery.DefaultDispatcherConfig())
+
+    return &DeliveryModule{
+        Locker:     locker,
+        Repository: repo,
+        Sender:     sender,
+        Dispatcher: dispatcher,
+    }
 }
```

---

# 6. Alarm helper의 작은 의미 중복 정리

## 문제

이전 큰 알람 이슈는 대부분 해결됐지만, 작은 정책 중복이 남아 있습니다.

- `alarm_service.go`의 `buildRuntimeTargetMinutes`
- `providers/modules/settings.go`의 `NormalizeTargetMinutes([]int{current, 3, 1})`
- `pkg/util/time.go`와 `pkg/service/alarm/checker/helpers.go`의 `MinutesUntilFloor` 이름 중복(의미는 다름)

작은 문제지만, 이런 곳에서 다시 drift가 생깁니다.

## 수정 파일

- `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
- `hololive/hololive-shared/pkg/providers/modules/settings.go`
- `hololive/hololive-shared/pkg/util/time.go`
- 관련 테스트

## 코드 개선안

### 6-1. shared checker에 runtime target builder 추가

```diff
--- a/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
+++ b/hololive/hololive-shared/pkg/service/alarm/checker/helpers.go
@@
 func NormalizeTargetMinutes(targetMinutes []int) []int {
@@
 }
+
+// BuildRuntimeTargetMinutes는 운영 중 단일 분 설정을 공통 정책(3,1 fallback)과 함께 정규화한다.
+func BuildRuntimeTargetMinutes(alarmAdvanceMinutes int) []int {
+    return NormalizeTargetMinutes([]int{alarmAdvanceMinutes, 3, 1})
+}
```

### 6-2. local helper 제거

```diff
--- a/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go
+++ b/hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go
@@
-func buildRuntimeTargetMinutes(alarmAdvanceMinutes int) []int {
-    return buildTargetMinutes([]int{alarmAdvanceMinutes, 3, 1})
-}
-
@@
-    normalized := buildRuntimeTargetMinutes(alarmAdvanceMinutes)
+    normalized := sharedchecker.BuildRuntimeTargetMinutes(alarmAdvanceMinutes)
```

```diff
--- a/hololive/hololive-shared/pkg/providers/modules/settings.go
+++ b/hololive/hololive-shared/pkg/providers/modules/settings.go
@@
-    return sharedchecker.NormalizeTargetMinutes([]int{current, 3, 1})
+    return sharedchecker.BuildRuntimeTargetMinutes(current)
 }
```

### 6-3. `MinutesUntilFloor` 의미 충돌 제거

`pkg/util/time.go`의 pointer 기반 helper가 실제 프로덕션 사용이 거의 없으면 삭제가 가장 좋습니다. 삭제가 부담이면 이름을 바꾸십시오.

```diff
--- a/hololive/hololive-shared/pkg/util/time.go
+++ b/hololive/hololive-shared/pkg/util/time.go
@@
-func MinutesUntilFloor(target *time.Time, reference time.Time) int {
+func MinutesUntilFloorPtr(target *time.Time, reference time.Time) int {
```

그리고 테스트/사용처도 일괄 rename 합니다.

---

# 7. Architecture / CI gate는 아직 Go 중심이다

## 문제

현재 `ci-boundary-gate.sh`는 여러 중요한 gate를 잘 돌리고 있지만, 아직 두 가지 공백이 남아 있습니다.

1. `check-project-map.sh`가 실제 CI gate 체인에 연결되지 않았습니다.
2. LOC gate가 `check-go-module-loc.sh` 중심이라 Rust/TSX/Shell 대형 파일 증가를 막지 못합니다.
3. `.github/workflows/rust-quality.yml`에는 path filter 중복이 남아 있습니다.

## 수정 파일

- `scripts/architecture/ci-boundary-gate.sh`
- 신규: `scripts/architecture/check-file-loc.sh`
- 신규: `docs/architecture/file-loc-thresholds.txt`
- `.github/workflows/architecture-gates.yml` (필요 시)
- `.github/workflows/rust-quality.yml`

## 코드 개선안

### 7-1. project map gate 연결

```diff
--- a/scripts/architecture/ci-boundary-gate.sh
+++ b/scripts/architecture/ci-boundary-gate.sh
@@
 echo "[M6] Release governance assets gate"
 "${SCRIPT_DIR}/check-release-governance-assets.sh"
 echo
+
+echo "[M6] Project map freshness gate"
+"${SCRIPT_DIR}/check-project-map.sh"
+echo
 
 echo "[CI] Architecture boundary gate passed"
```

### 7-2. generic file LOC gate 추가

신규 파일 `scripts/architecture/check-file-loc.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
THRESHOLDS_FILE="${ROOT_DIR}/docs/architecture/file-loc-thresholds.txt"

if [[ ! -f "${THRESHOLDS_FILE}" ]]; then
  echo "FAIL: missing ${THRESHOLDS_FILE}" >&2
  exit 1
fi

status=0
while IFS='|' read -r rel_path max_loc reason; do
  [[ -z "${rel_path}" || "${rel_path}" =~ ^# ]] && continue
  file="${ROOT_DIR}/${rel_path}"
  if [[ ! -f "${file}" ]]; then
    echo "FAIL: threshold target missing: ${rel_path}" >&2
    status=1
    continue
  fi
  loc=$(wc -l < "${file}")
  if (( loc > max_loc )); then
    echo "FAIL: ${rel_path} is ${loc} LOC (limit ${max_loc}) :: ${reason}" >&2
    status=1
  fi
done < "${THRESHOLDS_FILE}"

exit ${status}
```

신규 `docs/architecture/file-loc-thresholds.txt` 예시:

```text
# path|max_loc|reason
admin-dashboard/backend/src/holo/handlers.rs|400|split typed holo handlers by domain
admin-dashboard/backend/src/holo/types.rs|220|split DTOs by domain
admin-dashboard/backend/src/config.rs|260|split config parsing and security/session sections
admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx|320|split chart rendering and hook logic
```

그리고 gate 연결:

```diff
--- a/scripts/architecture/ci-boundary-gate.sh
+++ b/scripts/architecture/ci-boundary-gate.sh
@@
 echo "[CI] Run M4 Go module LOC gate"
 "${SCRIPT_DIR}/check-go-module-loc.sh"
 echo
+
+echo "[CI] Run M4 generic file LOC gate"
+"${SCRIPT_DIR}/check-file-loc.sh"
+echo
```

### 7-3. Rust workflow path 중복 제거

```diff
--- a/.github/workflows/rust-quality.yml
+++ b/.github/workflows/rust-quality.yml
@@
 on:
   pull_request:
     paths:
       - 'admin-dashboard/backend/**'
-      - 'admin-dashboard/backend/**'
   push:
     branches:
       - main
     paths:
       - 'admin-dashboard/backend/**'
-      - 'admin-dashboard/backend/**'
   workflow_dispatch:
```

---

# 8. Admin backend startup/config는 panic 기반이 아니라 typed startup error로 바꿔야 함

## 문제

현재 `admin-dashboard/backend/src/config.rs`와 `main.rs`는 다음 패턴이 남아 있습니다.

- `Config::load()` 내부 `panic!`
- `required_alias()` 내부 `panic!`
- `main()` 내부 여러 `.expect(...)`

개발 중에는 편해 보이지만, 실제 운영에서는 **잘못된 설정**과 **프로세스 내부 버그**를 같은 방식으로 죽여 버립니다.

## 수정 파일

- `admin-dashboard/backend/src/config.rs`
- `admin-dashboard/backend/src/main.rs`
- 관련 테스트

## 코드 개선안

### 8-1. `Config::load() -> anyhow::Result<Config>`

```diff
--- a/admin-dashboard/backend/src/config.rs
+++ b/admin-dashboard/backend/src/config.rs
@@
 use std::{env, time::Duration};
+use anyhow::{Context, Result, anyhow};
@@
 impl Config {
-    pub fn load() -> Self {
+    pub fn load() -> Result<Self> {
@@
-        Self {
-            port: {
-                let p = env_int("PORT", 30190);
-                u16::try_from(p).unwrap_or_else(|_| panic!("PORT={p} is out of u16 range"))
-            },
+        Ok(Self {
+            port: {
+                let p = env_int("PORT", 30190);
+                u16::try_from(p).map_err(|_| anyhow!("PORT={p} is out of u16 range"))?
+            },
@@
-            admin_pass_hash: required_alias(&["ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT"]),
-            session_secret: required_alias(&["SESSION_SECRET", "ADMIN_SECRET_KEY"]),
+            admin_pass_hash: required_alias(&["ADMIN_PASS_HASH", "ADMIN_PASS_BCRYPT"])? ,
+            session_secret: required_alias(&["SESSION_SECRET", "ADMIN_SECRET_KEY"])? ,
@@
-        }
+        })
     }
 }
@@
-fn required_alias(keys: &[&str]) -> String {
+fn required_alias(keys: &[&str]) -> Result<String> {
     keys.iter()
         .find_map(|key| {
@@
-        .unwrap_or_else(|| {
-            panic!(
-                "required environment variable missing: {}",
-                keys.join(" or ")
-            )
-        })
+        .ok_or_else(|| anyhow!("required environment variable missing: {}", keys.join(" or ")))
 }
```

### 8-2. `main`을 `run() -> anyhow::Result<()>`로 분리

```diff
--- a/admin-dashboard/backend/src/main.rs
+++ b/admin-dashboard/backend/src/main.rs
@@
 #[tokio::main]
 async fn main() {
-    dotenvy::dotenv().ok();
-
-    let cfg = config::Config::load();
-    let _tracing_guards = logging::init_tracing(&cfg);
-    tracing::info!(port = %cfg.port, env = %cfg.env, "starting admin-dashboard");
+    if let Err(err) = run().await {
+        eprintln!("fatal: {err:#}");
+        std::process::exit(1);
+    }
+}
+
+async fn run() -> anyhow::Result<()> {
+    dotenvy::dotenv().ok();
+
+    let cfg = config::Config::load()?;
+    let _tracing_guards = logging::init_tracing(&cfg);
+    tracing::info!(port = %cfg.port, env = %cfg.env, "starting admin-dashboard");
@@
-    let pool = deadpool_redis::Config::from_url(format!("redis://{}", cfg.valkey_url))
-        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
-        .expect("valkey pool creation failed");
+    let pool = deadpool_redis::Config::from_url(format!("redis://{}", cfg.valkey_url))
+        .create_pool(Some(deadpool_redis::Runtime::Tokio1))
+        .context("valkey pool creation failed")?;
@@
-    let holo_api = Arc::new(
-        holo::client::HoloApiClient::new(
-            &cfg.holo_bot_url,
-            if cfg.holo_bot_api_key.is_empty() {
-                None
-            } else {
-                Some(cfg.holo_bot_api_key.clone())
-            },
-        )
-        .expect("holo api client init failed"),
-    );
+    let holo_api = Arc::new(holo::client::HoloApiClient::new(
+        &cfg.holo_bot_url,
+        if cfg.holo_bot_api_key.is_empty() {
+            None
+        } else {
+            Some(cfg.holo_bot_api_key.clone())
+        },
+    ).context("holo api client init failed")?);
@@
-    let listener = tokio::net::TcpListener::bind(addr)
-        .await
-        .expect("failed to bind");
+    let listener = tokio::net::TcpListener::bind(addr)
+        .await
+        .context("failed to bind")?;
@@
-    axum::serve(
+    axum::serve(
         listener,
         router.into_make_service_with_connect_info::<SocketAddr>(),
     )
     .with_graceful_shutdown(shutdown_signal())
     .await
-    .expect("server error");
+    .context("server error")?;
@@
-    tracing::info!("shutdown complete");
+    tracing::info!("shutdown complete");
+    Ok(())
 }
@@
 async fn shutdown_signal() {
     let ctrl_c = tokio::signal::ctrl_c();
     let mut sigterm = tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
-        .expect("sigterm handler");
+        .unwrap_or_else(|err| panic!("sigterm handler: {err}"));
```

`shutdown_signal`의 SIGTERM 부분도 더 나아가면 fallible helper로 바꾸는 것이 맞습니다. 최소한 `run()`으로 올려서 `Result`로 합치는 것이 더 좋습니다.

---

# 9. cache mock의 panic-only 정책은 strict/lenient 이중 모드로 바꾸는 것이 좋음

## 문제

현재 `hololive-shared/pkg/service/cache/mocks/client.go`는 함수 필드가 설정되지 않은 메서드를 호출하면 전부 panic합니다. strict mock 자체는 나쁜 것이 아니지만, 저장소 전반에 이 mock이 널리 쓰이는 상황에서는 **테스트 작성 마찰이 높습니다.**

## 수정 파일

- `hololive/hololive-shared/pkg/service/cache/mocks/client.go`
- 신규 테스트 파일

## 코드 개선안

### 9-1. strict/lenient 모드 도입

```diff
--- a/hololive/hololive-shared/pkg/service/cache/mocks/client.go
+++ b/hololive/hololive-shared/pkg/service/cache/mocks/client.go
@@
 type Client struct {
+    Strict bool
+
     GetFunc      func(ctx context.Context, key string, dest any) error
@@
 }
 
 var _ cache.Client = (*Client)(nil)
+
+func NewStrictClient() *Client {
+    return &Client{Strict: true}
+}
+
+func NewLenientClient() *Client {
+    return &Client{Strict: false}
+}
+
+func (m *Client) panicIfUnset(name string) {
+    if m != nil && m.Strict {
+        panic("cache mock: " + name + " not set")
+    }
+}
@@
 func (m *Client) SMembers(ctx context.Context, key string) ([]string, error) {
     if m.SMembersFunc != nil {
         return m.SMembersFunc(ctx, key)
     }
-    panic("cache mock: SMembersFunc not set")
+    m.panicIfUnset("SMembersFunc")
+    return nil, nil
 }
@@
 func (m *Client) Exists(ctx context.Context, key string) (bool, error) {
     if m.ExistsFunc != nil {
         return m.ExistsFunc(ctx, key)
     }
-    panic("cache mock: ExistsFunc not set")
+    m.panicIfUnset("ExistsFunc")
+    return false, nil
 }
```

원칙은 이렇습니다.

- 기본은 `NewStrictClient()`를 쓰게 유도
- 읽기/조회 계열은 lenient에서 safe zero-value를 반환
- 쓰기/락/비교계열은 lenient에서도 panic 유지 여부를 선택

### 9-2. 테스트 추가

```go
func TestLenientClient_ReadMethodsReturnZeroValues(t *testing.T) {
    client := NewLenientClient()

    members, err := client.SMembers(context.Background(), "rooms")
    require.NoError(t, err)
    require.Nil(t, members)
}

func TestStrictClient_PanicsWhenUnset(t *testing.T) {
    client := NewStrictClient()
    require.Panics(t, func() {
        _, _ = client.SMembers(context.Background(), "rooms")
    })
}
```

---

# 10. 과대 파일 분해: handlers/types/config/chart

## 문제

큰 기능 버그는 아니지만, 최신 업로드본 기준으로도 몇몇 핵심 파일이 계속 비대합니다.

- `admin-dashboard/backend/src/holo/handlers.rs` : 677 LOC
- `admin-dashboard/backend/src/holo/types.rs` : 282 LOC
- `admin-dashboard/backend/src/config.rs` : 484 LOC
- `admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx` : 588 LOC

이 상태를 방치하면 다음 리팩토링이 다시 막힙니다.

## 수정 파일

- `admin-dashboard/backend/src/holo/handlers.rs` → 분할
- `admin-dashboard/backend/src/holo/types.rs` → 분할
- `admin-dashboard/backend/src/config.rs` → 분할
- `admin-dashboard/frontend/src/components/dashboard/SystemStatsChart.tsx` → 분할
- 관련 import, OpenAPI 등록, test 경로 수정

## 코드 개선안

### 10-1. holo handlers 분할

새 구조:

```text
admin-dashboard/backend/src/holo/
  handlers/
    mod.rs
    common.rs
    alarms.rs
    members.rs
    rooms.rs
    settings.rs
    stats.rs
    streams.rs
    milestones.rs
```

`common.rs`에 공통 helper를 모읍니다.

```rust
use crate::error::AppError;
use crate::holo::client::HoloApiClient;

pub async fn get_typed<T: serde::de::DeserializeOwned>(
    client: &HoloApiClient,
    path: &str,
    query: Option<&[(&str, String)]>,
) -> Result<axum::Json<T>, AppError> {
    let (_, body) = client.get(path, query).await?;
    Ok(axum::Json(body))
}
```

`mod.rs`는 재노출만 담당합니다.

```rust
pub mod alarms;
pub mod common;
pub mod members;
pub mod milestones;
pub mod rooms;
pub mod settings;
pub mod stats;
pub mod streams;

pub use alarms::*;
pub use members::*;
pub use milestones::*;
pub use rooms::*;
pub use settings::*;
pub use stats::*;
pub use streams::*;
```

### 10-2. holo types 분할

```text
admin-dashboard/backend/src/holo/
  types/
    mod.rs
    common.rs
    alarms.rs
    members.rs
    rooms.rs
    settings.rs
    stats.rs
    streams.rs
    milestones.rs
```

예를 들어 `StatusOnlyResponse`와 공통 response는 `common.rs`, member 관련 DTO는 `members.rs`로 분리합니다.

### 10-3. config 분할

```text
admin-dashboard/backend/src/config/
  mod.rs
  env.rs
  security.rs
  session.rs
```

- `env.rs`: `env_string`, `env_bool`, `env_int`, `required_alias`
- `security.rs`: `SecurityMode`, `SecurityConfig`, origin parsing
- `session.rs`: `SessionConfig`
- `mod.rs`: `Config`와 `load()` orchestration

### 10-4. `SystemStatsChart.tsx` 분할

새 구조:

```text
admin-dashboard/frontend/src/features/stats/components/system-stats/
  SystemStatsChart.tsx
  ResourceChart.tsx
  GoroutineChart.tsx
  ChartSkeleton.tsx
admin-dashboard/frontend/src/features/stats/hooks/
  useSystemStatsSeries.ts
admin-dashboard/frontend/src/features/stats/lib/
  systemStatsSeries.ts
```

대표 분해 기준:

- WebSocket/stream subscription: `useSystemStatsSeries.ts`
- 데이터 누적/샘플링/정규화: `systemStatsSeries.ts`
- 개별 chart 렌더링: `ResourceChart.tsx`, `GoroutineChart.tsx`
- 상위 컨테이너는 탭/레이아웃만 유지

이 분해를 마친 뒤, 7번의 generic LOC gate threshold를 실제 enforce 값으로 잡으십시오.

---

# 11. OpenAPI는 success-only가 아니라 error schema까지 typed contract로 닫아야 함

## 문제

backend는 이제 typed holo contract를 갖지만, OpenAPI는 여전히 주로 성공 응답 위주입니다. 1번에서 에러 passthrough를 도입하면, OpenAPI도 그 의미를 드러내야 합니다.

## 수정 파일

- `admin-dashboard/backend/src/openapi.rs`
- `admin-dashboard/backend/src/holo/handlers/*`
- generated frontend client 재생성 결과물

## 코드 개선안

### 11-1. `ErrorResponse`를 components에 포함

```diff
--- a/admin-dashboard/backend/src/openapi.rs
+++ b/admin-dashboard/backend/src/openapi.rs
@@
     components(schemas(
+        crate::error::ErrorResponse,
         crate::handlers::auth::LoginRequest,
```

### 11-2. 각 handler path에 error response 명시

예시 (`members.rs`):

```rust
#[utoipa::path(
    get,
    path = "/admin/api/holo/members",
    responses(
        (status = 200, description = "Members list", body = MembersResponse),
        (status = 401, description = "Unauthorized", body = ErrorResponse),
        (status = 502, description = "Upstream unavailable", body = ErrorResponse),
    ),
    tag = "holo"
)]
```

mutation 쪽에는 400도 넣습니다.

### 11-3. client 재생성 후 drift CI 유지

이미 frontend workflow에 regenerate + diff gate가 있으므로, backend swagger가 바뀐 뒤 다음 순서로 regenerate 하십시오.

```bash
cd admin-dashboard/frontend
npm run generate:api
```

그리고 generated output diff를 같이 커밋합니다.

---

# 12. 적용 순서

실무적으로는 아래 순서가 가장 안전합니다.

1. **Admin contract 의미 보정**
   - 1번 upstream 4xx passthrough
   - 11번 OpenAPI error schema 반영
2. **죽은 코드/잔존 transport 정리**
   - 2번 SSR 제거
   - 3번 `holoClient.ts` 제거
3. **shared SSOT 정리**
   - 4번 `envutil`/`logging` 단일화
   - 6번 alarm helper 소규모 정리
4. **thin wrapper 제거**
   - 5번 alias/provider/response wrapper 정리
5. **거버넌스/운영 품질 보강**
   - 7번 architecture gate 확장
   - 8번 typed startup error
   - 9번 cache mock strict/lenient
6. **마지막 구조 분해**
   - 10번 과대 파일 분해

이 순서로 가야 계약 의미와 운영 가시성을 먼저 바로잡고, 그 다음에 구조 정리와 가드 강화를 이어갈 수 있습니다.

---

# 13. 최종 평가

이번 업로드본은 이전 단계 대비 확실히 좋아졌습니다. 다만 아직 다음 상태입니다.

- 운영 핵심 경로: **대체로 안정화됨**
- 구조 단순화: **거의 끝났지만 마지막 잔존 어댑터가 남음**
- shared SSOT: **대부분 정리됐지만 `envutil`/`logging`이 아직 남음**
- admin contract: **typed contract 도입 성공, 하지만 error semantics와 dead SSR 잔재가 남음**
- 거버넌스: **기본 gate는 갖췄지만 project-map / generic LOC까지는 미완료**

즉 지금은 “방향은 맞고 큰 구조는 정리됐지만, 마지막 10~15%의 잔존 불일치가 아직 남아 있는 상태”입니다. 이번 문서의 항목을 다 닫으면, 그때는 저장소 전체 기준으로도 상당히 깔끔한 마감 상태에 들어갑니다.
