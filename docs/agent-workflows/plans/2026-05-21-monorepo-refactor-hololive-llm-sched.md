# 2026-05-21 — hololive-llm-sched refactor (Phase 2.C.5)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`.

## Goal

5개 UNLISTED-LARGE 파일을 임계 정의 + 분할로 정리하고, `Scheduler` 동명 충돌과 `LLMClient` 이중 정의를 해소한다. LLM client / 라우터 등록 / scheduler component build 의 중복을 helper 로 추출.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/04-hololive-llm-sched.md`

## Target work

LOC / 함수 budget:
- `internal/service/membernews/filter/filter.go` 389/UNLISTED — 22 helper 를 카테고리별 파일로(scoring, ranking, dedup, source 별).
- `internal/service/majorevent/summarizer/summarizer_consensus.go` 382/UNLISTED — consensus review 단계별 분해.
- `internal/app/internal/runtime/bootstrap_llm_scheduler.go` 378/UNLISTED — `buildLLMSchedulerComponents` 55라인 + `Run` 28라인 — provider/runtime/wiring 별 파일 분리.
- `internal/service/membernews/repository.go` 374/UNLISTED — query/mutation 분리.
- `internal/service/membernews/source_validator.go` 348/UNLISTED — URL validation 과 tier resolution 분리.
- `internal/llm/openai_client.go:59 NewClient` ~46 — 옵션 적용 helper 로.

테스트 보강:
- `cmd/llm-scheduler` (1) — 진입점 최소 테스트.
- `internal/model` (1) — search 모델 라운드트립.
- `internal/service/consensus` (1) — types contract 단언.
- `internal/llm` 5 prod / 1 test — `client.go`, `openai_response_diagnostics.go`, `openai_provider_errors.go`, `openai_fallback.go` 테스트.
- `internal/service/majorevent` 4 prod / 1 test — repository + errors.

네이밍 단일화:
- `internal/llm.Client` vs `summarizer.LLMClient` — 단일 인터페이스 정의 후 summarizer 가 alias 또는 의존.
- `internal/service/{majorevent,membernews}/scheduler/Scheduler` 동명 — 패키지 prefix 명시 또는 type rename(`MajorEventScheduler`, `MemberNewsScheduler`).
- 파일명 = 패키지명 패턴(`scheduler/scheduler.go`, `filter/filter.go`) — 책임별 파일 분리로 자연 해소.
- `llmSchedulerFormatter` vs `LLMSchedulerRuntime` 의 LLM 케이싱 — `LLM` 통일.
- `internal/service/membernews/internal/model/` ↔ `internal/model/` 의 model 트리 통합 또는 명시적 책임 분리.

중복 → 추출:
- 내부 라우트 등록(`api_internal_majorevent.go:37` ↔ `api_internal_membernews.go`) — `registerInternalRoutes(group, apiKey, handlers...)` factory.
- LLM client init(`ProvideMajorEventLLMClient` ↔ `ProvideMemberNewsLLMClient`) — `buildLLMClient(name, cfg)` helper.
- Scheduler 컴포넌트 빌드(`buildMajorEventComponents` ↔ `buildMemberNewsComponents`) — generic builder.
- Repository 분할 컨벤션 — 한 모듈 룰로 통일.

## File map

```
internal/llm/                                     # Client 단일 정의 + fallback/diagnostics 테스트
internal/service/majorevent/                       # scheduler/Scheduler rename, repository 테스트
internal/service/membernews/                       # 동일
internal/service/membernews/filter/                # 책임 분할
internal/service/majorevent/summarizer/            # consensus 분해
internal/app/internal/runtime/                     # bootstrap split, provider/route helper 추출
internal/service/consensus/                        # contract 테스트
internal/model/                                    # 모델 트리 통합 결정
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-llm-sched/...
go test  ./hololive/hololive-llm-sched/...
./scripts/architecture/ci-boundary-gate.sh
```

## Stop rules

- `LLMClient` 인터페이스 통합 시 mock 구현이 깨지면 단언 테스트 우선 작성.
- Scheduler rename 이 외부(cmd/bin, 다른 모듈) import 에 영향을 주면 별도 호환 PR.
- consensus types 변경이 majorevent 출력 결과 의미를 바꾸면 stop.

## Out of scope

- LLM 모델/프롬프트 변경.
- 외부 OpenAI/cliproxy 호출 형식 변경.
- Scheduler 의 cron/주기 정책 변경.
