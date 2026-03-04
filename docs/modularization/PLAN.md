# Hololive Bot 전체 모듈 분할화 실행 계획

> 작성일: 2026-03-02 | 갱신: 2026-03-03 (Kickoff 반영, Phase 0~6 확정)
> 범위: `/home/kapu/gemini/hololive-bot` 전체 (Go 6 모듈 + Rust 14 crates, Phase 0 완료 후)
> 전략: Incremental Strangler (빅뱅 금지, Phase별 독립 PR)

---

## 배경

- **Go `hololive-shared`** (55K LOC, 15개 패키지)가 god module로 기능하여 모든 서비스가 전체를 의존
- **Rust** shared/services/ 하위 8개 crate 중 5개 미사용, 3개는 dispatcher 단독 소비
- 의존성 최소화 및 유지보수성 향상을 위해 Go/Rust 양쪽 물리적 모듈 분할 진행

---

## 현재 의존성 매트릭스 (Go, 검증 완료)

| shared 패키지 | admin | bot | llm-sched | stream-ingester |
|---|---|---|---|---|
| config, domain, constants | O | O | O | O |
| cache, database, settings, server | O | O | O | O |
| youtube (12K LOC) | O | O | - | O |
| holodex (3.6K LOC) | O | O | - | O |
| template, member | O | O | O | O |
| alarm (2K LOC) | O | O | - | - |
| majorevent (5.5K LOC) | O(trigger) | - | O | - |
| membernews (5K LOC) | - | - | O | - |
| delivery (1.2K LOC) | - | - | O | - |
| configsub | - | O | O | - |
| youtube/outbox | - | O | - | O |
| notification (1.7K LOC) | - | O | - | - |
| chzzk (1.2K LOC) | - | O | - | - |
| twitch (320 LOC) | - | O | - | - |
| matcher (789 LOC) | - | O | - | - |
| errors (139 LOC) | - | O | - | - |
| iris (1.3K LOC) | - | O | - | O |
| adapter (3.9K LOC) | - | O | - | - |
| auth | O | O | - | - |
| health | O | O | O | O |
| repository | O | O | - | - |
| llm (590 LOC) | - | - | O | - |
| providers (1.8K LOC) | O | O | O | O |

---

## Phase 0: Rust 미사용 crate 삭제

- **Risk:** LOW
- **소요:** 1-2일
- **의존:** 없음 (독립 실행)
- **상태:** [x] 완료

### 삭제 대상 (5개, 소비자 0개 확인 완료)

| crate | 경로 |
|-------|------|
| shared-cache | `crates/shared/services/cache` |
| shared-configsub | `crates/shared/services/configsub` |
| shared-member | `crates/shared/services/member` |
| shared-ratelimit | `crates/shared/services/ratelimit` |
| shared-parser | `crates/shared/services/parser` |

### 작업

1. `Cargo.toml` workspace.members에서 5개 제거
2. `[workspace.dependencies]`에서 5개 path dependency 제거
3. 디렉토리 5개 삭제

### 검증

```bash
cd hololive/hololive-rs
cargo build --workspace && cargo test --workspace && cargo clippy --workspace --all-targets --all-features -- -D warnings
```

### 결과

19 crates -> 14 crates

---

## Phase 1: Rust dispatcher-only crate 재배치

- **Risk:** LOW-MED
- **소요:** 3-5일
- **의존:** Phase 0 완료 후
- **상태:** [x] 완료

### 이동 대상 (dispatcher-app만 소비하는 3개 shared crate)

| 현재 경로 | 이동 후 |
|----------|---------|
| `shared/services/notification` | `dispatcher/notification` |
| `shared/services/formatter` | `dispatcher/formatter` |
| `shared/services/template` | `dispatcher/template` |

### 작업

1. `crates/dispatcher/{notification,formatter,template}/` 디렉토리 생성, 코드 이동
2. 각 Cargo.toml path dependency 수정
3. `dispatcher-app/Cargo.toml` 의존성 경로 갱신
4. workspace members 갱신 (3개 경로 변경)
5. `crates/shared/services/` 디렉토리 완전 삭제 (잔여 파일 없음)

