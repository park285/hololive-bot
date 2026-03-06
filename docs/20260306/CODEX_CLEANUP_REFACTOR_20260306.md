# Codex 레거시 정리 + I/O 최적화 + 모듈화 리팩토링

> 작성일: 2026-03-06
> 범위: 6개 Go 모듈 전수 정적 분석 (5개 병렬 탐색 + 핵심 파일 직접 검증)
> 기반: `CODEBASE_REFACTOR_AUDIT_20260306.md` (P0 전체 완료, P1 부분 완료 후 잔존 문제)

---

## 1. 요약

이전 감사에서 P0(알람 sync/batch/outbox/auth) 전체, P1 일부(matcher index, live batch, fallback executor)를 완료했다.
본 문서는 **잔존하는 3개 축**을 다룬다.

1. **Codex 특유 과도한 방어 코드** (13건) -- receiver nil guard, 이중 nil 체크, 반복 logger 체크
2. **잔존 I/O 병목** (5건) -- 개별 HSet, 순차 milestone 발송, 개별 RecordChange
3. **모듈화/구조 개선** (4건) -- Dispatcher god object, Auth 중복, Scraper 분리, Constants 분할

---

## 2. Phase 1 -- 코드 정리 + HMSet 전환 + 기존 lint 수정 [DONE]

### 2-1. Receiver nil guard 제거 (5건) [DONE]

| ID | 파일 | 패턴 | 상태 |
|----|------|------|------|
| R1 | `kakao-bot-go/internal/app/runtime_runner.go` | `if r == nil { return }` | 제거 |
| R2 | `stream-ingester/internal/app/stream_ingester_runtime_runner.go` | `if r == nil { return }` | 제거 |
| R3 | `shared/pkg/service/cache/errors.go` | `if e == nil` in Error() | 제거 |
| ~~R4~~ | ~~`shared/pkg/service/auth/errors.go`~~ | ~~`if e == nil` in Error()~~ | 유지 (테스트에서 nil receiver 호출 검증) |
| R5 | `shared/pkg/service/holodex/errors.go` | `if e == nil` in Error() (2개) | 제거 |

### 2-2. 반복된 Logger nil 체크 제거 (1건) [DONE]

| ID | 파일 | 패턴 | 상태 |
|----|------|------|------|
| L1 | `kakao-bot-go/internal/app/runtime_runner.go` | `if r.Logger != nil` 5회 반복 | 제거 |
| ~~L2~~ | ~~`alarm_service.go`~~ | ~~`if as == nil`~~ | 유지 (테스트에서 nil receiver 호출 검증) |

### 2-3. 이중 nil 체크 제거 (1건) [DONE]

| ID | 파일 | 패턴 | 상태 |
|----|------|------|------|
| N1 | `shared/pkg/service/template/renderer.go` | formatNumber/formatNumberKR 첫줄 `if v == nil` | 제거 (toInt64 내부 nil 체크로 충분) |
| ~~N2~~ | ~~`renderer.go` toInt64 IsNil()~~ | | 유지 (typed nil pointer 역참조 방지에 정당) |

### 2-4. replaceHashMappings HMSet 전환 (1건) [DONE]

- `alarm_platform_mapping.go`: for-loop HSet -> HMSet 1회 호출 (RTT N+1 -> 2)

### 2-5. 기존 lint 이슈 수정 [DONE]

**kakao-bot-go (7건 -> 0건)**
| 이슈 | 파일 | 수정 |
|------|------|------|
| rangeValCopy 192B | `handler_live.go:149` | index 접근으로 전환 |
| rangeValCopy 192B | `chzzk/client.go:388` | index 접근으로 전환 |
| shadow err (2건) | `alarm_platform_mapping.go:178,184` | 변수명 `delErr`로 변경 |
| unused func | `matcher.go:108 tryExactAliasMatch` | dead code 삭제 |
| unused field | `alarm_types.go:69 cacheMutex` | dead code 삭제 |
| wrapcheck | `matcher.go:258 singleflight.Do` | `fmt.Errorf` 래핑 추가 |

**shared (9건 -> 2건, 잔여 2건은 구조 리팩토링 필요)**
| 이슈 | 파일 | 수정 |
|------|------|------|
| gci/goimports (3건) | `dispatcher.go`, `scheduler.go` | import 그룹 정렬 |
| httpNoBody | `alarm/client.go:184` | `nil` -> `http.NoBody` |
| SA4010 unused append | `scheduler.go:232` | 미사용 변수 `statsChannelIDs` 제거 |
| wrapcheck (2건) | `service.go:222,521` | `fmt.Errorf` 래핑 추가 |
| unused (9건) | `dispatcher_delivery_flow.go`, `dispatcher_send.go`, `dispatcher_claim.go` | per-room 전환 완료 후 dead code 삭제 |

