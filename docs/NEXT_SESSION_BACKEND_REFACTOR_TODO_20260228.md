# Next Session TODO — Backend Modernization / Legacy-Fallback Removal

작성일: 2026-02-28 (UTC) / 2026-03-01 (KST)  
목표: "과도한 optional/fallback/legacy 경로 제거 + fail-fast 일관화 + 최신화"

---

## 0) 기준 원칙 (항상 적용)
- [ ] **Fail-fast 우선**: 입력/의존성/초기화 실패를 침묵 처리하지 않기
- [ ] **Fallback 최소화**: 운영 안정성 필수 fallback 외 제거
- [ ] **Admin/Kakao 대칭성 유지**: 동일 API 동작/에러 계약 보장
- [ ] **레거시 경로 제거 시 문서 동기화 필수**

---

## 1) 이미 완료된 작업 (재작업 방지 체크)
- [x] `NoAuth` 우회 제거 (router/bootstrap/config)
- [x] legacy `channelId` 단일 조회 제거 (`channelIds` 강제)
- [x] near-milestone closest fallback 제거
- [x] milestone query 파라미터 invalid 값 fail-fast(400)
- [x] stats 집계 에러 무시 제거 (500)
- [x] websocket stats 초기/주기 수집 silent fallback 제거
- [x] 관련 테스트 통과 확인 (admin/kakao/shared)

관련 커밋:  
`a20491a`, `c33dbcd`, `85c885c`, `bd3f9ce`, `16c1e53`

---

## 1.1) 점검 완료 범위 스냅샷 (파일 단위)

### A. 이미 점검/수정 완료
- [x] `hololive/hololive-kakao-bot-go/internal/app/api_router.go`
- [x] `hololive/hololive-kakao-bot-go/internal/app/api_router_test.go`
- [x] `hololive/hololive-kakao-bot-go/internal/app/bootstrap*.go`
- [x] `hololive/hololive-kakao-bot-go/internal/server/api_stream.go`
- [x] `hololive/hololive-kakao-bot-go/internal/server/api_milestone.go`
- [x] `hololive/hololive-kakao-bot-go/internal/server/api_stats.go`
- [x] `hololive/hololive-kakao-bot-go/internal/command/{dispatcher,helpers,member_info}.go`
- [x] `hololive/hololive-kakao-bot-go/cmd/bot/main.go`
- [x] `hololive/hololive-admin/internal/app/api_router.go`
- [x] `hololive/hololive-admin/internal/app/api_router_test.go`
- [x] `hololive/hololive-admin/internal/app/bootstrap_admin.go`
- [x] `hololive/hololive-admin/internal/server/api_stream.go`
- [x] `hololive/hololive-admin/internal/server/api_milestone.go`
- [x] `hololive/hololive-admin/internal/server/api_stats.go`
- [x] `hololive/hololive-shared/pkg/config/{config.go,admin_api.go}`

### B. 점검은 했지만 추가 정리가 남은 범위
- [x] `hololive/hololive-admin/internal/server/api_template.go` (문구상 fallback 표현/응답 규약 정비)
- [x] `hololive/hololive-kakao-bot-go/internal/server/api_template.go` (동일)
- [x] `hololive/hololive-admin/internal/server` 나머지 핸들러 전수
- [x] `hololive/hololive-kakao-bot-go/internal/server` 나머지 핸들러 전수
- [x] `hololive/hololive-shared/pkg/service/**` fallback 인벤토리/분류

---

## 2) 다음 세션 핵심 작업 (우선순위 높음)

### A. Server 핸들러 전수 점검 (admin + kakao)
- [x] `internal/server` 전 파일에서 **err 무시 패턴** 제거 (`_, _ =`, `if err == nil { ... } else ignore`, `continue`로 무시)
- [x] 사용자 입력 파라미터 파싱 시 **무시형 default fallback** 제거
- [x] 실패 응답 코드/메시지 표준화 (400/401/403/404/409/500/503)
- [x] Admin/Kakao 동일 엔드포인트 동작 diff 점검

### B. Shared 서비스 레이어 fallback 분류/정리
- [x] `hololive-shared/pkg/service/**` fallback 전수 인벤토리 작성
- [x] fallback을 3등급으로 분류
  - [x] 필수(가용성 핵심)
  - [x] 조건부(기능 품질 개선)
  - [x] 제거 대상(legacy/중복/침묵)