### 검증

```bash
cd hololive/hololive-rs
cargo build --workspace && cargo test --workspace && cargo clippy --workspace --all-targets --all-features -- -D warnings
```

### 결과: 최종 workspace 구조

```
crates/
  alarm/       {core, service, infra, app}                   (4)
  scraper/     {core, service, infra, app}                   (4)
  dispatcher/  {app, notification, formatter, template}      (4)
  shared/      {core, infra}                                 (2)
= 14 crates
```

---

## Phase 2: Go 단일소비자 패키지 이동

- **Risk:** MED
- **소요:** 1-2주
- **의존:** 없음 (Phase 0/1과 병렬 가능)
- **상태:** [ ] 미착수

### 이동 대상 (kakao-bot에서만 사용, 의존 순서대로)

| Step | 패키지 | LOC | 이동 위치 | 사유 |
|------|--------|-----|----------|------|
| 1 | `errors` | 139 | `kakao-bot/internal/errors/` | 의존 없음 |
| 2 | `twitch` | 320 | `kakao-bot/internal/service/twitch/` | config, constants만 의존 |
| 3 | `chzzk` | 1,252 | `kakao-bot/internal/service/chzzk/` | config, constants만 의존 |
| 4 | `matcher` | 789 | `kakao-bot/internal/service/matcher/` | cache, constants, domain 의존 |
| 5 | `notification` | 1,762 | `kakao-bot/internal/service/notification/` | chzzk/twitch 이동 후 수행 |

### 부수 작업

- `providers/alarm_providers.go`에서 kakao-bot 전용 함수(ProvideChzzkClient, ProvideTwitchClient 등)를 `kakao-bot/internal/app/`로 이동
- 각 step 후 `go vet + golangci-lint + go test` 전체 통과 확인

### 이동 불가 패키지 (다중 소비자 확인됨)

- `youtube/outbox`: bot + stream-ingester 양쪽 참조
- `majorevent`: admin(trigger proxy) + llm-sched 양쪽 참조
- `adapter`: 현재 bot 전용이나 Rust formatter와 대칭 구조이므로 shared 유지 검토

### 검증

```bash
cd hololive
for mod in hololive-kakao-bot-go hololive-shared hololive-admin hololive-llm-sched hololive-stream-ingester; do
  (cd "$mod" && go vet ./... && golangci-lint run ./... && go test ./...)
done
```

---

## Phase 3: Go providers 패키지 축소

- **Risk:** MED
- **소요:** 1주
- **의존:** Phase 2 완료 후
- **상태:** [ ] 미착수

### 작업

1. Phase 2에서 이동된 함수 제거 후 잔여 providers 함수 분석
2. 단일 소비자 `Provide*` 함수를 해당 서비스의 `internal/app/` bootstrap으로 인라인
3. `platform/` (1 file, 101 LOC) -> providers에 병합 또는 삭제
4. `llm/` (3 files, 590 LOC) -> llm-sched 전용이면 `llm-sched/internal/`로 이동

### 검증

```bash
# 전체 Go 모듈 vet + lint + test
```

---

## Phase 4: Go hololive-shared 내부 정리

- **Risk:** MED
- **소요:** 1주
- **의존:** Phase 2/3 완료 후
- **상태:** [ ] 미착수

### 작업

1. Phase 2에서 삭제된 5개 패키지 디렉토리 완전 제거
2. `util/` 패키지 정리 (shared-go와 중복 wrapper 제거)
3. `health/` (57 LOC) -> 다중 소비자 확인되어 shared 유지
4. `repository/` (504 LOC) -> 다중 소비자 확인되어 shared 유지
5. `contracts/` 패키지 Go-Rust 계약 동기화 검증

### 검증

```bash
# go vet + lint + test + cross-language contract test
```

---

## Phase 5: shared-go 잔존 패키지 정리

- **Risk:** LOW-MED
- **소요:** 3-5일
- **의존:** 없음 (병렬 가능)
- **상태:** [ ] 미착수

