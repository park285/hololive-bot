# 일정 포스터 행 저장·`!스케쥴` 카드·변경 감지 구현 인계

작성: 2026-11-14. 사용자 지시 `"이제 구현시작"` 이후 기존 코드를 조사하고 저장소 계층 초안까지 썼다. 그 뒤 `"니가 하지말고 이제 구현관련 핸드오프 문서 작성해줘"`에 따라 구현을 멈췄다. 구현 진행과 실제 검증 결과는 이 파일에 이어서 적는다.

## 0. 읽을 것

| 문서 | 경로 | 용도 |
|---|---|---|
| 원 계획 | `/home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-11-13-schedule-poster-rows-and-change-detection.md` | 설계(180–230행), 작업 순서(231–248행), 사용자 결정(282–287행) |
| 설계 인계 | `/home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-11-13-schedule-poster-rows-and-change-detection-handoff.md` | §3.5 프롬프트·모델 실측, §4 카드 V6, §5 남은 작업, §6 승인 경계 |
| 프로토타입 | `/var/tmp/schedule-card-proto/` (Git 아님) | 프롬프트·스키마(`prompts/`), 하니스(`research/run.py`), 카드 렌더러(`*.go`), 글꼴(`fonts/`) |
| 저장소 규칙 | `../AGENTS.md`, `AGENTS.md`, `/home/kapu/work/iris-stack/.agents/workflows.md`, `hololive/hololive-api/scripts/migrations/CONVENTIONS.md` | Git·검증·마이그레이션 규칙 |

- 두 계획 문서는 원래 checkout에만 있다(미추적). 이 worktree에는 없다.

### 0.1 필수 스킬

작업을 시작하기 전과 해당 단계에 들어갈 때 `SKILL.md` 진입점과 작업에 관련된 참조 문서를 읽는다. 스킬의 권고가 배포나 마이그레이션 실행 권한을 주지는 않는다(§7).

| 스킬 | 경로 | 언제 | 이 작업에서 지킬 것 |
|---|---|---|---|
| `use-modern-go` (모던화, **필수**) | `/home/kapu/.omp/plugins/cache/plugins/goland-claude-marketplace___modern-go-guidelines___1.1.1/skills/use-modern-go` | Go 파일을 만들거나 고칠 때마다 | hololive-bot `AGENTS.md`가 요구한다. 파일마다 먼저 `sh "$(realpath <스킬 경로>)/scripts/run-tool.sh" list --file-path <파일>`을 실행하고, 출력을 head·tail·grep 없이 전부 읽은 뒤 지침을 따른다. 대상은 Go 1.27.4(`.go-version`)다. 새 JSON 코드는 `encoding/json/v2`, 테스트는 `t.Context()`, 루프는 `range n`, 오류 형 검사는 `errors.AsType`, 고루틴은 `wg.Go`, 포인터 값은 `new(value)`를 쓴다(2026-11-14 `list` 출력 기준). |
| `executing-plans` (**필수**) | `/home/kapu/.agents/skills/executing-plans` | 이 문서로 구현을 시작하거나 이어갈 때 | 현재 코드와 결과를 먼저 확인하고 시작한다. 진행 상황, 실제로 실행한 검증, 막힌 점은 이 파일 §8에 적는다. 끝내기 전에 diff를 검토하고 바뀐 부분에 맞는 검사를 실행한다. |
| `go-secure-coding` (**필수**, 보안 경계) | `/home/kapu/.agents/skills/go-secure-coding` | 외부 입력이 드는 코드를 만들거나 검토할 때 | 대상은 다음과 같다. 원본 이미지 내려받기, FxTwitter 글 본문, 모델 응답 JSON(신뢰할 수 없는 데이터로 다룸, 크기·개수 상한, 프롬프트 인젝션), 카드에 그리는 모델 출력 글, Decisions REST 응답 파서(400 본문 두 형식), 설정 파일 검증. 기존 `netguard` 호스트 제한과 `calendar_photo_security.go`를 재사용한다. |
| `secrets-scan` | `/home/kapu/.agents/skills/secrets-scan` | 커밋 요청을 받아 커밋하기 전 | 설정 예시, 테스트 fixture, 로그 문구에 키·토큰·프로젝트 비밀이 섞이지 않았는지 본다. ADC 키 파일과 `gemini.env` 값은 출력하지 않는다. |
| `pg-optimization` | `/home/kapu/work/iris-stack/.agents/skills/pg-optimization` | 쿼리 계획, 인덱스, 잠금을 바꿀 때만 | 지금 초안은 기본 키 조회와 수백 행 규모라 인덱스를 두지 않았다(migration 010과 같은 판단). 변경 감지의 판별 기록 표나 변경 기록 표에 조회 조건이 늘면 이 스킬로 판단한다. 운영 DB 조회는 이 스킬의 읽기 전용 가드를 따른다. |
| `context7-docs` | `/home/kapu/work/iris-stack/.agents/skills/context7-docs` | genai SDK 등 외부 API에 남은 공백이 있을 때만 | 순서는 iris-stack `AGENTS.md`를 따른다: 저장소의 고정된 사용 예(`google.golang.org/genai` v1.77.0, `vertex.go`·`gemini.go`) → 공식 문서·소스(`/home/kapu/go/pkg/mod/google.golang.org/genai@v1.77.0`) → 그래도 남은 공백만 이 스킬. 예: `ResponseSchema`의 `PropertyOrdering`·`Nullable` 직렬화. |
| `better-ui` | `/home/kapu/work/iris-stack/.agents/skills/better-ui` | 카드 디자인을 손볼 때(판형 확정 뒤 수정 요청 등) | 표기 원칙(§4.4)은 그대로 지킨다. 시안과 다르게 그리면 PNG를 다시 만들어 보고한다. |
| `hololive-bot-ops` | `/home/kapu/work/iris-stack/.agents/skills/hololive-bot-ops` | 배포 승인을 받은 뒤에만 | 운영 migration 적용, image 배포, 설정 파일 교체(`translation.rows`)는 이 스킬 절차로 한다. 코드 작업 중에는 부르지 않는다. |
| `stack-platform-ops` | `/home/kapu/work/iris-stack/.agents/skills/stack-platform-ops` | 운영 DB 읽기, 운영 데이터 교정, stack-secrets 접근이 필요할 때 | 운영 `members` 교정과 용어집 추가는 별도 승인 뒤 이 스킬로 한다. stack-secrets는 이 스킬을 거쳐서만 접근한다. |
| `stack-pointer` | `/home/kapu/work/iris-stack/.agents/skills/stack-pointer` | 사용자가 명시적으로 요청할 때만 | iris-stack 메타 저장소의 하위 모듈 포인터 커밋이다. push하지 않는다. |

- 서브에이전트(`subagent-driven`, `ultracode`)는 사용자가 명시적으로 허락할 때만 쓴다. 허락하면 이 표와 §0.2 규칙을 작업 지시에 그대로 넘긴다. 서브에이전트는 `AGENTS.md`를 물려받지 않는다.

### 0.2 필수 컨벤션 (별도 스킬 없이 규칙 문서로 강제됨)

| 출처 | 이 작업에서 지킬 것 |
|---|---|
| hololive-bot `AGENTS.md` | <ul><li>컴파일·테스트·빌드는 kapu에서만 한다.</li><li>주석은 기본적으로 달지 않는다. 이름·시그니처·코드로 알 수 없는 사실(숨은 불변식, 순서·잠금, 외부 제약, 보안 이유, 실패의 의미)만 한국어로 쓴다.</li><li>주석에 변경 이력, 결정 ID, PR·이슈 번호, 날짜, 지운 코드 언급을 넣지 않는다. 이런 내용은 커밋 메시지에 쓴다.</li><li>Go doc 주석은 이름으로 시작하는 짧은 문장이고 `@param` 같은 태그를 쓰지 않는다.</li><li>로그는 마스킹한 slog를 쓴다. 오류는 `fmt.Errorf("action: context: %w", err)` 꼴이고, `context.Context`는 첫 인자다.</li><li>lint 경고는 원인을 고친다. 억제는 이름 붙은 오탐일 때만 최소 범위로, `nolintlint`를 지키며 한다. NilAway와 race 검사는 막는 단계(blocking)로 둔다.</li></ul> |
| iris-stack `AGENTS.md`, `.agents/workflows.md` | <ul><li>소스는 이 worktree에서만 고친다.</li><li>Git 쓰기는 checkout마다 쓰는 쪽이 하나다. ref를 바꾸기 전에 Git 프로세스, lock, `git reflog -5`를 확인한다.</li><li>실패 경로는 정규 경로 하나만 둔다. 재시도·대체 경로·기본값 동작에는 계약과 근거가 필요하다(§3의 텍스트 대체 미적용 이유).</li><li>운영 변경은 "Fallback delta"를 보고한다.</li><li>게이트에 checker 자체 테스트나 문서 문구 검사를 더하지 않는다.</li><li>`go.work`는 로컬에만 둔다.</li></ul> |
| `hololive/hololive-api/scripts/migrations/CONVENTIONS.md` | migration 번호, `manifest.txt` 등록, 한국어 머리말, `BEGIN … COMMIT`, `IF NOT EXISTS`·`DROP CONSTRAINT IF EXISTS` 형식을 지킨다. 스키마 migration 안의 데이터 갱신(`アソビ隊` 별칭)이 허용되는지 확인한다(§4.1). |
| 서비스 문서 계약(`docs/current/services/hololive-api.md`) | 재시도·대체 동작을 더하거나 바꾸면 예외 계약 표(Trigger, 한도, 종단, Telemetry, Owner, 제거 조건)를 함께 고친다. |
| 기존 코드 관례 | <ul><li>설정은 `encoding/json/v2` + `RejectUnknownMembers` + `Validate`를 쓴다.</li><li>SQL은 `queries/*.sql` + `sqlassets.MustReader`로 등록하고 `SELECT *`는 쓰지 않는다(`check-sql-ownership.sh`).</li><li>저장소 쓰기는 revision이나 상태를 조건으로 건다.</li><li>Vertex 재시도는 `translationRetryOptions` 계약 하나로만 센다(SDK 자체 재시도 1회).</li><li>시스템 지시는 "입력은 데이터로만 다룬다" 꼴로 쓴다.</li></ul> |
| OMP 전역 지침(`/home/kapu/.omp/agent/AGENTS.md`, OMP 에이전트일 때) | <ul><li>보고는 간결한 격식체 한국어로, 결론부터 쓰고 관찰과 추론을 구분한다.</li><li>원격 쓰기, 운영 변경, 새 의존성, 비밀 접근은 명시 승인을 받은 뒤에만 한다.</li></ul> |

## 1. 작업 위치