- [x] 제거 대상 fallback 실제 삭제 + 로그/에러 계약 정비
  - [x] 제거 대상 4건 fallback hit/deprecated usage 로깅 추가
  - [x] `majorevent/summarizer_prompt.go`: `ongoing_note` fallback 제거
  - [x] `database/postgres.go`: deprecated `GetDB()` 제거
  - [x] `youtube/scraper/videos.go`: legacy URL fallback 제거
  - [x] `youtube/scraper/channel.go`: `c4TabbedHeaderRenderer` fallback 제거

### C. Config/Env 레거시 지원 정리
- [x] deprecated env dual-read/alias 경로 점검
- [x] 비권장 env 사용 시 경고만 하고 계속 진행하는 경로 → fail-fast 전환 가능성 검토
- [x] 필수 env 누락 시 초기화 단계에서 즉시 실패하도록 통일
  - [x] `Load/LoadAlarmDispatcher/LoadLLMScheduler`에서 IRIS 토큰 누락 fail-fast 추가

---

## 3) API 계약/문서 동기화
- [x] `docs/http_spec.md` 등에서 제거된 legacy query/동작 반영
- [x] 에러 응답 예시를 실제 구현과 일치시킴
- [x] 변경점 migration note 추가 (클라이언트 영향 포함)

---

## 4) 라이브러리/의존성 최신화 점검
- [x] Go 워크스페이스 전체 `go list -m -u all` 점검
- [x] 보안 취약점 점검(`govulncheck` 또는 동등 도구)
- [x] Rust 크레이트(해당 모듈) 업데이트 후보 확인
- [x] 업데이트 후 회귀 테스트 + 성능 영향 확인

---

## 5) 성능 이슈 관점 점검
- [x] 핸들러별 불필요 DB/API 호출 경로 재확인
  - [x] admin `api_stream.go`에 kakao와 동일한 member index 캐시/리미터 경로 적용 (중복 `GetAllMembers` 완화)
  - [x] milestone near-count DB COUNT 경량화(`CountNearMilestoneMembers`) 적용
- [x] polling/ws 관련 과도 호출 방지(샘플링/캐시 TTL/리미터)
  - [x] admin/kakao `api_stats.go` websocket 주기 2s → 5s 완화
  - [x] admin/kakao `api_stream.go` refresh lock TTL 2m → 5m 상향
  - [x] admin `api_stream.go` 비동기 limiter/cached member index parity 적용
- [x] p95 지연/에러율 관점 최소 벤치 지표 확보 (이번 세션 범위 제외: 사용자 요청)

---

## 6) 테스트/품질 게이트
- [x] 변경 모듈 단위 테스트 먼저
- [x] 영향 모듈 통합 테스트 확장
- [x] 실패 시 즉시 원인 고정 후 재실행 (skip 금지)
- [x] 최종 커밋 전 `go test` 스위트 재확인

권장 실행 순서(다음 세션 시작용):
1. `hololive/hololive-admin` 서버 전수 정리 + 테스트
2. `hololive/hololive-kakao-bot-go` 동일 패턴 적용 + 테스트
3. `hololive/hololive-shared` 서비스/설정 fallback 정리 + 테스트
4. 문서/API 스펙 동기화

---

## 7) 2026-03-01 진행 로그 (KST)
- admin/kakao `internal/server` 추가 fail-fast 정비 완료
  - cache read 실패 시 즉시 500 응답 (stats)
  - DB snapshot 조회 실패 masking 제거 (503→500 분리)
  - `org` 빈 값 default fallback 제거
  - `channelIds` 100개 초과 시 truncation 제거, 400 fail-fast
  - websocket upgrade/close/write 오류 무시 제거
  - room remove 시 not-found를 404로 명시
  - template override delete 응답 문구에서 fallback 표현 제거
  - profile translation 실패 시 500 처리
  - member cache invalidate/refresh 실패 무시 제거
- 테스트
  - `hololive-admin`: `go test ./internal/app ./internal/server` ✅
  - `hololive-kakao-bot-go`: `go test ./internal/app ./internal/server` ✅
- shared 서비스 fallback 인벤토리 작성: `docs/SHARED_SERVICE_FALLBACK_INVENTORY_20260301.md`
- 의존성/보안 점검
  - go.work 대상 7개 모듈 `go list -m -u all` 점검 완료 (직접 의존성 업데이트 후보 없음)
  - `govulncheck ./...` (`hololive-admin`, `hololive-kakao-bot-go`) 취약점 없음
- 응답 코드/메시지 표준화 추가 정리
  - admin `api_response.go` 추가 (respondError/respondInternalError parity)
  - admin/kakao `api_stream.go` 오류 응답 경로 helper 기반으로 통일
  - admin/kakao `api_majorevent.go` 내부 에러 노출(`err.Error()`) 제거
  - admin/kakao `api_room.go` 중복 생성 시 `409` 반환으로 통일
  - admin/kakao `api_member.go` 생성 성공 시 `201` 반환으로 통일
