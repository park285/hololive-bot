
# AP 상태머신 이관 작업 공통 규칙 (모든 하위 작업 필수)

## 계획
- 계획 원문: /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation.md (읽기 전용. 수정 금지)
- 저장소: hololive-bot (Go workspace, go 1.27). 모듈: hololive/hololive-api, hololive-alarm-worker, hololive-shared, hololive-dbtest, hololive-youtube-collector. 상위 메타 저장소 /home/kapu/work/iris-stack.

## 품질 우선
- 정확성·완전성·유지보수성이 속도·토큰 절약보다 우선한다. 요청 범위를 끝까지 구현하고 각 수용 기준마다 근거를 남긴다.
- 소유 불변식을 고친다. 기존 저장소 API·패턴을 재사용한다. 증명된 필요 없이 추상화·설정·방어 경로·재시도·fallback을 추가하지 않는다.
- 실패·무효·불명·모호 결과를 보존한다. 부작용 재시도는 durable receipt·idempotency key·소유 토큰·상태 전이 증명이 있을 때만.
- 범위를 몰래 줄이지 않는다. stub·placeholder·TODO 구현 금지. 막히면 정확한 장애물과 시도한 것을 보고.

## 언어·스타일
- 새로 쓰거나 고치는 자연어 주석(문서 주석 포함)은 한국어. 식별자·API 이름·인용 원문·기계 판독 지시문은 그대로.
- 주석은 현재 계약·부수효과·비자명한 이유만. 제거 이력·DEC/감사 연대기·게이트 지시를 주석에 쓰지 않는다.
- 로그는 masked slog, 오류는 fmt.Errorf("action: context: %w", err), context.Context는 첫 인자.
- Go 코드는 아래 Modern Go 지침을 따른다 (go 1.27). 해당 지침이 컴파일 불가·동작 변경이면 건너뛴다.
- SQL 시계: scripts/architecture/check-sql-ownership.py의 CLOCK_QUERY_POLICIES가 경로별 clock_timestamp()/statement_timestamp() 선택을 강제한다. alarm의 member fence·sending·실패 전이는 clock_timestamp().
- migration은 hololive/hololive-api/scripts/migrations/CONVENTIONS.md를 따른다. 이미 적용된 migration 파일은 수정하지 않는다.
- 시험: 소비자가 볼 수 있는 동작·경계·불변식·전이·우선순위·오류를 잡는 시험만. 배선·문자열·소스 텍스트·mock echo·자명한 기본값 시험 금지. 기존 시험이 바뀐 동작을 고정하면 갱신하고, 구현 세부를 고정하는 시험은 삭제. 결정적·격리·전체 suite 안전.
- 게이트 규칙: 체커 자기 시험, grep 가드, 문서-텍스트 단언 시험, 계약 해시 매니페스트를 추가하지 않는다.