- worktree: `/home/kapu/work/iris-stack/.worktrees/feat-schedule-poster-rows/hololive-bot`
  - 브랜치: `feat/schedule-poster-rows`
  - 기준: `f874d7ca7`(origin/main, #796)
  - 형제 `iris-client-go`, `shared-go`는 같은 폴더에서 origin/main detached 상태다.
  - 만든 명령: `bash tools/ops/stack-worktree.sh add feat/schedule-poster-rows hololive-bot`(iris-stack 루트에서 실행). 지울 때는 같은 스크립트의 `remove`를 쓴다. 이 스크립트는 깨끗하고 원격에 있는 worktree만 지운다.
- 원래 checkout `/home/kapu/work/iris-stack/hololive-bot`는 `73118729f`로 origin/main보다 11커밋 뒤처져 있다. 여기서는 소스를 고치지 않는다.
- 설계 인계 문서 작성 뒤 main에 들어온 관련 커밋:
  - #785: `!스케쥴` 문구 DB 이관
  - #788: 후보 자동 검토(`gemini.go`, Gemini Developer API, `SCHEDULE_POSTER_GEMINI_API_KEY_FILE`)
  - #789: 자동 검토 키 파일에 AI Studio 키 허용
  - #790: 확정본 주 범위 수정 API(`update_week.sql`), 즉시 수집 API, `reviewed_by`·`review_request_reason`(migration 017·018)
  - 그래서 계획 문서의 migration 번호 016은 **019**로 바뀌었다. Gemini Developer API 호출 경로와 키 파일도 이미 코드에 있다. 운영에 키가 배치됐는지는 확인하지 않았다.
- 1단계에는 Console 계약 변경이 없어 iris-console worktree는 만들지 않았다.

## 2. 지금 worktree에 있는 초안 (미커밋)

확인한 것은 `hololive-shared`의 `go build ./pkg/service/scheduleposter/`와 `go vet` 통과뿐이다. dbtest, schema golden, 단위 테스트, lint는 실행하지 않았다.

| 파일 | 상태 | 내용 |
|---|---|---|
| `hololive/hololive-api/scripts/migrations/019_schedule_poster_rows.sql` | 새 파일 | 아래 §4.1 |
| `hololive/hololive-api/scripts/migrations/manifest.txt` | 1줄 추가 | `019 019_schedule_poster_rows.sql` |
| `hololive/hololive-shared/pkg/service/scheduleposter/rows.go` | 새 파일 | 아래 §4.2 |
| `…/scheduleposter/queries/{pending_rows,record_rows,record_rows_failure,delete_rows,insert_rows,week_terms,published_rows}.sql` | 새 파일 7개 | 아래 §4.2 |
| `…/scheduleposter/queries/reject.sql` | 수정 | 거절 시 `rows_status='pending'`, `rows_error=''`, `rows_extracted_at=NULL`로 바꾼다. 행 묶음은 호출한 쪽(`store.go`의 `reject`)이 같은 트랜잭션에서 `delete_rows.sql`을 별도 문장으로 실행해 지운다(한 문장의 CTE로는 커밋 전이던 `RecordRows`의 행을 지우지 못한다). |
| `…/scheduleposter/queries/request_translation.sql` | 수정 | 재번역을 예약하면 `rows_status='pending'`이 되어 재추출도 함께 예약된다. 이전 행 묶음은 남는다. |
| `…/scheduleposter/queries/update_week.sql` | 수정 | 주 범위를 고치면 `pending`으로 되돌리고, `UpdateWeek`가 같은 트랜잭션에서 `delete_rows.sql`로 행 묶음을 지운다. 틀린 주로 뽑은 날짜를 게시하지 않기 위해서다. |
| `…/scheduleposter/sql.go` | 수정 | 새 쿼리 7개 등록 |
| `hololive/hololive-api/internal/planes/admin/internal/scheduleposter/prompts/{extract,translate}.txt` | 새 파일 | 프로토타입 채택본과 바이트 단위로 같다. 지금 SHA-256 앞 12자리: `extract.txt` `a9f950f8b38b`, `translate.txt` `06d2d03104db`(`translate_x1.txt`), `match.txt` `5094150c9971`(2단계에서 더함). 바뀐 경위는 §8 진행 기록에 있다. |
| `go.work.sum` | 1줄 추가 | `go build`가 덧붙인 줄이다. 기능과 무관하므로 커밋 전에 되돌린다. |

### 초안에서 알고 있는 빈 곳

1. **DB CHECK와 같은 Go 검증이 없다.** `encodeRows`는 행 수(1..100)와 status만 검사하고 글 길이와 목록 개수는 보지 않는다. 그런데 이 상한(§4.1)을 넘는 모델 출력이 `RecordRows`까지 가면 DB 오류가 난다. 그러면 포스터가 `pending`에 머물러 tick마다 모델을 다시 부른다(비용이 계속 든다). `ValidateRows([]Row) error`를 공개하고 admin이 기록 전에 시도 안에서 불러 실패를 재시도·종단 처리하게 해야 한다. `RecordRows`도 같은 함수를 쓴다.
2. 다음은 가정일 뿐 검증하지 않았다. dbtest로 확인한다.
   - json v2의 `format:DateOnly`(`Row.Day`)
   - nil 슬라이스가 `[]`로 직렬화되는지
   - `jsonb_to_recordset`이 JSON 배열을 `text[]`로 읽는지
   - 이름 붙은 문자열 타입 `RowStatus`로 Scan되는지
3. `week_terms.sql`은 같은 주를 덮는 다른 확정본의 `terms`를 저장 순서대로 돌려준다. 첫 값이 이기게(`setdefault`) 합치는 것은 admin의 책임이다.
4. `published_rows.sql`은 `rows_extracted_at IS NOT NULL`인 확정본을 고른다. `published_image.sql`은 번역 이미지가 있는 확정본을 고른다. 그래서 전환 기간에는 카드와 이미지의 출처 포스터가 다를 수 있다.

## 3. 결정 상태

### 확정 (근거 있음)
- 구현 착수와 격리 worktree 사용: `"이제 구현시작"`.
- 프롬프트는 최종본을 해시 그대로 이식한다. 리뷰어 검증과 모델 검증은 끝났다(설계 인계 §3.5).
- `ホロドリ → 홀로도리`는 운영 설정 glossary에 이미 있다.
- 키비주얼(프로필 원형 창)과 테마별 글꼴은 가독성·디자인 개선 목적으로 승인됐다(설계 인계 §6). 저장소 편입은 이 구현 단계에서 한다.
- 행 추출과 번역은 두 호출로 나눈다. 추출 행은 저장하고, 재추출하면 묶음 전체를 바꾼다(원 계획 설계).
- 추출·번역 단계의 모델 파라미터(모델, thinking level, temperature, 호출 상한)는 단계별 설정으로 분리한다: `"번역 모델 추출모델등의 파라미터값은 세팅분리가 당연함"`. 설계는 §4.3 "단계별 설정".
- 추출 기본값은 `gemini-4-hydrogen` `LOW`다: `"추출은 하이드로젠 low가 맞는듯한데"`. 근거는 그림 34장 × 3개 모델 × 2회 비교다(설계 인계 §3.5 "추출 전체 비교"). 날짜·시각 정확도는 세 모델이 같았다. hydrogen은 타임아웃 0회, 최대 24.8초였고 상태 규칙에서 벗어난 행이 가장 적었다. 칸 도장 배지 부작용도 가장 작았다. 이 결정은 뒤의 추출 모델 실험으로 `gemini-4-carbon` `LOW`·900초·3회 다수결로 바뀌었다(§8 마지막 기록).
- 변경 감지는 **자동 반영**으로 한다: `"자동반영으로 진행(대규모 데이터 테스트 결과 이슈 없었음 검증됬으니)"`.
  - 측정 근거(원 계획 "측정 결과"·"추가 실호출 검증"): 판별 결합 규칙은 147건에서 정밀도 0.956, 재현율 1.0이었다. 다른 멤버 글과 커뮤니티 글을 더한 229회 호출에서도 맞았다. 행 대응은 Vertex hydrogen `LOW`로 27/27, Go 비교는 17건 모두 정답과 같았다.
  - 같은 지시에 "하루 2~3회 이상 활동하는 경우의 처리"를 더 따져 보라는 요청이 있었다. 처리 규칙은 §4.6이다.
- 행이 아직 없는 확정본의 `!스케쥴`은 기존 "없음" 안내 문구로 답한다(아래 결정 1의 (a), 2026-11-14 사용자 선택). 대체 경로와 예외 계약을 두지 않는다.
- 칸 도장 배지 B안을 적용하고 모델로 재검증한다(2026-11-14 사용자 선택 "적용하고 재검증"). B안은 `extract.txt`의 `badges` 규칙에 "여러 행이 함께 쓰는 날짜 칸 표시는 어느 행의 배지로도 적지 않고, 내용으로 종류를 추측하지 않는다. 칸에 행이 하나뿐이면 그 표시는 그 행의 배지다"를 더하는 것이다. 결과는 §8에 적는다.
- 2단계(변경 감지)의 표본 수집(FxTwitter)과 실제 모델 시험(Vertex, Gemini)을 승인받았다(2026-11-14 사용자 선택 "지금 승인"). 1단계가 끝나는 대로 실행한다.
- 판형은 미리보기 PNG를 본 뒤 사용자가 정한다. 그때까지 기본값은 3:4다(2026-11-14 사용자 선택 "미리보기 보고 결정").
- 서브에이전트와 워크플로 사용을 허락받았다(2026-11-14). 워커 계열은 되도록 Opus를 쓴다.
  - 미검증은 세 가지다. ① 최종 `match.txt`(`f10c1c4c8ce8`)는 다시 쓴 뒤 모델로 돌려 보지 않았다. 측정에 쓴 표본은 이전 지시문으로 돌린 것이다. ② 번역의 `[update: …]` 행은 직접 만든 4건으로만 확인했다. ③ 카드의 변경 표시는 시안 데이터로만 그려 봤다.

### 구현자가 기본값으로 잡은 것 (사용자 미확인, 결과 보고에 반드시 명시)

| 항목 | 기본값 | 근거·대안 |
|---|---|---|
| 번역 기본값 | `gemini-4-hydrogen` `MEDIUM`, 검색 없음, temperature 미설정 | 설계 인계 §3.5의 권고이고, 사용자가 명시적으로 확정하지는 않았다. 근거는 다음과 같다. carbon `LOW`는 결과가 다른 41행 눈 판단에서 약 6행 대 10행으로 뒤졌다. 행에 없는 내용을 채우는 실수가 더 많았다(T12 10건 대 4건, `doubt_ko` 17행 대 8행). 검색을 붙이면 더 나빠졌다. 확실한 이점은 속도(중앙값 9.4초 대 21.7초)뿐인데, 백그라운드 작업이라 의미가 작다. argon은 12월부터 단가가 두 배다. 모델은 설정으로 바꿀 수 있다. 다음 가운데 하나가 생기면 carbon을 아직 보지 않은 새 포스터로 블라인드 비교한다. ① 용어집·고정 어휘로 고칠 수 없는 hydrogen 오역이 반복될 때 ② hydrogen 단가·가용성이 바뀔 때 ③ carbon의 12월 이후 단가가 확인될 때. 이미 본 검증용 포스터로 다시 재면 프롬프트 수정 효과가 부풀려진다. |
| 응답 형식 | 카드만 보낸다. 텍스트 대체는 하지 않는다. | 텍스트 대체를 넣으려면 `image_text_fallback` 예외 계약이 필요하다(원 계획 220행). 렌더 실패는 기존 명령 오류 경로를 탄다. |
| 운영 `members` 교정(짧은 이름 12명, IRyS) | 범위 밖 | 운영 데이터 쓰기 승인이 별도로 필요하다. migration 019에는 계획에 있던 `アソビ隊` 별칭만 넣었다. |

### 사용자에게 받아야 할 결정 (2026-11-14에 답을 받았다. 결과는 위 "확정"에 있다)

1. **행이 아직 없는 확정본의 `!스케쥴`**
   - (a) 안내 문구: 행만 쓰는 깔끔한 전환이다. 다만 확정 직후 약 1~2분과 추출 실패 동안은 번역 이미지가 있어도 보이지 않는다.
   - (b) 전환 기간에만 기존 번역 이미지 사용: 대체 경로이므로 서비스 문서에 계약 행이 필요하고, 6단계에서 지운다.
   - 어느 쪽이든 6단계 전까지는 이미지 번역 경로가 계속 돌아 비용이 든다.
2. 판형(16:9, 4:3, 3:4)과 V6 디자인 확인.
3. B안 적용 여부(프롬프트 수정과 재검증에는 모델 실행 승인이 필요하다).

## 4. 설계 계약

### 4.1 DB (migration 019, 초안 작성됨)

- `schedule_posters`에 세 열을 더한다.
  - `rows_status text NOT NULL DEFAULT 'pending'`. 값은 `pending`, `succeeded`, `failed`(CHECK).
  - `rows_error text NOT NULL DEFAULT ''`
  - `rows_extracted_at timestamptz`
  - `rows_revision bigint NOT NULL DEFAULT 1`
  - CHECK: `succeeded`면 `rows_extracted_at`이 있어야 한다. "`rows_extracted_at`이 있으면 확정본" 조건은 두지 않는다. 이전 image로 되돌렸을 때 확정본 거절이 CHECK에 걸리기 때문이고, 행을 읽는 쿼리가 모두 `review_status='confirmed'`로 거른다.
- 새 표 `schedule_poster_rows`
  - PK는 `(poster_id, ordinal)`. FK `poster_id → schedule_posters(id) ON DELETE CASCADE`이므로 60일 purge 때 함께 지워진다.
  - 열: `day date`, `start_time`·`end_time text NULL`(`^([01]?[0-9]|2[0-9]):[0-5][0-9]$`, 25:00 같은 표기를 그대로 두되 29:59까지), `tz`, `status`(`planned`/`tentative`/`cancelled`/`off`), `source_text`, `badges text[]`, `title_ko`, `with_ko text[]`, `place_ko`, `tags_ko text[]`, `note_ko`, `terms jsonb`(배열), `doubt_ko`, `warnings text[]`.
  - 상한: `ordinal` 1..100. 글은 1000자 이하(`source_text`·`title_ko`는 1자 이상), `tz`는 40자 이하. 배열은 각각 40개 이하.
- `members`의 `asobi-mawaritai`에 `aliases.ja` `アソビ隊`를 넣는다. 이미 있으면 넣지 않는다.
  - **확인 필요 1:** `CONVENTIONS.md`가 스키마 migration 안의 데이터 갱신을 허용하는지 본다.
  - **확인 필요 2:** 멤버 캐시는 epoch 신호가 있어야 다시 읽는다. migration만으로는 캐시가 갱신되지 않고 배포 재시작 때 반영된다[추론].
- 상태 전이(행 쓰기는 `revision`·`updated_at`을 바꾸지 않는다. 몇 시간 걸리는 이미지 번역의 결과 기록과 Console의 revision 조건을 깨지 않기 위해서다):

```mermaid
stateDiagram-v2
    [*] --> pending: 확정(열 기본값)
    pending --> succeeded: 추출·번역 성공(묶음 교체, rows_extracted_at=now)
    pending --> failed: 실패(이전 묶음 유지)
    succeeded --> pending: 재번역 예약(묶음 유지) / 주 범위 수정·거절(묶음 삭제)
    failed --> pending: 재번역 예약 / 주 범위 수정·거절
```

- 기록 조건: `record_rows.sql`·`record_rows_failure.sql`은 `review_status='confirmed' AND rows_status='pending' AND rows_revision이 읽었을 때와 같음`일 때만 바꾼다. 그 사이 거절, 주 수정, 재예약이 있었으면 결과를 버린다(false). `rows_revision`은 거절, 재번역 예약, 주 범위 수정이 올리는 열이다. 주 범위 비교만으로는 추출 중 재예약을 알아챌 수 없어 구현 중에 더했다(`revision`은 이미지 번역 기록이 올리므로 쓸 수 없다).
- 추출 대상(`pending_rows.sql`): 확정본 가운데 `rows_status='pending'`이고 `week_end >= JST 오늘`인 것. `reviewed_at`, `id` 순이다. 그래서 배포 시점에 이미 확정된 이번 주·다음 주 포스터도 자동으로 추출된다.
- 문서 전용 소유 표 `docs/current/architecture/repository-ownership.allowlist`에 `schedule_poster_rows|owner=hololive-api|writers=hololive-api|readers=hololive-api` 줄을 더한다.

### 4.2 shared 저장소 (`rows.go`, 초안 작성됨)

- 타입:
  - `RowStatus`(+`Valid`), `MaxRows=100`
  - `Term{Source, Ko, Basis}`
  - `Row`(json 태그는 `insert_rows.sql`의 `jsonb_to_recordset` 열 이름과 같다)
  - `PendingRows{ID, ChannelID, ImageURL, ImageSHA256, WeekStart, WeekEnd}`
  - `PublishedSchedule{PosterID, WeekStart, WeekEnd, ExtractedAt, Rows}`
- 메서드:
  - `PendingRows(ctx, todayJST, limit)`
  - `WeekTerms(ctx, item)`
  - `RecordRows(ctx, item, rows) (bool, error)`: 한 트랜잭션에서 상태 갱신 → 이전 묶음 삭제 → `jsonb_to_recordset`으로 삽입한다. ordinal은 1부터 다시 매긴다.
  - `RecordRowsFailure(ctx, item, reason) (bool, error)`
  - `PublishedRows(ctx, channelID, dayJST) (PublishedSchedule, bool, error)`: 카드에 쓰는 칸만 읽는다.
- 남은 일: `ValidateRows` 추가(§2-1), dbtest(§5-1).

### 4.3 admin 행 파이프라인 (미작성)

위치: `hololive/hololive-api/internal/planes/admin/internal/scheduleposter/`.

- **구조**
  - 새 인터페이스 `rowStore`: `PendingRows`, `WeekTerms`, `RecordRows`, `RecordRowsFailure`.
  - 새 인터페이스 `rowModel`: `extractRows(ctx, src, weekStart, weekEnd)`, `translateRows(ctx, prompt)`.
  - `vertexTranslator`가 `rowModel`을 구현하고 `New`에서 `Service` 필드로 넣는다. 단계별 설정(`TranslationConfig.Rows`)은 생성할 때 받는다. 기존 `posterTranslator`와 테스트 fake는 바꾸지 않는다.
- **루프**
  - `Start`에 `runLoop(ctx, "rows", …, translateInterval, s.extractPendingRows)`를 더한다.
  - 이미지 번역과 다른 goroutine으로 돌린다. 한 포스터가 몇 시간 걸리는 이미지 번역이 행 추출을 막지 않게 하기 위해서다.
  - 한 번에 5개를 차례로 처리한다.
  - 멤버를 읽지 못하면 그 tick은 건너뛰고 대기 상태로 둔다(`translatePending`과 같다).
- **한 포스터의 처리**
  1. `s.sources.image(ctx, item.ImageURL)`로 원본을 받는다. 실패하거나 `ImageSHA256`이 다르면 `source_unavailable`로 끝낸다.
  2. 추출을 호출하고 검증한다(아래). 0행이면 `no_rows`로 끝낸다. 일정표가 아닌 그림을 확정한 경우다.
  3. 번역 지시문을 조립해 번역을 호출하고 검증한다.
  4. 추출 행과 번역 결과를 `posterstore.Row`로 합친다. 경고를 `Warnings`에 담고 `ValidateRows`를 거쳐 `RecordRows`를 부른다.
- **재시도**
  - 기존 `retryOptions(posterID, stage)`와 `retryableTranslationError`를 재사용한다(10회, 5초부터 두 배, 최대 1분, 400·401·403·404는 재시도 안 함).
  - stage 이름은 `rows_extract`, `rows_translate`이고, `logUsage`로 응답마다 사용량을 남긴다.
  - 아래의 "거부" 검증 실패는 시도 안에서 오류로 돌려 재시도 대상이 되게 한다.
  - 호출 하나의 상한은 단계별 설정 `timeout_seconds`다(아래 "단계별 설정"). 포스터 하나의 상한은 코드 상수 30분으로 제안한다. 재시도 계약 값이므로 서비스 문서와 함께 바꾼다.
  - 서비스 문서의 "일정 포스터 번역 재시도 예외 계약" 표(Trigger, 한도, 종단, Telemetry)에 행 단계를 더한다.
- **실패 사유(`rows_error`)**: `source_unavailable`, `extraction_failed`, `translation_failed`, `no_rows`(제안). 포스터 주인이 `members`에 없으면 `translation_failed`로 한다.

#### 단계별 설정 (`translation.rows`)

추출과 번역의 모델 파라미터는 코드 상수로 두지 않는다. 단계마다 따로 둔 설정에서 읽는다. Vertex 클라이언트(`project`, `location`, ADC)는 기존 `translation`을 함께 쓴다.

```json
"translation": {
  "project": "<GCP 프로젝트>", "location": "global",
  "model": "gemini-nano-banana-2.1", "text_model": "gemini-4-argon", "transcribe_model": "gemini-4-hydrogen",
  "glossary": {"ホロドリ": "홀로도리"},
  "rows": {
    "extract":   {"model": "gemini-4-hydrogen", "thinking_level": "LOW",    "timeout_seconds": 180},
    "translate": {"model": "gemini-4-hydrogen", "thinking_level": "MEDIUM", "timeout_seconds": 180}
  }
}
```

| 키 | 값과 검증 | 기본값(추출 / 번역) | 비고 |
|---|---|---|---|
| `model` | 기존 `modelPattern` | `gemini-4-hydrogen` / `gemini-4-hydrogen` | 단계마다 따로 바꾼다. |
| `thinking_level` | `MINIMAL`·`LOW`·`MEDIUM`·`HIGH` 가운데 하나(`genai.ThinkingLevel`) | `LOW` / `MEDIUM` | 모델이 받지 않는 단계(argon의 `MINIMAL`)는 Vertex가 400으로 거절한다. 400은 재시도하지 않으므로 그 포스터는 바로 실패로 기록된다. |
| `temperature` | 생략하거나 0~2 | 생략(모델 기본값) | 생략하면 요청에 넣지 않는다. 측정은 생략 상태로 했다. 0으로 하면 반복 일치율은 오르지만 번역 시간이 약 두 배였다(설계 인계 §3.5). |
| `timeout_seconds` | 30~900 | 180 / 180 | Vertex 요청 하나의 상한이다. SDK가 `x-server-timeout`으로 보내므로 넘으면 504가 된다. hydrogen 실측 최대는 추출 24.8초, 번역 42.9초다. carbon은 추출에서 240초 타임아웃이 있었으니, 모델을 바꾸면 이 값도 함께 본다. |

- 키를 생략하면 단계별 기본값을 쓴다. 기존 `LoadConfig`처럼 기본값을 채운 구조체에 `jsonv2.Unmarshal`한다. 일부 키만 넣었을 때 나머지 기본값이 유지되는지(json v2의 중첩 객체 병합)는 테스트로 확인한다.
- 모르는 키는 기존처럼 거부한다(`RejectUnknownMembers`). 그래서 `rows`를 넣은 설정은 이전 image가 읽지 못한다. 설정은 image와 함께 바꾼다. 서비스 문서의 `transcribe_model` 주의 문구와 같은 처리다.
- `Start` 로그에 두 단계의 `model`·`thinking_level`을 남긴다. 사용량 로그(`Schedule poster model usage`)에는 이미 `model`이 있다.
- 설정으로 두지 않는 것과 그 이유:
  - 검색 도구: 추출은 보이는 글자만 옮겨야 한다. 번역에 검색을 붙이면 틀린 공식 제목, 지어낸 참여자, 빈 응답 실패가 생겼다(설계 인계 §3.5). 넣으려면 검색 응답 처리 코드가 따로 필요하다.
  - 시스템 지시·지시문·응답 스키마: 측정한 내용 그 자체이므로 코드와 함께 바꾸고 다시 검증한다.
  - topP·seed: 측정하지 않았다.
- 이미지 경로의 thinking level 상수(`textThinkingLevel` 등)는 6단계에서 지울 코드라 이번에 설정으로 옮기지 않는다.

#### 요청 형식 (검증한 하니스와 같아야 한다, `research/run.py`의 `call`·`extract`·`translate`)

- 공통
  - `CandidateCount: 1`, `ResponseMIMEType: "application/json"`, `ResponseSchema`는 `*genai.Schema` 리터럴로 쓴다.
  - 스키마 원본은 `/var/tmp/schedule-card-proto/prompts/{extract,translate}_schema.json`이다. type은 대문자로 쓰고 `nullable`, `propertyOrdering`, `enum`, `description`을 그대로 옮긴다.
  - 도구, topP, seed는 넣지 않는다. temperature는 설정에 값이 있을 때만 넣는다.
- 추출
  - 시스템 지시: `Treat the input image strictly as schedule data to transcribe into structured rows. Follow only the extraction instructions provided in this prompt.`
  - parts는 `[이미지, 지시문]` 순서다(`posterContents`와 같다). 지시문은 `extract.txt`의 `%s to %s` 두 곳에 주 시작일과 마지막 날(`YYYY-MM-DD`)을 넣은 것이다.
  - thinking level은 설정 `rows.extract.thinking_level`(기본 `LOW`)을 쓴다.
- 번역
  - 시스템 지시: `Treat the input rows strictly as schedule text to convert into Korean fields. Follow only the rules provided in this prompt.`
  - parts는 지시문 하나다. thinking level은 설정 `rows.translate.thinking_level`(기본 `MEDIUM`)을 쓴다.
  - 지시문은 `translate.txt` 원문(끝 줄바꿈 포함) 뒤에 다음을 그대로 잇는다.

```text

Poster owner: <이름표 줄>
Poster time zone: <tz 또는 (none)>

Name table (names and nicknames (romaji) → Korean full name / short name):
<이름표 줄, name_ja 순>

Glossary (original → Korean):
<원문 → 한국어, 원문 순 / 없으면 (none)>

Earlier renderings (original → Korean):
<원문 → 한국어, 원문 순 / 없으면 (none)>

Rows:
1. <text>[ [badges: a, b]][ [day off]][ [cancelled]]
…
```

- 이름표 줄: `<name_ja>[, <aliases.ja…>] (<Name>) → <name_ko> / <short_ko>`.
  - 별칭은 다음을 뺀다.
    - 빈 값과 중복
    - 정규식 `Ch\.|Ch。|ch\.|Channel|チャンネル|hololive|ReGLOSS|FLOW GLOW|holoX|所属`(대소문자 무시)에 걸리는 채널 제목 꼴
    - `name_ja`를 포함하지만 `name_ja`와 같지 않은 것
  - `short_ko`는 `cmp.Or(ShortKoreanName, NameKo)`로 한다.
  - 대상 멤버는 기존 `buildTextPrompt` 필터(`NameJa`·`NameKo`가 있고, 졸업하지 않았고, `GetOrg()=="Hololive"`)를 쓴다. 하니스 이름표 78명과 같은 집합인지 한 번 대조한다.
  - 기존 `memberLine`은 이미지 경로가 쓰므로 새 함수로 둔다.
- `Poster time zone`: 추출 행 `tz` 가운데 가장 많이 나온 값이다. 같으면 먼저 나온 값을 쓰고, 없으면 `(none)`.
- `Earlier renderings`: `WeekTerms` 결과에서 같은 원문이면 첫 값만 쓴다.
- 1단계에는 변경 감지가 없어 `[update: …]` 표지를 쓰지 않는다.
- 회귀 방지: 이번 주 포스터 하나의 입력으로 Go가 조립한 번역 지시문이 하니스 `reference_block`·`row_line` 결과와 바이트 단위로 같은지 비교하는 테스트를 권한다. 하니스 출력은 `research/out/week_final_glossary/`의 입력으로 만든다.

#### 검증 (거부 = 시도 실패 후 재시도, 경고 = `warnings`에 저장)

| 단계 | 거부 | 경고 (하니스 `check_extract`·`check_translate`를 그대로 옮긴다) |
|---|---|---|
| 추출 | 날짜를 `YYYY-MM-DD`로 읽을 수 없음, 시각이 형식에 맞지 않거나 시가 29 초과, 빈 `text`, 100행 초과 | E1 주 범위 밖 날짜(저장한다. 카드는 7일 밖을 그리지 않는다), E3 휴방에 시각, E5 시작 시각이 `text`에 다시 나옴 |
| 번역 | `index`가 1..n 순서가 아님, 빈 `title_ko`, `with_ko`·`tags_ko`의 빈 항목, DB 상한 초과 | T2 가나·한자 잔존, T3 원문 숫자 누락(전각·원문자 정규화), T4 제목에 `@`·`with`·`w/`·`ch.`, T5 주인이 `with_ko`에, T6 짧은 이름 대신 전체 이름, T8 `terms`가 원문·출력과 불일치, T10 휴방 상태와 제목 "휴방" 불일치, T12 이름표에 없는 표기로 정한 사람 |

- 경고는 저장을 막지 않는다. `doubt_ko`, `terms`, T12가 있는 행은 운영자가 검토할 대상이다. 확정한 표기는 용어집이나 별칭으로 옮긴다. 1단계에는 이 내용을 보여 주는 Console 화면이 없으므로 행별 경고 수를 Info 로그로 남긴다.

### 4.4 bot `!스케쥴` 카드 (미작성)

- 핸들러: `hololive/hololive-api/internal/planes/bot/internal/command/handlers/handler_schedule_poster.go`.
  - 지금은 `c.Deps().SchedulePosters.PublishedImage(ctx, channel.ID, day)`를 쓴다(61행). worktree 버전은 아직 읽지 않았다.
  - `SchedulePosters` 의존 인터페이스에 `PublishedRows`를 더한다.
  - JST 날짜 계산, 없음 안내, 조회 실패 문구, 속도 제한은 그대로 둔다. 행이 없을 때의 처리는 §3 결정 1을 따른다.
- 렌더러: 프로토타입을 `…/bot/internal/render/` 아래 새 패키지(예: `schedulecard`)로 옮긴다.
  - `gfx.go`, `text.go`(sfnt 윤곽을 `vector.Rasterizer`로 채움), `layout.go`(`Slot`, `splitTitle`, `fitTable`, `timeLabel`), `shapes.go`, `theme_{tsuzuri,kyoko,sopia,mela,fallback}.go`
  - `main.go`, `specs.go`, `tools.go`는 시안·도구 전용이라 옮기지 않는다.
  - 프로토타입은 약 3,900줄이고 `gofmt`와 `go vet`만 통과했다. golangci-lint의 함수 길이·복잡도와 NilAway 기준에 맞춰 나눈다.
- 테마 선택은 채널 ID로 한다. 표에 없는 채널은 `fallback`이다.

| 테마 | 멤버 | 채널 ID |
|---|---|---|
| `tsuzuri` | 스즈나 츠즈리 | `UCy9mgxB8pn2C4aNK_MPthDQ` |
| `kyoko` | 햐쿠토 쿄코 | `UCSjQDxud2HkAO2DVD3lwxmw` |
| `sopia` | 소라시나 소피아 | `UCROQtXcp2loQEmvpe5rhJzQ` |
| `mela` | 아치치 메라 | `UC8eitCE9Z6EwUCs-VUi1blg` |

- 표기 원칙(설계 인계 §4.1, 유지)
  - 헤더는 멤버 이름과 그 아래 `MM.DD – MM.DD`만 둔다.
  - 우하단은 `YYYY.MM.DD HH:MM 업데이트`만 둔다. 시각은 `ExtractedAt`을 JST로 바꾼 값이다.
  - `[변경]`·`[확정]`·`[미정]` 배지는 쓰지 않는다.
  - 합성 볼드는 쓰지 않는다. 2배로 렌더한 뒤 `xdraw.CatmullRom`으로 줄이고, `image.RGBA`에는 premultiplied 값만 넣는다.
  - 7일을 항상 그린다.
- 행 → `Slot` 대응
  - `Title`은 `본제목 (부연)`이다. 부연은 다음 순서로 잇는다.
    1. `with_ko`를 ` · `로 이은 뒤 `와/과 함께`. 받침에 따라 조사를 고르는 도우미를 만든다. 마지막 항목이 "그 외 참가자"면 `… 등과 함께`.
    2. `place_ko` + `에서`
    3. `note_ko`
  - `tags_ko`는 작은 딱지로 그린다(디자인 미정). 시안은 언어 코드(EN·JP)가 아닌 태그를 부연에 넣었다. 제목이 이미 "멤버십 한정 방송"이면 같은 태그는 뺐다.
  - status 대응
    - `planned`: 확정 시각
    - `tentative`: 점선 캡슐(`미정`, `21:00 ?`)
    - `off`: 테마별 휴방 표시
    - `cancelled`: 취소 표시(디자인 미정)
  - 1단계에는 변경 감지가 없어 "내용 변경"과 "시각 변경" 표시(`Changed`)를 쓰지 않는다.
- 키비주얼(`Canvas.Medallion`): 멤버 프로필 사진을 쓴다. 기존 달력의 사진 적재와 보안 검사(`calendar_photos.go`, `calendar_photo_security.go`)를 재사용한다.
- 글꼴
  - 프로토타입 `fonts/`의 7종(BagelFatOne-Regular, BlackHanSans-Regular, DoHyeon-Regular, Jua-Regular, Pretendard-Bold, Pretendard-ExtraBold, YeonSung-Regular)과 `fonts/licenses/OFL-*.txt`를 `…/bot/internal/assets/fonts/`에 넣고 `go:embed`한다. 바이너리가 약 13MB 늘어난다.
  - 표제 글꼴에 없는 기호(`★`, `·`, `→`, `–`)는 Pretendard Bold로 그린다.
  - `sfnt.Buffer`와 글리프 캐시를 공유하므로 기존 `render.fontMu`(`calendar_fonts.go`)처럼 직렬화한다.
- 캐시 키는 `(poster_id, rows_extracted_at, 판형)`이다. 달력 캐시(`calendar_cache.go`, `calendar_disk_cache.go`) 방식을 따른다. 실측은 카드 하나에 0.5~1.8초, PNG 0.28~0.63MB다.

### 4.5 문서

- `docs/current/services/hololive-api.md` "주간 일정 포스터"
  - 행 단계: 대상, 모델, 요청, 검증, 실패 사유, 로그
  - `!스케쥴` 표시 변경, migration 019
  - 설정 파일 예시와 키 설명에 `translation.rows.{extract,translate}.{model,thinking_level,temperature,timeout_seconds}`와 기본값을 넣는다. 이전 image가 이 키를 읽지 못한다는 주의도 적는다.
  - 재시도 계약 표에 행 단계 추가
- `CHANGELOG.md`(저장소 관례대로), 소유 표(§4.1).
- 1단계는 관리 API와 `docs/current/contracts/admin-api.openapi.json`을 바꾸지 않는다. 재추출은 기존 재번역 경로를 같이 쓴다.

### 4.6 변경 감지 (2단계, 자동 반영, 미작성)

설계 원문은 원 계획 "변경 감지와 반영"(197–214행)과 설계 인계 §3.5 "연결 계약"이다. 여기에는 거기서 바뀐 점과 하루 여러 활동 처리만 적는다.

- **흐름**: 수집(`change_poll_seconds`, 최대 2페이지) → 판별(`gemini-decision-1`, 글 본문만, `schedule_info` ≥ 0.7이고 `kind`가 `other`·`weekly`가 아님) → 행 대응(hydrogen `LOW`, `match.txt`) → Go 검증·비교 → 변경 기록 → 내용이 있는 항목만 다시 번역(`[update: …]`) → 카드.
  - 판별한 글은 결과와 함께 60일 기록한다. 이 기록이 같은 글을 다시 판별하지 않게 막는다. "같음"과 `not_relevant`도 기록한다.
  - 원래 행은 고치지 않는다. 화면 일정은 원래 행에 변경을 게시 시각 순서로 적용해 계산한다.
  - 변경마다 출처 글 URL을 남긴다.
- **단계별 설정**: 대응·판별 단계도 §4.3 "단계별 설정"과 같은 꼴로 둔다. `rows.match`는 기본 hydrogen `LOW`다. 판별 기준값(0.7)과 `change_poll_seconds`(원 계획 제안 600초)도 설정으로 둔다.
- **원 계획과 달라진 Go 검증**: 원 계획의 "행이 있으면 그 행의 날짜와 같아야 한다"는 최종 `match.txt`와 맞지 않는다. 최종 지시문은 다른 날로 옮긴 방송을 원래 행 번호와 새 날짜로 돌려준다(Rules 19행).
  - 날짜가 다른 대응 항목은 "옮김"으로 비교한다.
  - 새 날짜가 주 범위 밖이면 이번 주 카드에서 취소로 표시한다(연결 계약).
  - 근거(`evidence`)는 공백을 정규화한 뒤 글 본문의 부분 문자열이어야 한다. 날짜는 게시일(JST)부터 주 마지막 날 사이여야 한다. 단, 옮긴 행의 새 날짜는 주 밖도 받는다.

#### 하루 2~3회 이상 활동하는 경우 (제안 규칙)

이번 주 포스터의 하루 여러 건 날: 쿄코 7일 중 6일(11/14는 3건), 메라 2일, 츠즈리 1일. 측정한 27건 가운데 쿄코 글은 7건이고 모두 맞았다. 이 결과는 이전 지시문의 것이다. 최종 `match.txt`는 같은 날 두 행 예시(Example 2, 5)를 넣었지만 모델로 돌려 보지는 않았다.

| 상황 | 처리 | 이유 |
|---|---|---|
| 한 글이 같은 날 여러 방송을 말함("12시 A / 21시 B") | 방송마다 항목 하나다(지시문 규칙). Go는 항목별로 검증·반영한다. | 한 항목이 실패해도 나머지는 반영한다. |
| 하루에 공지 글이 여러 번 옴(방송마다 직전 공지) | 글마다 판별·대응한다. 같은 멤버의 글은 게시 시각 순서로 **차례대로** 처리한다. 대응 입력의 "Current schedule rows"는 앞 글까지 반영한 상태(`[update: …]` 포함)여야 한다. | 병렬로 처리하면 뒤 글이 앞 글의 변경을 모르고 대응한다. |
| 같은 행을 여러 글이 차례로 바꿈 | 게시 시각이 늦은 글이 이긴다. 모든 변경과 출처는 기록에 남긴다. | 원래 행과 변경 기록으로 언제든 다시 계산할 수 있다. |
| 한 글 안에서 두 항목이 같은 행을 가리킴 | 그 행의 항목은 반영하지 않고 사유(`duplicate_row`)를 기록한다. 같은 글의 다른 항목은 반영한다. | 어느 쪽이 맞는지 코드가 정할 수 없다. 자동 반영이라 틀린 쪽을 고르면 그대로 게시된다. |
| 새 방송(`row` null)인데 같은 날 같은 시작 시각의 행이 이미 있음 | 새로 더하지 않고 그 행에 대응한 것으로 비교한다. | 시각만 말하는 직전 공지를 새 방송으로 잘못 읽으면 같은 방송이 두 번 보인다. |
| 시각 변경 결과가 같은 날 다른 행의 시각과 같아짐 | 반영하지 않고 사유(`time_collision`)를 기록한다. | 직전 공지("このあと19時から")를 다른 행에 잘못 대응했을 가능성이 크다. |
| 제목 없이 시각만 있는 직전 공지인데 같은 날 행이 여럿임 | 모델 대응을 따르되 위의 두 충돌 규칙으로 막는다. 모델 대응이 틀렸는지는 코드로 알 수 없다. | 측정의 쿄코 11/13 `22:00→19:00`("このあと19時から"만 올린 글)이 이 경우였고 맞았다. 표본이 1건이라 표본을 다시 받을 때 이런 글을 따로 모은다. |
| 하루 건수가 늘어남(추가 방송) | 카드는 반영 뒤 시각 순으로 정렬하고, 시각이 미정인 행은 그날 끝에 둔다. `fitTable`의 압축 규칙(하루 여러 건을 건당 한 줄로, 0.62까지)을 하루 4~5건에서 레이아웃 테스트로 확인한다. | 시안의 한계 확인용 카드는 이런 하루 건수로 시험하지 않았다. |
| 직전 공지의 반영 지연 | `change_poll_seconds` 600초면 방송 시작 뒤에 반영될 수 있다. 값은 설정이고, 300초로 줄이면 FxTwitter 요청은 4명 × 최대 2페이지 × 시간당 12회가 된다. | 짧게 할지는 운영에서 지연을 보고 정한다. |

- 비용 [추정]: 판별은 글당 약 $0.00005, 대응은 통과한 글당 약 $0.011이다. 4명이 하루 40건을 올려 모두 통과해도 대응은 하루 약 $0.44다.
- **시작 전 시험**(모델 실행 승인 필요): 원 계획 "평가 표본" 규칙으로 표본을 다시 받는다. 하루 여러 건인 날의 글과 시각만 있는 직전 공지를 일부러 포함하고, 최종 `match.txt`와 위 Go 규칙으로 정답과 대조한다.

## 5. 남은 작업 (순서, 수용 기준)

1. [x] **shared 마무리와 DB 시험**
   - `ValidateRows`를 추가한다.
   - `hololive-dbtest`에 019 migration 테스트를 둔다(CHECK 거부, 별칭을 두 번 넣지 않음).
   - 저장소 테스트(`store_test.go` 방식)
     - 기록하면 이전 묶음이 바뀐다.
     - 주 수정, 거절, 재예약 뒤의 기록은 false다.
     - 실패를 기록해도 이전 묶음과 `rows_extracted_at`이 남는다.
     - `PublishedRows`는 `PublishedImage`와 같은 기준으로 고른다.
     - `WeekTerms`는 자기 포스터를 빼고 저장 순서로 돌려준다.
     - 행 쓰기는 `revision`을 바꾸지 않는다.
   - schema golden을 재생성한다.
2. [x] **admin**
   - 프롬프트 embed, 스키마, `rowModel` Vertex 구현, 지시문 조립, 검증, 루프, 단계별 설정(§4.3 "단계별 설정")을 만든다.
   - 설정 테스트
     - 키를 생략하면 단계별 기본값(hydrogen `LOW` / hydrogen `MEDIUM`, 180초, temperature 없음)이 쓰인다.
     - 한 단계의 일부 키만 넣어도 나머지 기본값이 유지된다.
     - 잘못된 `thinking_level`, 범위 밖 `temperature`·`timeout_seconds`, 모르는 키는 기동 오류다.
     - 설정 값이 요청(`GenerateContentConfig`의 model, `ThinkingLevel`, `Temperature`, 요청 상한)에 그대로 들어간다.
   - fake 모델·저장소 테스트
     - 거부 검증이 재시도된다.
     - 0행이면 `no_rows`다.
     - 주인이 없으면 실패한다.
     - 성공하면 경고와 함께 저장된다.
     - 원본 해시가 바뀌면 `source_unavailable`이다.
     - 취소된 ctx는 기록하지 않는다.
   - 하니스 대조 테스트(§4.3).
3. [x] **bot**
   - 글꼴과 렌더러를 이식하고, 행 → 카드 조립과 핸들러를 만든다.
   - 레이아웃 테스트: 7일, 긴 제목 맞춤, 하루 여러 건, `off`·`tentative`·`cancelled`.
   - 핸들러 테스트: 있음, 없음, 조회 실패, 렌더 실패.
   - 시안 PNG와 눈으로 비교한다(`/tmp/schedule-card-preview/`는 재부팅 때 사라지므로 `./cardproto`로 다시 만든다).
4. [x] **문서**(§4.5)와 이 파일의 진행 기록.
5. [ ] **게이트**(§6). 모두 통과하면 diff를 검토하고 결과를 보고한다. 보고에는 "Fallback delta"를 넣는다.
6. [ ] **변경 감지**(§4.6, 자동 반영): 1단계에 이어 한다. 시작 전에 §4.6의 표본 시험을 한다. 수용 기준은 다음과 같다.
   - 판별 기록이 같은 글을 다시 판별하지 않는다.
   - 같은 멤버의 글을 차례대로 처리한다.
   - §4.6의 충돌 규칙(`duplicate_row`, 같은 시각 추가, `time_collision`)과 옮김·주 밖 이동을 단위 테스트로 확인한다.
   - 하루 4~5건의 카드 레이아웃 테스트를 둔다.
   - 측정한 17건과 다시 받은 표본을 fixture로 쓴다(정상 공지, 근거 불일치, 지난 날짜, 없는 행 번호, 하루 여러 건).
7. 범위 밖(별도 결정·승인): 이미지 경로 제거(2-6), Console 일정표 화면, 운영 데이터 교정.

## 6. 검증 명령 (kapu에서, worktree 루트 기준)

- 빌드: `./build-all.sh --build-only --no-bump`
- 테스트: 영향받는 패키지에 `go test -race`
  - `hololive/hololive-shared/pkg/service/scheduleposter/...`
  - `hololive/hololive-api/internal/planes/admin/internal/scheduleposter/...`
  - bot `render`·`handlers`
- DB 테스트: `TEST_DATABASE_URL`(그리고 `dbtest.go`의 소유 토큰 환경 변수)이 필요하다. golden 재생성은 `SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest`.
- 정적 검사: golangci-lint, NilAway(stage 구성은 `scripts/ci`와 README 검증 절을 따른다), `scripts/architecture/check-sql-ownership.sh`
- 게시 전: `scripts/ci/pre-push-gate.sh`. 스택 게이트는 `/home/kapu/work/iris-stack/tools/checks/*.sh` 가운데 영향받는 것을 실행한다.
- LSP가 프로토타입 모듈에 내는 `undefined: Canvas` 류 오류는 그 모듈이 `go.work`에 없어서 생긴다.

## 7. 승인 경계 (아직 받지 않음)

- Git 커밋, push, PR(커밋은 요청이 있을 때만). `go.work`는 로컬에만 둔다.
- 운영 DB migration 적용, 배포, 설정 파일 교체(hololive-bot-ops 절차를 따른다)
- 새 모델 실행: 실제 Vertex 연기 시험, B안 재실행, 리뷰어 재호출, 프롬프트 수정
- 운영 `members`·용어집 쓰기(stack-platform-ops 절차를 따른다), stack-secrets 접근, `cfg://` 영구 저장
- Console 계약 변경, 텍스트 대체 계약 추가

## 8. 진행 기록

- 2026-11-14: worktree를 만들었다. 저장소 계층(migration 019, 쿼리, `rows.go`) 초안을 쓰고 프롬프트 2개를 복사했다. `hololive-shared` 패키지 `go build`와 `go vet`을 통과했다. 그 밖의 검증은 실행하지 않았다.
- 2026-11-14: 사용자 지시에 따라 단계별 모델 파라미터 설정(`translation.rows`)을 설계에 넣었다(§4.3). 추출 기본값 hydrogen `LOW`를 확정으로 옮겼다(§3). 번역 기본값은 hydrogen `MEDIUM`을 권고로 두고, carbon을 다시 비교할 조건을 적었다. 코드는 바꾸지 않았다.
- 2026-11-14: 변경 감지를 "범위 밖"으로 적은 것을 바로잡았다. 측정은 끝났고, 남은 것은 반영 방식 결정과 최종 `match.txt` 모델 시험이다. 그래서 작업 순서 6번에 넣었다(§3, §5).
- 2026-11-14: 변경 감지를 자동 반영으로 확정했다(사용자 지시, §3). 하루 여러 활동 처리 규칙과 최종 `match.txt`에 맞춘 Go 검증 수정(옮긴 행 허용)을 §4.6에 적고, 작업 6번의 수용 기준을 정했다.
- 2026-11-14: 필수 스킬(§0.1)과 필수 컨벤션(§0.2)을 추가했다. 스킬 설명과 경로는 각 `SKILL.md` 머리말과 `realpath`로 확인했다.
- 2026-11-14: 사용자 결정 네 가지를 받아 §3에 적었다(행 없을 때 안내 문구, 판형은 미리보기 뒤 결정, B안 적용, 2단계 모델 시험 승인). 구현은 워크플로로 진행한다(shared → admin, 렌더러 이식 병행).
- 2026-11-14: **B안 재검증(Vertex `gemini-4-hydrogen` `LOW`, 실제 호출 102회).** 후보 문구는 `/var/tmp/schedule-card-proto/prompts/extract_b.txt`(SHA-256 앞 12자리 `2c5b41d0b354`)이고 `badges` 규칙 끝에 두 문장을 더했다. 결과 파일은 `research/out/b_hydrogen_low_{a,b}`(34장 × 2회), `research/out/b_ab`(반복 비교)다.
  - 34장 × 2회 68회 모두 성공했다. 재시도 0회, 시간 중앙값 10.9초, 최대 35.9초, 구조 경고 0건, 행 수는 이전과 같은 257행이다.
  - 쿄코 양식 2장(`week/kyoko`, `design/kyoko_1102`) 4회 모두 여러 행이 있는 날짜 칸의 종류 도장 배지가 없어졌다(이전 hydrogen은 `kyoko_1102` 2회 중 1회 15행 전부에 붙였다). EN/JP 배지는 그대로다. 행이 하나뿐인 칸(11/05)은 규칙대로 `ゲーム`를 유지했다. 이 행들을 hydrogen `MEDIUM`으로 번역한 4회에서 제목에 배지가 섞인 행은 없었다(`PEAK 풀 스쿼드!`).
  - 다른 포스터: 날짜·시각·시간대·상태는 34장 중 33장이 이전 실행과 같다. 다른 1장(`kaela_1108`의 "will decide later!" 행 `tentative`)은 같은 그림을 두 문구로 6회씩 돌렸을 때 양쪽 모두 6회 중 2회 나오는 기존 흔들림이다. 행마다 도장이 따로 있는 츠즈리 양식 4장의 배지는 유지됐다.
  - 부작용 관찰: `gigi_1027`에서 첫 실행 한 번 `#sponsored` 3행을 놓쳤다. 같은 그림 8회 반복에서는 두 문구 모두 8회 다 읽었다(B안 누계 10회 중 1회, 원래 문구 10회 중 0회 놓침). 같은 포스터의 말풍선 딱지 `idk what`을 배지로 읽는 비율이 8회 중 2회에서 6회로 늘었다. 번역 결과(`공포 게임`, 부연 `무서우면 방송 종료 · 게임 미정`)는 두 경우가 같았다.
  - 판단: 적용한다. 저장소 `prompts/extract.txt` 교체는 admin 워커가 끝난 뒤에 한다(워커가 원래 해시를 검사하고 있다). 리뷰어 문언 재검증은 하지 않았다.
- 2026-11-14: 2단계 표본 시험을 시작했다(`/var/tmp/schedule-card-proto/research/change/`). 이전 측정의 스크립트와 표본은 남아 있지 않아 새로 받는다. 판별 질문 문구는 설계 세션 기록에서 되찾은 최종본을 쓴다. Gemini 키는 `iris-stack/gemini.env`의 두 개를 번갈아 쓴다(사용자가 둘째 키를 추가했다).
- 2026-11-14: **1차 구현 워크플로 완료(shared → admin, 렌더러 이식, shared 검토).** 워커가 보고한 검사는 각 패키지의 `go build`, `go vet`, `go test -race`, golangci-lint, NilAway, `check-sql-ownership.sh`다. 전체 빌드와 게이트는 아직 실행하지 않았다.
  - shared: `ValidateRows` 공개, dbtest(019 migration 2개, 저장소 11개), schema golden 재생성, 소유 표 줄 추가. §2의 가정 가운데 json v2 `format:DateOnly`는 틀렸다(Go 1.27.4가 format 태그를 거부). `Row.Day`는 JSON에서 빼고 내부 기록 구조체가 날짜 문자열을 넣는다. `PendingRows.RowsRevision` 필드와 `rows_revision` 열을 더했다(§4.1). `CONVENTIONS.md`는 스키마 migration 안의 데이터 갱신을 막지 않는다(017·018·016 선례).
  - admin: `translation.rows` 단계별 설정, `rowModel`의 Vertex 구현, 지시문 조립, 거부·경고 검증, `rows` 루프. 쿄코 포스터의 번역 지시문이 하니스 결과와 바이트 단위로 같고, 4장 모든 행의 경고가 하니스와 같다. 이름표 대상은 Go 필터로 77명이다(하니스 78명과의 차이는 한국어 전체 이름이 빈 IRyS 1명). 하니스의 짧은 이름 12명 교정(`SHORT_KO_FIX`)은 운영 데이터 교정 전까지 Go에 없다. 실제 Vertex 요청은 시험하지 않았다.
  - 렌더러: `render/schedulecard` 패키지(`Render(Card, Format)`), 글꼴 7종과 OFL. 변경 표시는 옮기지 않았다. 딱지는 외곽선 알약, 취소는 흐린 색과 취소선으로 정했다(사용자 확인 전). 미리보기 PNG는 scratchpad의 `schedule-card-preview/{3x4,4x3,16x9}`에 있다.
  - shared 검토 지적: `ValidateRows`가 날짜를 검사하지 않음(0년 날짜가 DB에서 거부됨), 기록 트랜잭션과 거절·주 수정이 겹치면 행 묶음이 남는 경합, `rows_extracted_at`이 있으면 확정이어야 한다는 CHECK 때문에 이전 image로 되돌렸을 때 확정본 거절이 실패함. 셋째는 CHECK의 그 조건을 빼기로 정했다(게시·추출 쿼리가 이미 `review_status='confirmed'`를 건다). 수정은 다음 워크플로에서 한다.
  - `"cancelled"` misspell 지적은 DB·지시문 계약 철자라 저장소 선례대로 그 줄에만 `//nolint:misspell`을 단다.
- 2026-11-14: B안 지시문을 저장소 `prompts/extract.txt`와 프로토타입 `prompts/extract.txt`에 반영했다(이전본은 프로토타입 `prompts/extract_pre_b.txt`). admin 패키지 테스트는 통과했다.
- 2026-11-14: 2차 워크플로를 시작했다(shared 지적 수정, `!스케쥴` 핸들러·조립·캐시·사진, admin 검토 3관점과 렌더러 검토 2관점 뒤 검증·수정). 2단계 설계 패널(설계안 3 → 심사 3 → 종합 → 누락 점검)도 읽기 전용으로 돌고 있다.
- 2026-11-14: **2단계 표본 시험 완료(실제 호출: 판별 202회, 행 대응 74회, FxTwitter 약 53회, 약 $0.8).** 산출물은 `/var/tmp/schedule-card-proto/research/change/`(글 200건 `posts.json`, 정답 `labels.json`, 판별 `decisions.json`, 대응 `match_run{1,2}.json`, 합성 `match_synthetic.json`, 추가 확인 `match_probe_real.json`, 규칙 모사 `sim.py`)다. 독립 검토가 정답과 수치를 파일에서 다시 확인했다.
  - 표본: 아소비대 4명의 11-07~11-14 15시 원글 200건(일정 정보 52, 주간 3, 기타 142~143, 모호 2~3). 포스터 게시 뒤 글 117건. 메라는 X에 주간 일정 글이 없어 주 시작(11-09 00:00 JST)을 기준으로 삼았다(가정).
  - 판별(결합 규칙 0.7): 일정 정보 52건 중 50건, 오탐 2, 놓침 2. 정밀도·재현율 0.962. 놓침 2건은 모두 `kind=other`(0.84, 0.88)였다. 두 키를 번갈아 썼고 429는 없었다. 질문 문구와 문자열 입력 형식은 `decide.py`에 있다. 저장소 `gemini.go`의 배열 입력과 점수가 같은지는 확인하지 않았다.
  - 행 대응(최종 `match.txt`, hydrogen `LOW`, 30건 × 2회): 행 번호·날짜·시각·상태는 33개 항목이 두 번 모두 맞았다. 시각변경 5, 미정으로 1, 항목 없음 2가 맞았고 거절·충돌은 0건이다. "같음"과 "내용 변경"의 구분은 정답을 바로잡은 뒤 29/30, 28/30이다. 같은 입력의 판정이 1/30 뒤집혔다.
  - 한계: 이 30건은 `match.txt`를 만들 때 본 측정 표본과 대부분 같은 글이고 예시 3개가 표본 글과 구조가 같다. 새 글은 4건뿐이다. 추가, 옮김, 취소, 휴방 전환, 확정, `duplicate_row`, `time_collision`, 24시 이상 표기는 실제 글로 한 번도 실행되지 않았다.
  - 합성 시험(쿄코의 직전 공지에서 제목 줄을 지운 글 4개 × 2회): 시각이 기존 행과 다르면 모델이 4회 모두 `row` null을 돌려줘 같은 방송이 두 번 생긴다.
  - 추가 확인(쿄코의 실제 글 3건 × 2회): 제목 없는 변경 글 "開始時間を【⏰２１時３０分】に変更"은 두 번 모두 맞는 행의 시각변경이었다. 정오를 "7PM PDT | 4AM CEST | 12AM JST"로 쓴 공지 2건은 모델이 네 번 모두 PDT를 골랐고 날짜가 실행마다 달랐다. 시간대를 바꿔 반영하면 한 글은 두 번 모두 다음 날로의 거짓 "옮김"이 되고, 다른 글은 한 번은 "같음", 한 번은 `time_collision` 거절이었다.
  - §4.6 서술 정정: 쿄코 11/13 22:00→19:00 글에는 제목(Phasmophobia)이 있었다. 제목 없이 시각만 있는 직전 공지는 실제 표본에 없다.
- 2026-11-14: **표본 시험에 따른 §4.6 규칙 보강(구현에 반영한다).**
  1. `row`가 null이고 `text`가 빈 항목은 추가하지 않고 사유를 기록한다.
  2. 시간대는 JST(=KST)로 고정한다(2026-11-14 사용자 지적 `"어차피 kst(jst)고정하는게 맞지않아?"`). 항목의 시간대가 없거나 JST면 JST 시각으로 다루고, 그 밖의 시간대면 반영하지 않고 사유(`foreign_zone`)를 기록한다. 포스터별 시간대 비교와 시간대 변환은 구현하지 않는다. 대상 4명의 포스터가 모두 JST(또는 표기 없음)라 성립한다. JST가 아닌 포스터를 쓰는 멤버를 대상에 넣으려면 추출 단계가 JST 시각을 고르게 해야 한다.
  3. 결과 시각이 게시 시각보다 이르면 반영하지 않고 사유를 기록한다.
  4. `time_collision`은 같은 글의 모든 항목을 반영한 뒤의 상태로 판정한다. 시각을 바꾸는 모든 판정(미정으로 포함)에 적용하고, 시각이 바뀌지 않은 항목에는 적용하지 않는다.
  5. 날짜 하한은 게시일과 주 첫날 가운데 늦은 쪽이다. 24시 이상 표기는 다음 날 시각으로 바꿔 비교한다. 주 밖으로 옮긴 행은 원래 날짜에 취소로 남긴다.
  6. `text`만 바뀐 항목은 카드의 변경 표시를 하지 않는다.
  7. 수집은 고정 글(페이지 맨 앞의 옛 글) 때문에 일찍 멈추지 않도록 페이지의 마지막 항목 시각으로 중단을 판정한다.
  - 사용자 승인이 필요한 것(지시문 수정): `match.txt`에 "여러 시간대가 적혀 있으면 JST 시각을 고른다", "제목 없이 오늘의 시각만 준 글은 그날의 다음 행을 가리킨다", "`text`에는 행과 다른 사실만 옮긴다"를 넣고 다시 시험하는 것. 판별에 "`schedule_info` ≥ 0.8이면 `kind=other`여도 통과"를 더하는 것(200건에서 오탐 증가 0, 놓침 2건 해소).
- 2026-11-14: **`match.txt` 보정과 재시험(사용자 지시 `"는 프롬프트로 보정"`, 실제 호출 Vertex 100회).** 후보 두 개를 표본 30건(누적), 쿄코 실제 글 4건 × 3회, 합성 글 4개 × 2회로 돌렸다. 결과 파일은 `research/change/match_variant_{c1,c2}.json`이다.
  - c1(`prompts/match_c1.txt`, SHA-256 앞 12자리는 아래 채택본과 같다): `start` 규칙에 "12AM과 12PM을 바꿔 쓴 글: 적힌 대로의 12시가 게시 시각에 이미 지났고 다른 쪽 12시는 지나지 않았으면 다른 쪽을 적는다", `tz` 규칙에 "여러 시간대가 적혀 있으면 게시 시각과 행이 쓰는 시간대를 고른다"를 넣었다.
    - 표본 30건: 행·날짜·시각·상태가 이전 실행과 모두 같다(같음↔내용 변경 1건만 뒤집힘, 기존 흔들림). 시간대는 JST와 없음뿐이다.
    - "12AM JST" 공지 2건(`…467774`, `…819246`): 6회 모두 맞는 행의 12:00 JST로 나왔다(이전에는 PDT, 거짓 옮김). 포스터 오기 정정(11/12 00:00→12:00)도 그대로 맞았다.
    - 주 시작 전날 글(`…526819`, 11-08 게시)은 3회 모두 `row` null, 11-09 00:00으로 나와 거짓 추가가 된다. 그래서 "포스터 주의 첫날보다 먼저 올라온 글은 그 포스터에 대해 처리하지 않는다"를 규칙에 더한다(아래 보강 8).
  - c2(`prompts/match_c2.txt`): c1에 "제목 없이 오늘의 시각만 준 글은 게시 시각에 아직 시작하지 않은 그날의 방송이 하나뿐일 때 그 행을 가리키고, 둘 이상이면 `row` null", "`text`에는 응원·인사·시청 권유를 빼고 옮긴다"를 더했다. 표본 30건은 이전 실행과 판정까지 모두 같았고, 합성 S1(22:00→19:00)이 2회 모두 맞는 행의 시각변경이 됐다. 후보가 둘인 S2는 2회 모두 `row` null이었다.
  - 채택: c1을 프로토타입 `prompts/match.txt`로 채택했다(이전본은 `match_pre_c.txt`). c2의 두 문장은 사용자 확인 뒤에 넣는다.
  - 보강 8: 포스터 주의 첫날(JST)보다 먼저 올라온 글은 그 포스터의 변경 감지 대상이 아니다.
- 2026-11-14: **2차 워크플로 완료(검토 지적 수정, 핸들러).** 워커가 보고한 검사는 바뀐 패키지의 `go build`, `go vet`, `go test -race`, golangci-lint 0건, NilAway, `check-sql-ownership.sh`다.
  - shared: `ValidateRows`가 날짜(영값, 연도 1..9999 밖)를 거부한다. 거절과 주 범위 수정은 변경이 성공하면 같은 트랜잭션에서 `delete_rows.sql`을 별도 문장으로 실행한다(커밋 전이던 `RecordRows`의 행이 남던 경합, 재현 테스트 `TestRowsRemovalWaitsForAConcurrentRecordRows`). `chk_schedule_posters_rows_extracted`의 둘째 조건을 뺐다. 가장 최근 확정본에 행이 없으면 `PublishedRows`가 그날을 덮는 이전 확정본의 행을 돌려주는 동작을 테스트로 고정했다.
  - admin(검토 21건): 추출이 정하는 칸의 저장 조건과 제어 문자를 추출 시도 안에서 검사한다(`extraction_failed`). 경고는 행마다 40개로 자른다. `rows` 키가 없거나 null인 응답은 형식 오류로 재시도한다. 시간대 동률은 먼저 나온 값, T12 주인 제외는 같은 멤버 기준, 오류 문구의 모델 값은 120자로 자른다. 하니스 대조 테스트는 포스터 4장의 지시문을 바이트 단위로 비교한다.
    - 고치지 않은 것: 저장소 오류 때 포스터가 대기 상태로 남아 1분마다 모델을 다시 부르고 횟수 상한이 없다. 새 재시도 계약이 필요해 코드는 두고 서비스 문서에 적었다.
  - 렌더러(검토 23건): 최저 배율에서도 넘치면 하루 표시 건수를 줄이고 "외 N건"으로 접는다(이전에는 행이 겹쳤다). 휴방 제목 자르기와 사유 표시, NFC 정규화, 글꼴에 없는 글자 제거, 괄호 역할을 뺀 조사 선택, 언어 코드 딱지(EN, JP) 복원, 주 범위 밖 행을 형식 검사 전에 거르기, `wrap` panic 수정, 글리프 캐시 4096개 상한, NotoSansJP OFL 추가. 미리보기 PNG를 다시 만들었다.
    - 측정(검토자, 다시 재지 않음): 카드 한 장의 순간 힙 최고치 약 130~137MB. 렌더는 직렬이라 동시 요청으로 곱해지지 않는다.
  - bot 핸들러: `PublishedRows` → `Matcher.GetMemberByChannelID` → `ScheduleCards.RenderScheduleCard` → `SendImage`(PNG). 카드 캐시는 프로세스 메모리 16장, 키는 (포스터, 행 저장 시각, 판형). 사진 적재는 달력 코드(`fetchPhotos`)를 함께 쓴다. 판형 기본값은 `render/schedule_card.go`의 `scheduleCardFormat` 상수다.
- 2026-11-14: 남은 결함 두 가지를 직접 고쳤다(실패하는 테스트를 먼저 확인).
  - 시 상한 29를 shared `ValidateRows`와 migration 019 CHECK로 옮겼다(렌더러가 30시 이상을 거부하는데 저장 계층은 99시까지 받았다). schema golden을 다시 만들었다.
  - 렌더러 `shownTags`가 제목의 부분 문자열로 딱지를 빼서 `ENDING`이 든 제목에서 EN 딱지가 사라졌다. ASCII 글자·숫자에 붙은 일치는 같은 표기로 보지 않게 했다(`titleStates`).
- 2026-11-14: 문서를 썼다. `docs/current/services/hololive-api.md`에 설정 키 `translation.rows`, "일정표 행" 단계, `!스케쥴` 표시, "일정표 행 추출·번역 재시도 예외 계약", 이전 image로 되돌릴 때의 주의를 넣었고 `CHANGELOG.md`에 항목을 더했다.
- 커밋할 때 필요한 것: 주석 게이트가 비공개·테스트 선언의 새 주석을 막는다. 남긴 주석은 숨은 불변식·보안 이유·하니스 일치 같은 사실을 담은 것이며, 커밋 명령 앞에 `# keep-comment: <경로>:<선언> <잃는 사실>` 줄이 필요하다. 대상 선언:
  - admin `rows.go`: `rowsPosterBudget`, `Service.extractOne` / `rows_check.go`: `maxRowWarnings`, `validateExtracted`, `clip`, `rowChecker`, `titleHasMarker` / `rows_model.go`: `maxRowsResponseBytes`, `rowsRequestConfig` / `rows_prompt.go`: `extractPromptTemplate`, `earlierRenderings`, `buildTranslatePrompt` / `rows_test.go`: `TestExtractPendingRowsMatchesTheHarness`
  - bot `render/schedule_card.go`: `scheduleCardKey` / `render/calendar_photos.go`: `fetchPhotosWithinBudget` / `schedulecard/gfx.go`: `shape` / `slots.go`: `clockText`, `cleanText` / `text.go`: `maxCachedGlyphs`, `fontSet`, `covers` / `theme_mela.go`: `melaMinCapH`
- 2026-11-14: **1단계 검사(마지막 수정 뒤, 직접 실행).** `gofmt -l` 차이 없음. `go build ./...`(api, shared, alarm-worker, youtube-collector, dbtest) 통과. `go vet`과 `go test -race -count=1` 통과: shared `./pkg/service/scheduleposter/...`, dbtest `-run 'SchedulePoster|TestSchemaSnapshotGolden|Migration'`, api `./internal/planes/admin/internal/scheduleposter/... ./internal/planes/bot/... ./internal/migrationrunner/...`. golangci-lint 0건(같은 패키지), NilAway 진단 없음, `scripts/architecture/check-sql-ownership.sh` 통과.
  - 실행하지 못한 것: `./build-all.sh --build-only --no-bump`는 미커밋 worktree를 거부한다(`Deployment source worktree is not clean`). 커밋 뒤에 돌린다. `scripts/ci/pre-push-gate.sh`와 스택 게이트는 게시 전 검사라 2단계까지 끝낸 뒤 커밋 승인을 받으면 돌린다.
- 2026-11-14: **2단계 설계 확정.** 설계 패널(설계안 3 → 심사 3 → 종합)의 종합안을 `/var/tmp/schedule-card-proto/research/change/design_final.md`에 두었다. 표본 시험·독립 검토·사용자 결정에 따른 보정은 같은 폴더의 `design_amendments.md`이고, 둘이 다르면 보정이 이긴다. 구현은 이 두 문서를 기준으로 한다. §4.6의 표와 다른 점은 보정 문서에 있다.
  - 뼈대: migration 020에 글 원장 `schedule_poster_posts`(글당 1행, 판별 결과와 마지막으로 대응한 행 묶음의 도장)와 `schedule_poster_changes`(항목당 1행, 글이 말한 값만 저장). 원래 행은 고치지 않고, 읽을 때 Go 순수 함수가 게시 순서로 적용한다. 묶음이 바뀌면(재추출) 판별은 다시 하지 않고 대응만 다시 한다.
  - 구현자가 정한 기본값(사용자 미확인): `change_poll_seconds` 기본 0(끔, 켜려면 300~86400), 처음 본 지 6시간 뒤 종단, 글 하나 10분 상한, 주기당 글 20건, 내용 없는 새 방송 거절(`no_content`), 지난 시각으로의 변경 거절(`past_time`), 내용만 바뀐 칸은 카드에서 강조하지 않음, 옮긴 칸의 캡슐 문구(`금 → 20:00`).
- 2026-11-14: **핸들러·문서 독립 검토와 수정(검토 2관점 → 수정).** 지적 10건이 모두 맞아 고쳤다.
  - 사진 내려받기가 일시적으로 실패한 채 그린 카드가 캐시에 남아 그 주 내내 사진 없이 나가던 결함: 멤버 사진이 없거나 사진을 실제로 받았을 때만 캐시에 넣는다(재현 테스트 `TestScheduleCardRendererRedrawsAfterTransientPhotoFailure`). 계속 실패하는 사진이면 요청마다 다시 그린다.
  - `titleStates`가 겹쳐 시작하는 일치를 놓치던 결함을 고쳤다.
  - 테스트 보강: 동시 요청의 그리기 공유와 첫 요청 취소 분리, 그리기 panic, 사진 예산 만료 뒤 캐시 안 함, 핸들러의 멤버 조회 실패.
  - 문서 정정: 재번역 예약은 이미지 번역이 `succeeded`·`failed`일 때만 받는다(대기 중이면 409 `invalid_state`). 그래서 확정 직후 행 추출이 실패하면 이미지 번역이 끝날 때까지(최대 3시간과 대기열) 다시 뽑을 수 없다. 행 추출 상태는 관리 API와 Console에 없어 Warn 로그나 DB로만 확인한다. 설정 파일이 없는 배포는 행 추출도 꺼져 새로 확정한 포스터에 "없음"으로 답한다. migration 019의 `アソビ隊` 별칭은 멤버 이름 검색에도 쓰이고 이전 image로 되돌려도 남는다.
- 2026-11-14: 2단계 설계 누락 점검(blocker 2, major 11, minor 10)을 `design_amendments.md` G절에 반영했다. blocker 두 건은 이미 정한 보강(주 첫날 전 글 제외, 내용 없는 새 방송 거절)과 같았다.
- 2026-11-14: **2단계 구현 워크플로 시작(7단위).** 기반(shared 타입·`ApplyChanges`·admin 공용 타입) → DB·저장소(migration 020), 카드 렌더러 변경 표시, admin 판정·지시문 → 모델 경계·설정을 나란히 → admin 변경 감지 루프와 bot 카드 조립. 끝나면 전체 검사, 독립 검토, 서비스 문서(변경 감지 절과 재시도 예외 계약), CHANGELOG를 한다.
- 2026-11-14: **2단계 구현 워크플로 완료(7단위).** migration 020(`schedule_poster_posts`, `schedule_poster_changes`), shared `changes_apply.go`(`ApplyChanges`, `ValidateChanges`, `NormalizeDayTime`)와 `changes.go`(`InsertPosts`, `PendingPosts`, `RecordDecision`, `RecordMatch`, `PurgePosts`), admin `changes*.go`(수집, 판별, 대응, `judgeItems`, 재번역, 루프), 설정 키(`change_poll_seconds` 기본 0, `translation.rows.match`, `translation.rows.decide.schedule_info_min`), 카드의 변경 표시(`Item.PrevStart`, `Item.MovedFrom`)와 캐시 키.
  - `prompts/match.txt`는 프로토타입 보정본을 그대로 복사했다(SHA-256 앞 12자리 `fce79ba303b7`).
  - 하니스 기록 대조: 표본 90건(3회 × 30건)과 probe 34건의 판정이 하니스와 같다. 다르게 둔 기대값 15건은 보정 규칙 때문이다(PDT 4건 `foreign_zone`, 합성 8건 `no_content`, 주 첫날 전 글 3건은 수집 조건이 거른다). 지시문 꼬리의 바이트 대조는 2줄(미정 전환의 시각 비움)만 다르다.
  - 1단계 테스트 `TestStoreRecordRowsStoresEveryColumn`의 기대값이 바뀌었다. `PublishedRows`가 이제 `SourceText`·`Badges`도 읽는다(대응 입력에 필요).
  - 직접 실행한 통합 검사(마지막 수정 뒤): `gofmt -l` 차이 없음, 5개 모듈 `go build ./...`, shared·dbtest·api(admin, bot, migrationrunner)의 `go vet`과 `go test -race`, golangci-lint 0건, NilAway, `check-sql-ownership.sh` 모두 통과.
  - 변경 표시 미리보기 PNG(테마 5개, 3:4)는 scratchpad의 `schedule-card-preview/changes/`에 있다. 쿄코·츠즈리 두 장을 직접 열어 겹침과 잘림이 없음을 확인했다.
  - 실제 Vertex·Gemini·FxTwitter 호출은 하지 않았다. 저장소와의 실제 DB 동작은 shared dbtest가, admin 루프는 fake가 확인한다.
- 2026-11-14: **검토 시작.** 사용자 지시(`"전체적인 워크플로의 변경내용을 코덱스 아스트라(병렬허용) 에게도 적대적 리뷰 받을것"`)에 따라 Codex(`gpt-6-astra`) 적대적 리뷰 5건을 영역별로 나란히 돌린다(DB·저장소, 행 파이프라인, 변경 감지, 카드·핸들러, 설계·운영). 설계 일치와 테스트·fixture를 보는 내부 검토 2건도 함께 돈다. 결과를 모아 한 번에 고친다.
- 2026-11-14: **검토 지적 1차 수정 묶음 반영(F1~F20, 승인받은 F14~F16).** 처리 방법의 정본은 `/var/tmp/schedule-card-proto/research/change/review_fixes.md`다. 워커 여섯(shared, admin 행, admin 변경 감지, 카드, 관리 API, 문서)이 같은 worktree에서 영역을 나눠 고쳤고, 결함 수정은 실패하는 시험을 먼저 더했다. 아래 검사 결과는 각 워커의 보고이며 이 기록을 쓰며 다시 돌리지 않았다.
  - shared: `same`도 글이 말한 값을 저장하고 게시 순서로 덮는다(F4, migration 020 제자리 수정). `PendingPosts`가 글의 상태와 도장을 읽고 `RecordMatch`는 읽은 값과 같을 때만 쓴다(F5). `RecordDecision`은 `status='unjudged'` 조건이 이미 CAS라 시그니처를 두었다. 글 상태 `revoked`와 `RevokePost`(F14), `RequestRows`(F15), `PosterPosts`와 `Poster`의 행 필드(F16)를 더했다. 지적된 SQL 조건(CAS, 채널, 포스터 게시 뒤, JST 주 시작)을 하나씩 지워 새 시험이 잡는 것을 확인했다.
  - admin 행: 기록 실패 때 결과를 메모리에 들고 기록만 다시 한다(F1), 표지 흉내 원문의 대괄호 치환과 U+2028·U+2029 거부(F2), 검증되지 않은 표기는 `terms`에서 뺀다(F3), JST가 아닌 행은 `unsupported_zone`(F7), 지표 정의(F16). `service.go` 800줄 상한 때문에 행 루프 상태를 `rowsLoopState`로 묶었다. 하니스 대조의 기대값은 바뀌지 않았다.
  - admin 변경 감지: 대응 결과 보관(F1, 키에 채널을 더하고 앞선 변경 수가 바뀌면 다시 계산), `same` 값 채우기(F4), 취소·휴방의 `row_date`(F10), 되살림 충돌(F11), Vertex에 닿지 못한 실패의 서비스 실패 분류(F12), 수집 멈춤을 본인 원글 기준으로(F13), 시각 없는 항목과 24시 이상 칸(F17), 같은 칸으로 돌린 항목의 `past_time`(F18), 수집 하한과 10쪽(F19), 공백을 뺀 근거 대조(F20), 지표 연결. 작업 중 새로 판별한 글을 읽은 상태(`unjudged`)로 `RecordMatch`에 넘겨 CAS에 막히던 결함을 찾아 고쳤다. 하니스 기록(글 90건, probe 34건)의 판정은 바뀐 것이 없다. 수정을 하나씩 되돌린 변형 26개가 모두 시험에 걸렸다.
  - 카드: 시각 변경 표시는 카드의 날짜 칸으로 정한다(F6, 금 25:00→토 02:00의 기대값이 `금 → 02:00`으로 바뀜). 캐시 키와 렌더 공유 키에 반영 변경 수를 더했다(F8). 미리보기 PNG는 scratchpad의 `schedule-card-preview/changes-fixed/`에 있다.
  - 관리 API: `GET /schedule-posters/{id}/changes`, `POST /schedule-posters/{id}/changes/revoke`, `POST /schedule-posters/{id}/rows`와 포스터 응답의 `rowsStatus`·`rowsError`·`rowsExtractedAt`·`rowsCount`를 생산자 계약과 실제 handler 계약 시험에 넣었다. 일정 포스터 operation은 12개다.
  - 문서: `docs/current/services/hololive-api.md`에 "일정 변경 감지" 절과 "일정 변경 감지 재시도 예외 계약"을 넣고, 행 다시 뽑기·행 지표·`unsupported_zone`·기록 재시도, metrics 노출 서술을 고쳤다. `CHANGELOG.md`에 변경 감지, 관리 API, 지표 항목을 더했다.
  - 남은 것: F12의 토큰 엔드포인트 HTTP 오류는 `cloud.google.com/go/auth`를 직접 의존으로 올려야 분류할 수 있어 하지 않았다(불명 실패로 남는다). 판별의 안전 차단 400은 글 `reason`에 Decisions 오류 코드(`safety` 등)를 그대로 저장하는데, 관리 API 계약의 글 `reason` enum에는 그 값이 없다. F21~F24는 2차 수정 범위다.
- 2026-11-14: **검토 지적 2차 수정 묶음 반영(F21~F27, 지시문 채택, 문서).** 정본은 같은 `review_fixes.md`의 "2차 수정" 절이고 F9는 그대로 두었다. 워커 여섯(shared, admin 지시문·행, admin 변경 감지, 카드, 관리 API, 문서)이 영역을 나눠 고쳤고, 결함 수정은 실패하는 시험을 먼저 더했다. 아래 검사 결과는 각 워커의 보고이며 이 기록을 쓰며 다시 돌리지 않았다.
  - 지시문 채택(두 단계 재검증 통과, `refine/results.json`, `refine/confirm_results.json`): 저장소 `prompts/`에 바이트 단위로 복사했다. SHA-256 앞 12자리는 `match.txt` `5094150c9971`(이전 `fce79ba303b7`), `translate.txt` `7e4ff9ab461e`(이전 `dcdc80d22d49`), `extract.txt` `a9f950f8b38b`(이전 `2c5b41d0b354`)다. §2 표의 해시는 초안 당시 값이다. 효과: 대응은 "같음"을 내용 변경으로 본 오판 5/23 → 1/23(대조 사례 6대0, p=0.031), 진짜 내용 변경 30/30 유지. 번역은 update의 일반적인 말이 포스터 제목을 덮던 사례 0/4 → 6/6 보존. 추출은 이름 오독 14/31 → 0/31, 그림에 없는 이름 0건, 유닛 이름 경고 T12 6 → 0. 판별 결합 규칙(`kind=other`여도 0.8 이상 통과)은 채택하지 않았다.
  - admin 지시문·행: `buildExtractPrompt(weekStart, weekEnd, named)`가 지시문 끝에 `"\n\nKnown names (reading aid only):"`와 멤버마다 `"\n" + nameForms(member)`를 붙인다(끝 줄바꿈 없음). 번역 이름표 줄은 `nameForms`에 `" → " + 한국어 이름 + " / " + 짧은 이름`을 붙인다. `extractRows`·`extractRequest`가 지시문을 인자로 받는다. 하니스 함수를 그대로 쓴 생성기(모델 호출 없음)로 fixture를 다시 만들었고, 이전 지시문으로 기존 번역 fixture 4개를 바이트 단위로 재현한 뒤 바꿨다. `TestExtractPendingRowsMatchesTheHarness`가 서비스 경로의 추출 지시문을 `extract_prompt.txt`와 바이트 대조한다. `testdata/rows/members.json`은 migration 019 뒤의 값(IRyS `아이리스`, `アソビ隊`)이다. 판정·경고 기대값과 모델 출력 기록은 바뀌지 않았다.
  - shared: `RecordMatch`가 반영 변경을 기록하면 같은 트랜잭션에서 `requeue_later_posts.sql`이 같은 포스터·묶음에 대응한 뒤 글(`matched`·`match_failed`)을 `relevant`로 돌리고 도장·사유를 비운다(F21). `RestorePost`와 `restore_post.sql`, `PosterPost.Restorable`을 더했고, 철회·되살리기는 확정본에서만 된다(F23·F27). 오류 순서는 포스터 없음 `ErrNotFound`, 확정본 아님 `ErrInvalidState`, 글이 이어지지 않음 `ErrNotFound`, 상태 다름 `ErrInvalidState`다. migration 019는 IRyS의 빈 `korean_name`을 `아이리스`로 채운다(조건부·멱등, dbtest 확인, F24).
  - admin 변경 감지: 대응 결과 보관을 다시 쓸지 정할 때 앞선 변경의 수가 아니라 내용(`RecordedAt` 제외)을 비교한다. 수만 보면 A < C < B 순서에서 A가 C를 대기열로 돌린 뒤 B가 옛 결과를 기록하는 결함을 시험으로 먼저 실패시키고 고쳤다(F21). 대응의 종단 기준은 `max(글을 처음 본 시각, 묶음 시각, 이 묶음에 처음 시도한 시각) + 6시간`이다(F22). 재번역도 `changeServiceCodes`(400·401·403·404·429·503)와 연결 전 실패를 서비스 실패로 보고, 판정 결과를 `matchJudged` 단계로 보관해 다음 주기에 재번역부터 한다. 토큰 발급 실패 `*auth.Error`를 연결 전 실패에 넣었고 `cloud.google.com/go/auth`를 직접 의존으로 옮겼다(`go mod tidy -diff` 깨끗, F25). 판별 단계의 종단 기준은 그대로 글을 처음 본 시각 + 6시간이다.
  - 카드: 캐시 키와 singleflight 키가 `len(schedule.Changes)`와 그 가운데 가장 늦은 기록 시각을 쓴다. "업데이트" 시각은 그대로 반영 변경만 본다(F26).
  - 관리 API: `POST /schedule-posters/{id}/changes/restore`(`holoRestoreSchedulePosterChange`, 스키마는 revoke와 같음)와 글마다 `restorable`을 생산자 계약과 계약 시험에 넣었다. 확정본이 아니면 409 `invalid_state`다. 일정 포스터 operation은 13개다. 비공개 요청 타입 이름은 `schedulePosterPostRequest`로 바꿨고 계약 스키마 이름은 그대로다.
  - 문서: 서비스 문서의 추출 이름 목록과 `アソビ隊` 배포 순서 한계, 이름표 78명과 짧은 이름 12명 차이, 대응·번역 지시문 동작, F21 다시 대응과 그 한계, 종단 기준, 재번역 실패 분류, 되살리기·`restorable`·오류 순서, 카드 캐시 키, migration 019의 IRyS 교정을 고쳤다. 없어진 한계(되살릴 수 없음, 뒤 글 판정이 옛 상태 기준으로 남음, 재번역 401·403·429와 토큰 발급 실패)를 지웠다. `CHANGELOG.md` 미출시 항목을 맞췄다.
  - 실호출 검증(1차 수정 전 구현, F19·F20이 여기서 나왔다. 새 지시문으로는 다시 하지 않았다): Vertex 72회, Gemini Decisions 171회(503 1회), FxTwitter 11회. 행 추출 38행이 하니스와 모두 같고, 판별 73건의 점수 차이가 0이며, 대응 30건 중 29건이 같다. 시험 DB에서 97건이 한 번씩만 판별되고 셋째 tick의 호출이 0이었다. 결과는 scratchpad `realcall/`에 있다. 모델 품질 사례: 메라 11-11 07:00 행에서 글의 `朝活配信`이 내용 변경으로 판정돼 제목이 `아침 방송`으로 바뀜(정답은 "같음"), 쿄코 `アンビ隊` → `안비대`와 츠즈리 `アリビキまわり隊` 오독(다시 돌리면 바르게 읽음), `kind=other`로 판별돼 대응되지 않은 츠즈리 일정 글 1건.
  - 남은 것: 짧은 한국어 이름 12명 교정(`members.short_korean_name`)과 T12 경고를 낸 별명(つーちゃん, biboo, Beebs, Rissa, アンビ隊)의 별칭 추가는 알림·봇 표시 이름을 바꾸므로 사용자 결정이 필요하다. 지시문 다듬기 2차의 `translate_w1.txt`(`755acb7fb52d`)는 아직 저장소에 없다(모델 비교 시험 뒤 반영). 판별 안전 차단 400의 `reason`과 계약 enum의 불일치는 그대로다. 철회는 뒤 글을 다시 대응하게 하지 않는다. Console의 되살리기 버튼과 생성 계약 재생성은 iris-console 쪽 일이라 이 기록에서 확인하지 않았다.
- 2026-11-14: **검토 2와 3차 수정 묶음 반영(F28~F39).** 정본은 같은 `review_fixes.md`의 "3차 수정" 절이고, 지적 원문은 `review_round2.json`이다. 워커 다섯(shared, admin 행·지시문, admin 변경 감지, 관리 API 계약·카드, 문서)이 영역을 나눠 고쳤고, 결함 수정은 실패하는 시험을 먼저 더해 실패를 본 뒤 고쳤다. 아래 검사 결과는 각 워커의 보고이며 이 기록을 쓰며 다시 돌리지 않았다.
  - 검토 2: 검토자 넷. 하나는 짧은 한국어 이름 12건의 처리 결정(권고 `members_migration`, 근거는 커밋 865f91c7b의 자동 규칙 생성 자료)이고, 셋이 지적 13건(high 1, medium 3, low 9)을 냈다. 코드로 고친 것은 CD-1·R1(같은 결함, F28), CD-2(F29), CD-3(F30), CD-4(F31), CD-5(F32), R2(F33), R3(F34), S1(F35), S3(시험 공백, F36)이다. S2(대응 작업이 겹칠 때), CD-6(TLS handshake 시간 초과와 alert), R4(이름표 멤버 값 검사 없음)는 전제·한계로 서비스 문서에만 적었다. 워커가 R2·R3를 포함해 맡은 지적을 모두 코드에서 재현해 기각한 지적은 없다.
  - F28: 판별의 안전 차단 400은 글 사유 `blocked`로 닫고 Decisions 세부 코드는 Warn 로그의 `error`에만 남는다. 계약 시험 fixture에 `judge_failed`/`blocked` 글과 `match_failed`/`invalid_changes` 글을 넣고, 계약 enum이 저장소의 글 사유 집합과 같은지와 네 값이 실제 응답으로 스키마를 통과하는지 확인한다. 계약 enum은 바꾸지 않았다.
  - F29: 판별의 종단 기준은 `max(글을 원장에 넣은 시각, 이 프로세스가 처음 판별하려 한 시각) + 6시간`이다. 대응(F22)과 같은 방식이다.
  - F30: 저장소 오류가 난 주기(어느 단계든)는 `hololive_schedule_poster_change_cycles_total{result="store_error"}`로 세고 완료 시각 gauge를 갱신하지 않는다. 판별·대응 단계의 저장소 오류는 Warn `…change detection paused` 대신 Error `Schedule poster task failed`(`task`=`changes`)를 남긴다. 설계는 "실패 결과로 센다"만 정했고 라벨 값 이름은 행 지표와 같은 `store_error`로 정했다.
  - F31: 반영 변경을 기록하면 그 멤버의 대기 목록을 다시 읽어, 방금 기록한 글보다 뒤인 글을 게시 순서대로 같은 주기에 이어 처리한다. 주기당 20건 한도는 다시 읽은 뒤 처리한 글도 센다. 다시 읽은 뒤 20건에서 정확히 끊기는 것은 시험으로 고정하지 않았다.
  - F32: 대응의 `bad_answer`는 재시도 묶음의 모든 시도가 응답 해석 실패일 때만이고, 다른 실패가 섞이면 불명 실패다.
  - F33: 행 번역이 경고를 붙인 최종 행에도 `ValidateRows`를 돌려 실패한 시도를 버린다. 시험의 fake `RecordRows`도 `ValidateRows`를 돌린다.
  - F34: 표지 판정 `forgesMarker`(`rows_prompt.go`)는 여는 쪽(`[update:`, `[badges:`, `[day off`, `[cancelled`)만 보고, `markerSafe`와 변경 감지의 `text_marker`가 함께 쓴다. 기록된 표본의 지시문은 바이트 단위로 그대로다. 전각 대괄호나 공백이 섞인 꼴은 막지 않는다.
  - F35: 글이 포스터에 속하는 범위를 `queries/post_window.sql` 한 곳으로 모았고, `insert_posts`, `pending_posts`, `record_match`, `poster_posts`, `revoke_post`, `restore_post`가 `/* post_window */` 자리에 끼워 쓴다. 범위에 주 끝 상한(주 마지막 날 다음 날 00:00 JST 전)을 더했다. 설계는 심야 표기를 덮는 상한도 허용했지만, 대응 루프가 JST 오늘 날짜로 포스터를 고르므로 월요일 새벽 글은 지난주 포스터에 대응할 수 없어 그 시간대를 열면 지난주 화면에서 이번 주 글을 철회하는 경로가 다시 생긴다. 그래서 00:00으로 했다. 변경 목록은 조치할 수 있는 상태의 글 최근 200건과 나머지 최근 50건(응답 최대 250건)이고, 계약 `SchedulePosterPosts.posts.maxItems`를 250으로 고쳤다(고치기 전 시험이 `250 items > maxItems 50`으로 실패).
  - F36: 다른 멤버의 확정본에 대응한 뒤 글과 같은 포스터의 옛 묶음에 대응한 뒤 글이 대기열로 돌아가지 않는 DB 시험을 새로 더했다(기존 시험에 단언을 더하면 maintidx에 걸려 따로 뗐다).
  - F37: migration 019가 짧은 한국어 이름 12건을 지금 값이 옛 값일 때만 고친다. 별칭은 더하지 않는다. dbtest로 12건 교정, 목록 밖 멤버 보존, 운영자가 고친 값 보존, 다시 적용해도 같음을 확인했다. admin `testdata/rows/members.json`과 번역 지시문 fixture 4개를 하니스 생성기로 다시 만들었고, 이름표 78명이 하니스 `load_members()`와 같다. 운영 적용에는 migration 적용 승인이 따로 필요하고, 적용하면 알림·봇 응답·방송 기록·달력 카드의 12명 표시가 성에서 이름으로 바뀐다.
  - F38: `prompts/translate.txt`를 `translate_w1.txt`로 바꿨다(`cmp` 일치, SHA-256 앞 12자리 `755acb7fb52d`). 지금 세 지시문은 `match.txt` `5094150c9971`, `translate.txt` `755acb7fb52d`, `extract.txt` `a9f950f8b38b`이고 §2 표도 이 값으로 고쳤다.
  - 지시문 다듬기 2차 결과(`review_fixes.md`의 같은 이름 절): 번역은 `translate_u2`에 농담·말장난 판단 문장 하나를 더한 w1을 채택했다. 말장난으로 적힌 조건의 메모 누락이 0/12 → 11/12(p<0.001), 경고 수 12 → 10, update 사례 71/72 → 72/72이고, 단서였던 cecilia_1027 8행 메모는 20회씩 다시 돌려 흔들림으로 확인했다(15/28 대 16/28). 추출의 없는 배지는 문장 후보 둘이 효과가 없었고 생각 수준 MEDIUM은 그 오류를 구분되게 줄이지 못하면서 쿄코 양식에서 칸 도장을 모든 행 배지로 퍼뜨려 지시문도 생각 수준도 바꾸지 않았다. 남은 것은 "나중에 정함"·"TBA" 행의 상태 흔들림과 시청자에게 청하는 짧은 말의 메모 누락(30~40%)이다.
  - F39: 훅이 지적한 비공개·시험 선언 주석을 워커마다 확정했다(기본 삭제, 남긴 것은 지우면 잃는 사실을 보고). 커밋 때 `# keep-comment:` 줄로 쓴다.
  - 문서: 서비스 문서에 위 사실(안전 차단 `blocked`, 종단 기준, `store_error`, 다시 읽기, `bad_answer` 조건, 표지 판정, 글 범위와 200+50 상한, 12명 교정과 배포 주의, 번역 지시문)과 S2·CD-6·R4의 전제를 적고, 없어진 한계(12명 이름표 차이, 다시 대응이 다음 주기까지 밀림)를 지우거나 고쳤다. `CHANGELOG.md` 미출시 항목을 맞췄다.
  - 남은 것: `metrics_test.go`의 `TestMetricsExposeEveryLabelValueBeforeTheFirstEvent`가 `change_cycles_total`의 옛 라벨 집합(`completed`, `paused`)을 기대해 이 기록을 쓸 때 scheduleposter 패키지 시험이 그 하나로 실패한다(`store_error`를 더하는 한 줄). iris-console의 생성 계약 사본은 `maxItems` 250과 새 설명으로 다시 만들어야 하며, 그 전에는 글이 50건을 넘는 포스터의 변경 목록을 게이트웨이가 거절한다. 일요일 심야 칸에 관한 월요일 새벽 글을 지난주 포스터에 대응시키는 동작은 없다(대응 루프의 포스터 선택을 바꿔야 함).
- 2026-11-15: **추출 모델 실험과 3회 다수결 구현.** 원자료는 `/var/tmp/schedule-card-proto/research/change/perfect_results.json`(배열 [5] 구성 찾기, [6] 독립 검증), 채점기·정답은 `refine/perfect/`(`score.py`의 `vote`, `q/truth_q1.json`), 결함 등급 기준은 `refine/perfect/severity.md`(U 용납 안 됨, A 용납 가능)다. 포스터 33장, Vertex 약 1,800회.
  - 조건별 결과(단독 1회): carbon `LOW` + `extract.txt`(`a9f950f8b38b`)는 U가 있는 실행 6/277(대부분 어두운 휴방 칸에 글이 적힌 칸의 off 대 tentative), A 304건(실행당 약 1.1, 대부분 A1 표기·위치 차이). carbon `MEDIUM`은 행 누락·원문에 JSON 조각이 새는 출력 깨짐이 8/131회(U 29건, 다른 조건 약 900회에서는 0)라 쓰지 않는다. argon `MEDIUM`은 U 실행 4/151, argon `HIGH`는 5/111, hydrogen은 U 실행 20~42%였다. 지시문 수정 후보 q1~q5는 시험 칸 문구와 겹친 예시 낱말을 쓴 q2만 통과했고(q2 0/9 대 예시만 바꾼 q5 9/9, p≈2e-5) 그래서 채택하지 않았다. 지연은 단독 최대 521초, 동시 부하에서 718초라 요청 시간 제한을 900초로 했다.
  - 3회 다수결: 1차 49묶음은 U 0, 독립 재실행 43묶음은 U 1(shiori_1019 'Resting or Not'이 off 2·tentative 1로 갈려 다수결 off). 합쳐 U 1/92이고, 그 묶음을 포함해 방송 있음/없음이나 행 유무가 갈린 4묶음은 모두 실행 간 불일치가 보였다. 그래서 안전장치는 다수결이 아니라 불일치 처리(갈린 칸을 `tentative`로 두고 경고)다.
  - 독립 검증: 33장 그림으로 정답 257칸 전부(조건 사이에 갈린 126칸과 갈리지 않은 131칸)를 대조해 그림과 어긋난 정답은 없었다. 다만 사람도 갈릴 수 있는 4칸(shiori_1019 'Resting or Not', h_shiori_0831 'TBD Streams Beyond Here', h_kaela_1026 'three meetings and a dream to stream', tsuzuri_yt6 화살표로 이은 내구 방송)은 권고 구성이 거의 늘 같은 답을 낸다. 한 칸만 정답을 반대로 정해도 277회 중 18~24회가 U가 되고(다수결 5~7묶음) 경고는 뜨지 않는다. 처음 보는 포스터로는 시험하지 못했다.
  - Claude 번역 비교(`review_fixes.md`의 "Claude 번역 비교 결과", "Claude 번역 재확인 (지시문 x1)"): Anthropic 요청 302회와 88회. 모든 Claude 조건이 update 사례 `revive_changed`에서 제목을 Minecraft로 남겼고(x1에서도 Sonnet 5.5·Opus 5.5 각 11/11, hydrogen + x1은 0/23, p=3.5e-9), `doubt_ko`도 비어 제품이 잡지 못한다(U4). 지어낸 참가자는 양쪽 모두 0. 그래서 번역은 hydrogen `MEDIUM` + `translate_x1.txt`(`06d2d03104db`, 시청자에게 청하는 말을 `note_ko`에 두는 문구를 더함)로 정했고 저장소 `prompts/translate.txt`와 번역 지시문 fixture 4개를 바꿨다. Claude를 쓰려면 새 운영 의존성·키 배치 승인이 필요하다.
  - 구현: 추출 기본값을 `gemini-4-carbon` `LOW` 900초로 따로 두었다(번역·대응 기본값은 그대로). 추출은 같은 요청을 3번 동시에 보내고(실행마다 재시도·사용량 기록·검증), 성공 2개 미만이면 `extraction_failed`, 성공 결과는 `rows_vote.go`가 하니스 `vote`·`match_rows`와 같은 짝짓기로 합친다. 불일치 경고는 추출 단계 경고 뒤에 `E6`(방송 유무, `tentative`로 둠)·`E7`(날짜·시작·끝·시간대·원문·배지 쌍에 2개 이상 같은 값 없음)·`E8`(2/3에만 있어 남긴 행)·`E9`(1개에만 있어 뺀 행)로 붙인다. 경고는 DB `schedule_poster_rows.warnings`에만 있고 관리 API·Console·카드에는 나오지 않는다. 포스터 상한은 추출 단계(2×추출 시간 제한 + 150초)와 번역 단계(2×번역 시간 제한 + 150초)의 합(기본 41분)이고, 추출 단계 상한이 지나면 늦은 실행을 끊고 성공한 실행들로 정해 번역 몫을 남긴다.
  - 검토 지적 5건은 모두 재현해 고쳤다. F1 원문·배지 쌍에 다수가 없으면 경고가 없던 것(`E7 … 원문·배지`), F2 늦은 실행 하나가 포스터 상한을 다 써 번역이 만료된 context로 실패하던 것(추출 단계 마감), F3 빈 결과 2개와 행 있는 결과 1개가 `no_rows` 대신 `extraction_failed`가 되던 것, F4 `9:00`/`09:00`과 빈 시간대/`JST`를 다른 값으로 세어 잡음 경고가 나던 것, F5 서비스 문서의 번역 지시문 해시. 각각 실패하는 시험을 먼저 더했다.
  - 남은 것: 운영 설정 파일이 `translation.rows.extract`에 `model`·`timeout_seconds`를 직접 적었으면 새 기본값이 반영되지 않으므로 배포 전에 확인한다. 추출 Vertex 호출과 비용은 3배다. 실제 Vertex 동시 3회의 지연·할당량 영향과 운영 DB로는 검증하지 않았다. 경고를 사람이 보는 길(Console·관리 API)은 없다.
- 2026-11-15: **제품 Go 경로 실호출과 JST가 아닌 포스터의 재시도 수정.**
  - 첫 실호출(`kaela_1108` GMT+7, `shiori_1019` CDT, `w_tsuzuri`)은 `kaela_1108` 하나가 상한 15회를 다 썼다. JST가 아닌 시간대의 행이 있는 응답도 실행마다 10번 재시도해, 그런 포스터 하나가 carbon 호출 30회와 추출 단계 상한(32분 30초)까지 쓰고서야 `unsupported_zone`이 됐다. 연구 표본 33장 가운데 22장(EN·ID 멤버, 혼합 시간대 포함)이 이런 포스터이고, 기존 실행 802회에서 시간대가 JST인지 아닌지는 포스터마다 한 번도 갈리지 않았다. 그래서 시간대 오류는 재시도하지 않고, 시간대는 다른 검증을 모두 통과한 응답에서만 보게 했다(깨진 출력은 형식 오류로 재시도). 실패하는 시험을 먼저 더했고, 검토 지적(섞인 실패의 분류 시험, 문서의 호출 횟수 단정)도 반영했다. 실행 사이에 시간대가 갈리는 다중 시간대 포스터에서 한 실행이 다른 시간대를 고르고 다른 실행이 일시 오류로 끝나면 회복 없이 `unsupported_zone`이 될 수 있으나, 기존 실행에서 그런 포스터는 없었다.
  - 다시 돈 실호출(Vertex 19회, 재시도·오류 0): `kaela_1108`은 추출 3회 뒤 `unsupported_zone`. JST 포스터 4장(`w_tsuzuri`, `tsuzuri_yt6`, `kyoko_1102`, `w_mela`)은 모두 기록됐고 행 수가 정답과 같으며 U 0, A 9(A1 표기·A2 배지). 세 실행의 행 수·날짜·시각·상태·배지가 모두 같아 E6~E9는 붙지 않았고, 실행 사이 차이는 원문의 구분 기호·전각 숫자·화살표뿐이었다. 번역 경고는 `w_mela`의 T12 1건(`biboo` → 비쥬, 사람은 맞음). JST 포스터의 추출은 호출마다 8~15초, 번역 19~31초, 포스터마다 30~46초였다. 운영 설정에는 `translation.rows`가 없어 기본값이 쓰였다. 멤버 목록은 `testdata/rows/members.json`이었다.
  - 제품 대상만 다시 센 연구 수치(새 호출 없음): 정답의 모든 행이 JST이거나 시간대가 없는 11장 기준 carbon `LOW` 단독은 U가 있는 실행 1/87(채점기 자동 등급 U4 1건이며 앞선 검증자는 A1로 판정), 3회 다수결은 0/29묶음이다. 앞서 보고한 U(단독 6/277, 다수결 1/92)는 모두 제품 대상 밖 포스터에서 나왔다. 단독 실행 87회 사이에 행 수·상태·날짜·시각이 갈린 적이 없어 E6·E8·E9는 0으로 예상되고, E7은 같은 포스터에서 3개를 고른 조합의 2.8%에 붙는다(모두 `w_kyoko` 11-10 배지 꼴). 사람도 갈릴 수 있는 4칸 가운데 제품 대상에 남는 것은 `tsuzuri_yt6` 화살표 내구 방송 하나이고, carbon `LOW`는 정답과 18/18 같다. 정답 판단이 반대라면 경고 없이 18/18 틀린다. 계산 스크립트와 결과는 `/var/tmp/schedule-card-proto/research/change/product_scope/eligible/`, 실호출 결과는 같은 폴더의 `realcall_rows4/`.
- 2026-11-15: **JST가 아닌 포스터를 JST로 바꿔 카드 지원.**
  - 사용자가 "JST로 변환해 카드 지원"과 "한국 날짜 칸으로 다시 묶기"를 골랐다. 추출 검증은 시간대 표(빈 값·`JST`, `KST`, 고정 시차, `PT`·`MT`·`CT`·`ET`·`CET`·`BST`·`AET` 계열 지역)에 있는 시간대를 받고, 다수결의 시간대 비교는 같은 지역·같은 시차 표기를 같게 본다. 추출 경고(E1~E9)와 번역은 현지 값 그대로 하고, 번역까지 끝난 행을 저장 직전에 JST 날짜·시각(00:00~05:59는 전날 24:00~29:59)으로 바꾼다(`rows_zone.go`). 시작 시각 없는 행은 포스터의 최빈 날짜 이동을 따르고, 동률이면 그날 현지 20:00의 이동을 쓴다.
  - 저장: migration 019를 같은 파일에서 고쳐(아직 어디에도 적용 전) `schedule_poster_rows.source_zone`과 `schedule_posters.rows_day_shift`(CHECK -1~1)를 더했다. `RecordRows`가 하루 밀기 값을 묶음과 같은 트랜잭션에서 쓰고, 거절·주 범위 수정이 0으로 돌린다. `published_rows.sql`(변경 감지의 게시 포스터 선택도 같은 쿼리)은 민 주 범위로 고르고, 카드 창도 그만큼 민다. 창 밖의 바꾼 행은 끝 칸에 JST 요일을 붙여 그린다. 변경 감지는 바꾼 행이 있는 포스터를 건너뛴다. JST 포스터의 카드 PNG는 수정 전후 45조합(테마 5 × 판형 3 × 입력 3)이 바이트 단위로 같았다.
  - 설계와 다르게 한 곳: 카드 항목의 `Converted`는 handler가 아니라 카드 입력을 만드는 `render/schedule_card.go`의 `newScheduleCard`가 채운다(handler는 행 묶음을 그대로 넘긴다). 시작 시각이 없는 행과 휴방은 끝 칸으로 옮기지 않는다(요일을 붙일 시각이 없어 다른 날 칸에서 그날 일정으로 읽히므로). 요일은 `slot`에 칸을 더하지 않고 시작 표기 앞에 붙인다(`slot`이 커지면 gocritic `rangeValCopy` 상한을 넘는다). 시작 시각 없는 바꾼 행의 끝 시각은 비운다.
  - 실호출(Vertex 24회, 재시도·검증 거부 0): JST가 아닌 포스터 6장(`kaela_1108` GMT+7, `gigi_1027` PDT, `shiori_1110` CDT·CST, `cecilia_1110` CET·PDT, `anya_yt1` WIB와 시간대 빈 휴방 칸, `h_yt_cecilia-immergreen_2` CEST·JST)이 모두 기록됐다. 정답 행을 Python `zoneinfo`로 따로 바꿔 견주면 U 0, A1 2건(추출 표기), 변환 차이 0, 하루 밀기 값 6장 모두 일치(`gigi`·`shiori` +1). 표준·일광 표기를 그날 지역 시각으로 읽는 것(`CDT` 표기 칸이 그날 CST)도 실제 응답에서 확인했고, 제품 카드 PNG의 창·칸 배치도 기록과 같았다. 아래 검토 후속 수정 뒤 같은 현지 다수결 행을 새 코드로 다시 바꿔 46행·하루 밀기 값 6개가 모두 같음을 확인했다(시각 있는 칸의 빈 시간대는 6장에 없었다). 서머타임 종료 뒤 시작 시각이 있는 칸, 끝 칸 접기, '외 N건' 접기는 실제 응답에서 나오지 않았다. 결과는 `/var/tmp/schedule-card-proto/research/change/product_scope/realcall_zone/`.
  - 남은 것: 하루 민 확정본과 밀지 않은 확정본이 이어지는 경계일에는 `!스케쥴`이 '없음'이 될 수 있다(서비스 문서에 적음). 검토 후속: `published_rows.sql`은 민 범위로 거르되 밀지 않은 주 시작일로 정렬해 같은 주 수정본이 앞서게 했고, `pending_rows.sql`·`request_rows.sql`도 민 범위의 끝(`week_end + rows_day_shift`)까지 추출을 받는다. 다수결은 같은 key 안에서 가장 많은 시간대 표기를, 동률이면 바꾸지 않는 표기(없음·JST)를 저장한다. 검토가 설계 밖으로 남긴 결함과 그 뒤 독립 검토의 지적도 고쳤다(각각 실패하는 시험 먼저): 시각 없는 칸(EN·ID 포스터의 휴방 칸 등)의 빈 시간대는 추출 지시문이 비우게 하므로 시각 있는 칸 다수의 시간대로 읽고, 시각 있는 칸의 빈 시간대는 계약대로 JST다(포스터 대표 시간대를 모든 빈 행에 쓰면 JST 포스터의 해외 칸 하나가 한 주 전체를 바꿨다). 시각 있는 칸을 짝지은 실행들이 다른 시간대로 읽은 칸은 날짜·시작이 서로 다른 시간대의 실행에서 올 수 있어 `E10 실행 간 시간대 불일치`와 함께 `planned`를 `tentative`로 둔다. 하루 밀기 값은 동률이면 포스터의 최빈 날짜 이동을 먼저 고른다(현지 일요일 방송이 없는 PT 주가 0이 되어 경계일에 앞 주의 밀린 월요일 방송이 카드에서 빠지던 것). 카드는 창에서 이틀 안의 바꾼 행만 끝 칸에 접는다(한 주 뒤 행이 같은 요일의 끝 칸에 그날 일정처럼 들어가던 것). 생산자 계약의 `rowsError` 설명도 `unsupported_zone(JST로 바꿀 수 없는 시간대의 행)`으로 고쳤다(enum은 그대로, Iris Console 생성 계약은 이 커밋 기준으로 다시 만든다).