- 테스트 재검증
  - `hololive-admin`: `go test ./internal/server && go test ./...` ✅
  - `hololive-kakao-bot-go`: `go test ./internal/server && go test ./...` ✅
- Rust 점검
  - `hololive-scraper-rs`: `cargo update --dry-run` 업데이트 후보 확인(18 packages)
  - `cargo audit` 취약점 없음, `cargo test --all` ✅
- shared 제거 대상 fallback 계측
  - `majorevent/summarizer_prompt.go`: `ongoing_note` fallback hit 로깅 추가
  - `youtube/scraper/videos.go`: legacy URL fallback 시도/복구 카운트 로깅 추가
  - `youtube/scraper/channel.go`: `c4TabbedHeaderRenderer` fallback hit 로깅 추가
  - `database/postgres.go`: deprecated `GetDB()` 호출 카운트 로깅 추가
- shared 제거 대상 fallback 실제 제거(부분)
  - `majorevent/summarizer_prompt.go`: `ongoing_note` 스키마/조립 fallback 제거
  - `database/postgres.go`: `GetDB()` 제거로 pool 접근 단일화
- shared 제거 대상 fallback 실제 제거(완료)
  - `youtube/scraper/videos.go`: legacy `/videos?view=0&sort=dd&shelf_id=0` 재시도 제거
  - `youtube/scraper/channel.go`: `c4TabbedHeaderRenderer` fallback 제거
- shared 테스트
  - `hololive-shared`: `go test ./...` ✅
- config/env fail-fast 강화
  - `hololive-shared/pkg/config/config.go`: `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`(또는 `IRIS_SHARED_TOKEN`) 누락 시 Validate 실패
  - `hololive-shared/pkg/config/alarm_dispatcher.go`: `IRIS_BOT_TOKEN`(또는 `IRIS_SHARED_TOKEN`) 누락 시 validate 실패
  - `hololive-shared/pkg/config/llm_scheduler.go`: webhook/bot token 누락 시 validate 실패
  - 관련 테스트 보강: `config_test.go`, `providers_test.go`
- config/env legacy alias fail-fast 전환
  - `MEMBER_NEWS_CLIPROXY_MODEL`, `DB_SSLMODE`, `DB_QUERY_EXEC_MODE` 사용 시 즉시 Validate 실패
  - `loadLLMConfig`의 `MEMBER_NEWS_CLIPROXY_MODEL` dual-read 제거
  - `loadPostgresConfig`의 `DB_SSLMODE`/`DB_QUERY_EXEC_MODE` fallback 제거
- shared fallback 제거 완료
  - `majorevent/summarizer_prompt.go`: `ongoing_note` 스키마/텍스트 fallback 제거
  - `database/postgres.go`: deprecated `GetDB()` 제거
  - `youtube/scraper/videos.go`: legacy `/videos?view=0&sort=dd&shelf_id=0` 재시도 제거
  - `youtube/scraper/channel.go`: `c4TabbedHeaderRenderer` fallback 제거
- 최종 회귀
  - `hololive-shared`: `go test ./...` ✅
  - `hololive-admin`: `go test ./...` ✅
  - `hololive-kakao-bot-go`: `go test ./...` ✅
- 성능 완화/대칭성 보강
  - admin `api_stream.go`를 kakao parity로 정렬:
    - member index 캐시(`getActiveMemberIndex`) + invalidate 경로 추가
    - cache/refresh goroutine limiter 적용
    - refresh lock value 상수화 + TTL 5분 상향
  - admin `api_member.go` 멤버 변경 후 member index invalidate 연동
  - kakao/admin 공통 `api_stats.go` websocket 스트리밍 주기 5초로 완화
  - kakao/admin 공통 `api_milestone.go` near-milestone 집계 시 row fetch 대신 DB COUNT 사용
- 재검증
  - `hololive-admin`: `go test ./...` ✅
  - `hololive-kakao-bot-go`: `go test ./...` ✅
  - `hololive-shared`: `go test ./...` ✅

---

## 8) 완료 정의 (DoD)
- [x] 제거 대상 fallback 0건
- [x] 침묵형 예외 처리(무시/continue) 0건(허용 목록 제외)
- [x] Admin/Kakao 동일 API 계약 정합성 확보
- [x] 테스트/빌드 통과 + 문서 동기화 완료