## 경계 (위반 금지)
- **경로:** edit/write/read/grep/glob 도구의 상대 경로는 main 체크아웃(/home/kapu/work/iris-stack/hololive-bot)으로 해석된다. 모든 편집·쓰기는 배정 worktree의 절대 경로로 하고, bash는 cwd를 worktree로 지정한다.
- 배정된 worktree 안에서만 편집한다. /home/kapu/work/iris-stack/hololive-bot (main 체크아웃)에는 다른 작업의 미커밋 변경이 있으므로 절대 수정하지 않는다(읽기는 가능).
- git commit·branch·checkout·reset·stash·rebase·merge·push 등 ref/index 변경 금지. 커밋은 부모가 한다. `git diff`/`git status`/`git log` 읽기는 가능.
- 운영 DB·원격 호스트·배포·secret 접근 금지. stack-secrets 값을 읽지 않는다. 시험 DB는 hololive-dbtest의 testcontainers만.
- 새 production 의존성·toolchain 변경 금지. go.work·go.work.sum 수정 금지.
- Iris/ 루트, Iris/native/**, Iris/tools/** 스캔 금지. iris-client-go/README.md와 Iris/docs/contracts/current.md는 읽기 가능.
- tools/cocohook-capture 증거 노출 금지.

## 검증 방식
- **golangci-lint 필수:** 편집을 마친 뒤 worktree 루트에서 `/home/kapu/go/bin/golangci-lint run -c .golangci.yml ./hololive/<변경 모듈>/...` 를 실행해 0 issue로 만든다. 자주 걸리는 규칙: godot(마침표 뒤 새 문장이 소문자 ASCII 단어로 시작하면 위반 → 한국어로 시작하거나 문장 구조를 바꾼다. 주석 마지막은 마침표), wsl_v5(문·대입 사이 빈 줄), govet shadow(err 재선언), goconst, unparam. nolint로 숨기지 않는다.
- 편집 도중 빌드·시험을 반복하지 말고, 편집을 마친 뒤 한 번: gofmt -l, 영향 패키지의 go build/go vet, 영향 패키지 go test(필요시 -race), 바뀐 DB 동작은 hololive-dbtest의 해당 시험을 -run으로 실행. 실패하면 원인을 고치고 실패한 대상만 재실행.
- SQL 변경 시 python3 scripts/architecture/check-sql-ownership.py(또는 .sh)를 실행.
- 버그 수정은 수정 전 실패·수정 후 통과를 확인한다(시험을 먼저 작성해 실패를 관찰).
- 관찰한 명령과 결과만 보고한다. 관찰하지 않은 것은 [INFERENCE]로 표기.

## CHANGELOG
- 작업 지시에 CHANGELOG 담당이 명시된 경우에만 CHANGELOG.md `## 미출시` 맨 위에 한 항목(한국어, `- **제목:** 설명` 형식, 기존 항목 문체)을 추가한다.

## Modern Go 지침 (go 1.27, 전체 목록)
generic_methods: Use generic methods instead of package-level generic helper functions when the operation naturally belongs to the type itself.
json_v2: Use encoding/json/v2 for new JSON code in Go 1.27+; leave existing encoding/json code unchanged unless migration is explicitly requested.
promoted_field_literals: Set embedded struct fields directly with promoted field names in Go 1.27+ struct literals instead of constructing the embedded struct explicitly.
strings_bytes_cut_last: Use strings.CutLast and bytes.CutLast instead of LastIndex plus manual slicing around the last separator.
stdlib_uuid: Use the standard library uuid package instead of third-party libraries or custom UUID implementations when targeting Go 1.27+.
url_clone: Use net/url URL.Clone and Values.Clone methods to copy URLs and URL values instead of manual copying.
new_expression: Use new(value) for pointer fields or arguments instead of generic/type-specific pointer helper functions or temporary variables used only for &value.
errors_as_type: Use errors.AsType[T](err) when checking whether an error matches a specific type.
sync_waitgroup_go: Use wg.Go when spawning goroutines tracked by a sync.WaitGroup.
testing_t_context: Use t.Context() when a test function needs a context tied to the test lifetime.
json_omitzero: Use omitzero on JSON-tagged bool, numeric, struct, and time fields whose zero value should be omitted; keep omitempty for empty strings, slices, and maps.
testing_b_loop: Use b.Loop() for the main loop in benchmark functions.
strings_split_seq: Use strings or bytes SplitSeq and FieldsSeq helpers when iterating over split results.
maps_keys_values_iter: Use maps.Keys or maps.Values directly as iterators instead of manually looping over a map.
slices_collect: Use slices.Collect to build a slice from an iterator.
slices_sorted: Use slices.Sorted to collect and sort iterator values in one step.
time_tick_gc: Use time.Tick when it fits; Go 1.23 can recover unreferenced tickers without requiring Stop for GC.
range_over_int: Use for i := range n when iterating from 0 to n-1.
loopvar_capture: Do not add redundant loop-variable copies before closures or taking addresses; Go 1.22 gives each iteration its own variables.
cmp_or: Use cmp.Or to pick the first non-zero value from a fallback chain.
reflect_type_for: Use reflect.TypeFor[T]() instead of reflect.TypeOf((*T)(nil)).Elem().
http_servemux_patterns: Use method-aware ServeMux patterns and r.PathValue for path parameters.
min_max: Use built-in min and max instead of handwritten comparisons.
clear: Use clear(m) to delete all map entries or clear(s) to zero slice elements.
slices_contains: Use slices.Contains instead of a manual search loop.
slices_index: Use slices.Index to find the index of an element, returning -1 when absent.
slices_index_func: Use slices.IndexFunc to find an element by predicate.
slices_sort_func: Use slices.SortFunc with cmp.Compare instead of sort.Slice for typed comparisons.
slices_sort: Use slices.Sort for slices of ordered values.
slices_max_min: Use slices.Max and slices.Min instead of manual loops over ordered values.
slices_reverse: Use slices.Reverse instead of a manual swap loop.
slices_compact: Use slices.Compact to remove consecutive duplicates in place.
slices_clip: Use slices.Clip to remove unused capacity.
slices_clone: Use slices.Clone to copy a slice.
maps_clone: Use maps.Clone instead of manual map iteration.
maps_copy: Use maps.Copy to copy entries from one map into another.
maps_delete_func: Use maps.DeleteFunc to delete map entries that match a predicate.
sync_once_func: Use sync.OnceFunc instead of sync.Once plus a wrapper closure.
sync_once_value: Use sync.OnceValue to memoize a computed value.
context_after_func: Use context.AfterFunc to run cleanup when a context is canceled.
context_timeout_deadline_cause: Use timeout and deadline contexts with causes when callers need to inspect the cancellation reason.
strings_clone: Use strings.Clone to copy a string without retaining shared backing memory.
bytes_clone: Use bytes.Clone to copy a byte slice.
strings_cut_prefix_suffix: Use strings.CutPrefix or strings.CutSuffix when you need both the trimmed result and whether it matched.
errors_join: Use errors.Join to combine multiple errors while preserving error matching.
context_cancel_cause: Use context.WithCancelCause and context.Cause when cancellation needs to carry an error cause.
fmt_appendf: Use fmt.Appendf when appending formatted text to a byte slice and an intermediate fmt.Sprintf string is unnecessary.
atomic_types: Use typed atomics such as atomic.Bool, atomic.Int64, and atomic.Pointer[T] instead of untyped atomic functions.
any: Use any instead of interface{}.
bytes_cut: Use bytes.Cut instead of bytes.Index plus manual slicing.
strings_cut: Use strings.Cut instead of strings.Index plus manual slicing.
errors_is: Use errors.Is(err, target) instead of err == target so wrapped errors are handled correctly.
time_until: Use time.Until(deadline) instead of deadline.Sub(time.Now()).
time_since: Use time.Since(start) instead of time.Now().Sub(start).

