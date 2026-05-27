# P4 Handoff: scraping/ sub-package split

## Status: COMPLETE

## Context

- **Plan**: `/home/kapu/.claude/plans/dazzling-scribbling-tower.md` → P4
- **Branch**: `refactor/p4-scraping-split` (in `hololive-bot` submodule)
- **Scope**: `hololive/hololive-shared/pkg/service/youtube/scraper/internal/scraping/`
- **Goal**: 35 source files, 5,352 LOC flat package → cohesive sub-packages

## Result

5,352 LOC → 3,866 LOC root + 3 sub-packages (1,450 LOC extracted, -27.8%).
Root는 Client struct 69개 메서드 (12 파일) + proxy/failure/snapshot/state 인프라.

### Final Structure

```
scraping/
├── backoff/      (292 LOC) — BackoffState, BackoffOption, jitter support
├── parser/       (995 LOC) — 8개 데이터 타입 + 6개 HTML/RSS 파서
├── ratelimiter/  (163 LOC) — RateLimiter, local+distributed wait
├── (root)        (3,866 LOC) — Client, proxy, failure, snapshot, state, content methods
```

### Extraction Log

| Wave | Sub-package | LOC | Commit | Pattern |
|---|---|---|---|---|
| 1 | `backoff/` | 292 | 44626f2d | type alias + var alias, testing helper export |
| 2 | `parser/` | 995 | 44626f2d | type alias + var alias, checkAlerts wrapper injection |
| 3 | `ratelimiter/` | 163 | 25c889bd | type alias + var alias, distributedBucketFromURL root 유지 |

### Deferred Targets

| Target | LOC | Reason |
|---|---|---|
| `failure/` | ~168 | FailureReason/Source/Detail 109건 참조, ClassifyFailure는 root 타입 의존 |
| `snapshot/` | ~239 | failure/ 추출 선행 필요 (FailureReason/Source 순환 방지) |
| `proxy/` | ~489 | WithProxy(ClientOption) → *Client 순환 의존 |
| `goscrapy/` | ~325 | goscrapyRunner 인터페이스가 *Client 참조 |

### Design Decisions

1. **checkAlerts wrapper injection**: parser/ 추출 시 `checkAlerts`(root의 sentinel error 의존)를 parser에 이동 불가. root의 `parseVideosFromInitialData`, `parseUpcomingEventsFromInitialData` wrapper가 checkAlerts 호출 후 parser 함수에 위임.
2. **distributedBucketFromURL root 유지**: `ytDefaults` (config 패키지) 참조로 ratelimiter/ sub-package에 이동 불가.
3. **DistributedLimiter interface export**: 원래 unexported `distributedLimiter`를 `ratelimiter.DistributedLimiter`로 export. Go structural typing으로 기존 caller 무수정.
4. **BackoffState testing helper**: `SetTransientCooldownForTest`, `TransientErrors` export하여 root 테스트에서 unexported 필드 접근 대체.

## Verification

```
go build ./...scraper/... — pass
go build ./...providers/... — pass (ConfigureDistributed 호출부)
go test ./...scraping/... — 44.4s all pass
./scripts/architecture/ci-boundary-gate.sh — pass
```