**llm-sched (4건 -> 0건)**
| 이슈 | 파일 | 수정 |
|------|------|------|
| gci/goimports (4건) | `summarizer.go`, `types.go` | --fix 자동 수정 |

**잔여 (별도 PR 대상, 구조 리팩토링 필요)**
- `scheduler.go:169` funlen (101 > 90) -- Phase 2 배치화 시 자연 해소
- `yt_initial_data_extractor.go:41` gocognit (38 > 31) -- PR-A scraper 분리 시 해소

### 검증 결과
- 6개 모듈 `go build`: 전체 성공
- 6개 모듈 `golangci-lint`: kakao-bot 0, shared 2(구조적), dispatcher 0, llm-sched 0, stream-ingester 0
- 6개 모듈 `go test`: 전체 통과

---

## 3. Phase 2 -- I/O 최적화 [DONE]

### 3-1. Milestone/Approaching 병렬 발송 + batch 마킹 [DONE]

| ID | 파일 | 문제 | 개선 | 상태 |
|----|------|------|------|------|
| B3 | `scheduler.go` SendMilestoneAlerts | N milestone x M room 순차 발송 + 개별 MarkMilestoneNotified | errgroup.SetLimit(4) 병렬 발송 + MarkMilestonesNotifiedBatch | 완료 |
| B5 | `scheduler.go` sendApproachingAlerts | B3과 동일 패턴 (approaching alerts) | 동일 해법 적용 + MarkApproachingChatNotifiedBatch | 완료 |

### 3-2. RecordChange 배치화 [DONE]

| ID | 파일 | 문제 | 개선 | 상태 |
|----|------|------|------|------|
| B4 | `scheduler.go` processChannelStats | 채널당 개별 RecordChange INSERT | RecordChangeBatch multi-value INSERT + caller가 수집 후 batch | 완료 |

### 3-3. collectRoomsByChannel pipeline화 [DONE]

| ID | 파일 | 문제 | 개선 | 상태 |
|----|------|------|------|------|
| B2 | `outbox/dispatcher.go` | 채널당 SMembers 개별 호출 (최대 50회, RTT N) | DoMulti pipeline 배치 조회 (RTT 1) | 완료 |

### 부수 개선
- `scheduler.go` trackAllSubscribers funlen 101>90 해소 (prepareWorkItems + processAndRecordChanges 분리)
- `scheduler.go` Go 1.22+ forvar lint 경고 3건 수정 (불필요한 loop variable copy 제거)

### 검증 결과
- 6개 모듈 `go build`: 전체 성공
- 6개 모듈 `go test`: 전체 통과
- 6개 모듈 `golangci-lint`: shared 1건(기존 gocognit, scraper 구조 리팩토링 대상)

---

## 4. Phase 3 -- 구조 개선

### 4-1. Auth 중복 제거

| 파일 | 줄 수 | 유사도 |
|------|------|--------|
| `shared/pkg/service/auth/service.go` | 572 | 기준 |
| `kakao-bot-go/internal/service/auth/service.go` | 571 | 0.99 |

- kakao-bot에서 shared 버전 import로 전환
- 로컬 auth/service.go 삭제
- 설계 초안: `docs/20260306/AUTH_CORE_UNIFICATION_DRAFT_20260306.md` (이미 작성됨)

### 4-2. Constants 파일 분할

| 현재 | 분할 후 |
|------|---------|
| `constants/constants.go` (581줄, 27 struct) | `constants/cache.go`, `constants/api.go`, `constants/network.go`, `constants/limits.go` |

### 4-3. Dispatcher MessageFormatter 추출

| 현재 | 분리 |
|------|------|
| `outbox/dispatcher.go` (1,167줄, 57메서드) | MessageFormatter (11메서드, ~320줄) 우선 추출 |

### 4-4. YouTube fallback Policy 표준화

| 현재 | 개선 |
|------|------|
| YouTube service: 인라인 `len(failedIDs) > 0` + quota | fallback.Policy 객체 사용으로 Holodex와 일관화 |

### 완료 기준
- Auth: shared import 전환, 로컬 삭제, 테스트 통과
- Constants: import 경로 변경 없음 (동일 패키지 내 분할)
- Dispatcher: MessageFormatter struct 추출, 기존 테스트 통과
- Fallback: Policy.ShouldRun() 사용, quota 로직은 caller에 유지

---

## 5. 실행 순서

```
Phase 1 (코드 정리 + HMSet)
  -> Phase 2 (I/O 최적화)
    -> Phase 3 (구조 개선)
```

---

## 6. 비고

- 신규 라이브러리 도입 없음 (기존 errgroup, singleflight, pgx.Batch 재사용)
- Phase별 독립 커밋 (rollback 가능 단위)
- Phase 4 (별도 PR) 항목: `docs/20260306/CODEX_CLEANUP_SEPARATE_PR_20260306.md` 참조