### 현재 잔존 (12개)

dbx, httpclient, json, logging, stringutil, ctxutil, envutil, ginjson, jsonutil, retry, workerpool, telemetry, runtime/automaxprocs

### 작업

1. 각 패키지의 실제 소비자 수 분석
2. hololive-shared 전용 패키지는 hololive-shared로 흡수
3. 단일 소비자 패키지는 해당 모듈로 이동
4. 범용 유틸(json, stringutil 등)은 shared-go에 유지하되 `go mod tidy` 정리

### 검증

```bash
# go vet + lint + test + go mod tidy (전체 모듈)
```

---

## Phase 6: CI 아키텍처 가드

- **Risk:** LOW
- **소요:** 2-3일
- **의존:** 없음 (Phase 2와 병렬 가능)
- **상태:** [ ] 미착수

### 작업

1. depguard 규칙 추가: 모듈 간 금지 import 패턴 정의
2. Rust `cargo tree` 기반 alarm/scraper 교차 의존 금지 체크
3. `build-all.sh`에 아키텍처 검증 단계 추가

---

## 일정 요약

```
Week 1:  Phase 0 (Rust 삭제) + Phase 2 step 1-2 (Go 이동 시작) + Phase 6 (CI 가드)
Week 2:  Phase 1 (Rust 재배치) + Phase 2 step 3-5 (Go 이동 완료)
Week 3:  Phase 3 (providers 축소) + Phase 5 (shared-go 정리)
Week 4:  Phase 4 (shared 내부 정리)
```

### 병렬 실행 가능 조합

```
Phase 0 ──→ Phase 1
              ↓
Phase 2 ──→ Phase 3 ──→ Phase 4
Phase 5 (독립)
Phase 6 (독립)
```

---

## 최종 목표 구조

### Go (go.work): 6개 모듈 유지

| 모듈 | 변화 |
|------|------|
| hololive-shared | ~40K LOC (현재 55K에서 ~15K 이동) |
| kakao-bot | internal에 5개 서비스 패키지 흡수 (~4.3K LOC 증가) |
| admin | shared 의존 축소(서비스 전용 bootstrap/provider 정리 중심) |
| llm-sched | llm 패키지 흡수 가능 (~590 LOC) |
| stream-ingester | 변경 없음 |
| shared-go | 범용 유틸만 잔존, 도메인 컨텍스트 제거 |

### Rust (Cargo.toml): 14 crates (현재 20에서 6개 삭제/재배치)

```
crates/
  alarm/       {core, service, infra, app}
  scraper/     {core, service, infra, app}
  dispatcher/  {app, notification, formatter, template}
  shared/      {core, infra}
```

---

## 성공 지표 (Definition of Done)

1. Rust workspace 20 -> 14 crates (미사용 5개 삭제, 3개 재배치)
2. Go kakao-bot 단일소비자 5개 패키지 internal 흡수 완료
3. Go providers 단일소비자 함수 인라인 완료
4. Go hololive-shared LOC ~25% 감소 (55K -> ~40K)
5. CI 아키텍처 가드: 금지 import 위반 시 빌드 실패
6. 전체 테스트 회귀 0건

---

## PR 운영 규칙

1. PR은 **한 Phase/한 Step**만 변경 (Phase 2는 step별 분리)
2. 기능 변경과 구조 변경 혼합 금지
3. PR마다 포함 사항:
   - 변경 경계 설명
   - 영향받는 서비스
   - 검증 커맨드 결과 (build/test/lint)
   - 롤백 방법

---

## 리스크 및 대응

| 리스크 | 대응 |
|--------|------|
| Phase 1 Rust 경로 변경 후 import 누락 | workspace 전체 빌드+테스트 필수 검증 |
| Phase 2 Go 패키지 이동 시 circular import | 의존 순서(Step 1->5) 엄수 |
| shared-go 정리 시 다른 모듈 빌드 깨짐 | 소비자 수 사전 분석, 단일소비자만 이동 |
| CI 가드 오탐 | 허용 목록(allowlist) 관리 |
