# 홀로봇 기능별 접기와 구조 개선 계획

2026-10-01 재감사 반영안입니다. 글자 수 임계를 없애고 기능과 표시 항목 수로 접기를 결정합니다. 사용자는 **조회 목록 1건은 펼침, 2건 이상은 접기**를 선택했습니다. 방송·콘텐츠 알림은 묶음도 자동 패딩을 없앱니다. 실제 사용되는 뉴스 표시 처리만 공통화하고 미사용 코드·불필요한 래퍼·렌더 실패를 가리는 대체 본문을 정리하는 계획입니다. **2026-10-01 사용자 구현 요청에 따라 애플리케이션 구현과 로컬 검증을 진행합니다. 커밋·게시·배포·운영 데이터 수정은 포함하지 않습니다.**

## 재감사 결론

- [bot 행사 요약](../../../hololive/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter/formatter_major_event.go)은 실제 동작 코드에서 호출되지 않습니다. 행사 요약 공통 builder 추가안은 철회하고 미사용 bot 구현 삭제를 이번 구현의 첫 단계로 둡니다.
- `Presentation{Kind, HasContent, Text}`는 추가하지 않습니다. 기능별 호출 위치와 기존 설정 분기로 충분합니다. 다만 ‘2건 이상 접기’ 판정은 한 곳에 두기 위해, 옮겨 오는 `FoldForSeeMore`와 같은 `internal/templateview`에 표시 건수 판정 함수를 두고 bot/LLM은 표시 건수만 넘깁니다. LLM의 `renderNotification`은 접기 대상인 보고서 세 개만 사용하므로 유지합니다.
- 뉴스는 기존 [convertMemberNewsDigest](../../../hololive/hololive-api/internal/planes/llm/runtime/api_internal_membernews.go)를 재사용하고 `membernewscontracts.Digest`를 공통 builder의 입력으로 씁니다. 새 모델·adapter는 필요하지 않습니다.
- [renderDigestMessage](../../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/scheduler/digest_helper.go)는 formatter 미주입·빈 렌더 결과·nil digest에 코드 본문을 만듭니다. 실제 조립 경로는 formatter를 주입하며 정상적인 뉴스 없음은 [emptyDigest](../../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/service.go) 객체입니다. 이 상태들을 구분해 렌더 실패 시 미발송 계약을 일관되게 적용하는 안을 채택합니다.

## 표시 정책

아래 접기는 `BOT_SEE_MORE_FOLD=true`일 때 프로그램이 삽입하는 패딩 기준입니다. 카카오톡 자체의 긴 메시지 접기까지 끄는 기능은 아닙니다.

| 기능 | 적용 기준 |
| --- | --- |
| 라이브·예정 방송·채널 일정·방송 이력·알람 목록·멤버 목록·캘린더 텍스트 | 표시 결과 0건은 안내문, 1건은 펼침, 2건 이상은 접기. 멤버 지정 조회에도 동일하게 적용합니다. |
| 멤버 뉴스 직접 조회·예약 다이제스트 | 같은 항목형 뉴스 기능이므로 0건은 안내문, 표시 블록 1개는 펼침, 2개 이상은 접기로 통일합니다. 표시 블록은 `TopItems` 수에 `MoreSummary`가 있으면 1을 더한 값입니다. |
| 주간·월간 행사 요약 | 항목형 보고서에도 1건 펼침, 2건 이상 접기를 적용합니다. 0건은 기존 빈 반환·미발송 계약을 유지합니다. |
| 멤버 프로필·전체 도움말 텍스트 | 건수로 나눌 목록이 아닌 상세 기능이므로 유효 본문이 있으면 길이와 무관하게 접습니다. |
| 방송·선행공개·영상·쇼츠·커뮤니티·축하·생일 방송·X 스페이스 알림 | 단일·묶음·길이와 무관하게 자동 패딩을 넣지 않습니다. 방송 N분 전·시작 알람은 현재 같은 방·같은 분의 여러 방송 묶음이 250자를 넘을 때만 접히며, 변경 후에는 묶음도 접지 않습니다. |
| 빈 결과·조회 불가·오류·구독 상태·추가/삭제 결과·후보 선택·사용법·졸업 안내 | 펼칩니다. `알람 목록`은 조회지만 `행사 목록`은 현재 구독 상태 안내입니다. 명령어 단어로 분류하지 않습니다. |
| 이미지 | 기존 도움말·캘린더·썸네일 발송을 유지합니다. 기존 텍스트 대체 경로만 위 정책을 적용합니다. |

건수는 표시 데이터를 기준으로 합니다. 멤버 목록은 정규화 후 유효 멤버 수, 캘린더는 nil 멤버 제외 후 Count, 뉴스는 위의 표시 블록 수를 씁니다. 기본 템플릿은 `TotalCount`를 표시하지 않고, 요약기 스키마에는 여러 줄 `MoreSummary`의 길이 제한이 없으므로 `TotalCount`나 `len(TopItems)`만으로는 화면과 판정이 어긋납니다. 다른 목록은 개수·필터·표시 한도 안내가 머리 문단에 있으므로 항목 수만 셉니다. 행사 요약은 LLM 본문 사용 시 Events view를 비우므로 원본 행사 수를 씁니다. 채널 정보 없음과 일정 없음은 모두 펼치되 의미를 보존합니다. 예약 뉴스·행사는 즉시 확인할 알람과 구분해 읽기용 보고서로 취급하는 권장안을 유지합니다.

## 구현 작업과 순서

- [x] **미사용 행사 코드 정리:** bot의 주간/월간 행사 formatter, 전용 data 타입과 `buildMajorEventViews`를 제거합니다. LLM의 한 줄 `buildMajorEventViews`·테스트 전용 `formatMajorEventDatesFromDB` 래퍼도 제거하고 기존 `templateview` 함수를 직접 사용합니다. bot의 실제 구독·상태·사용법 기능은 유지합니다. 날짜·링크·요약 중복 방지 검증은 실제 소유 패키지의 테스트로 보존합니다. 삭제 후 `templateview/major_event.go`의 소비자는 LLM뿐이지만, 같은 패키지에 bot/LLM 공용 뉴스 builder가 들어오므로 위치는 유지합니다.
- [x] **뉴스 표시 처리만 공통화:** `internal/templateview`에 `membernewscontracts.Digest`와 `messagestrings.Store`를 받는 builder를 둡니다. bot은 계약 DTO를, LLM은 기존 `convertMemberNewsDigest`의 결과를 넘깁니다. 양쪽 template data·카테고리 변환·항목 복사를 합치고 입력 불변성을 유지합니다. 표시 블록 수 계산도 builder가 맡아 수동 조회와 예약 다이제스트가 다르게 판정하지 않게 합니다. HTTP DTO·JSON 필드·LLM 모델은 바꾸지 않습니다.
- [x] **알람 접기 제거와 함수 소유권 이동:** worker `dispatchrun/alarm_dispatch_render.go`와 shared YouTube outbox `format/formatter.go`의 두 접기 호출을 제거합니다. `RunnerConfig`, `youtubedispatch` 생성자/wrapper, `workerapp/build_egress.go`, shared `format/payload.go`에서 접기 필드·인자도 정리합니다. `FoldForSeeMore`·`cutSeeMoreHead`·패딩/머리 문단 상수는 `hololive-api/internal/templateview`로 옮기고 길이 인자·임계 상수를 삭제합니다. `KakaoZeroWidthSpace`는 shared에 남깁니다. worker의 API internal 직접 import를 막는 패키지 경계와 최종 payload 회귀를 함께 유지합니다.
- [x] **bot/LLM 연결 단순화:** bot의 반복되는 render→실패 문구→선택적 접기를 `ResponseFormatter`의 작은 helper로 묶습니다. LiveQuery의 머리 안내 삽입과 문자열로 작성하는 방송 이력은 기존 조립 후 `foldSeeMore`를 사용하고, `BroadcastHistoryEmpty`는 직접 반환합니다. LLM의 `renderNotification`은 유지하고 `foldEligible` bool을 전달합니다. 이름과 의미는 1건에도 내용이 있다는 점을 반영해 `hasContent`와 구분합니다. 목록·보고서의 접기 자격은 `internal/templateview`의 표시 건수 판정 함수 하나로 계산하고, 각 formatter에 `>= 2` 조건을 따로 두지 않습니다. 프로필·도움말은 유효 본문이 있으면 자격이 있습니다. 설정 ON과 접기 자격이 모두 참일 때만 접습니다. bot 오류 문구와 LLM 오류 반환 계약은 각각 유지합니다.
- [x] **예약 뉴스 렌더 경로 단일화:** `renderNotification`의 공백뿐인 결과를 오류로 처리합니다. `renderDigestMessage`는 nil formatter·nil digest·렌더 오류·빈/공백 결과를 실패로 반환하고 코드 대체 본문과 `emptyHeader` 전달값을 제거합니다. `processDigestForRoom`의 기존 실패 집계로 보내며 enqueue하지 않습니다. 정상적인 빈 뉴스 객체는 템플릿 안내문으로 발송하고 접지 않습니다. 주간·월간 모두 적용하며 output guard와 구독 멤버 없음의 skip을 유지합니다. 상위 요약 서비스의 별도 계약이 있는 대체 요약 생성은 이번 제거 대상이 아닙니다. `renderNotification`의 공백 오류 처리는 주간·월간 행사 요약에도 적용됩니다. 현재는 행사 템플릿이 공백을 렌더하면 `enqueueToRooms`가 빈 본문을 enqueue하고(`OutboxRepository.Enqueue`에 빈 본문 검증 없음) 행사를 알림 완료로 표시합니다. 변경 후에는 enqueue·표시 없이 오류를 반환하고 다음 주기에 다시 시도합니다.
- [x] **설정 중복과 주석 정리:** `loadBotConfig`가 `LoadSeeMoreFold`를 재사용하도록 해 bool 해석을 한 곳에 둡니다. 다른 환경변수 오류와 함께 보고하는 기존 엄격 검증을 보존합니다. `config_types.go`의 ‘긴 목록’ 설명, `markdown.go`의 접기 함수 소유 위치를 전제한 주석도 갱신합니다. 설정 이름·기본 true·false 차단 기능은 유지합니다.
- [x] **문서와 회귀 검증:** [스타일 가이드 8절](../architecture/MESSAGE_STYLE_GUIDE.md), [API runbook](../runbooks/hololive-api.md), [worker runbook](../runbooks/alarm-worker.md), [worker service](../services/alarm-worker.md)를 갱신합니다. 250자 조건·묶음 알람 접기·제거된 bot 행사 요약·bot/LLM 동명 3곳 parity 설명을 실제 소비 경로에 맞춥니다. 뉴스의 두 경로 일관성은 유지합니다.

먼저 뉴스 builder와 미사용 코드 정리에서 기존 렌더 결과를 보존하고, 알람 호출 제거·접기 함수 이동·소비자 갱신을 함께 완료한 뒤 새 기능/건수 정책을 검증합니다. 템플릿 본문·DB 스키마·저장된 사용자 override는 변경하지 않습니다. `youtubedispatch.MessageFormatter` 전체 wrapper 제거는 이번 범위에 넣지 않습니다.

## 보존 조건

머리 문단 최대 4줄·패딩 500개·멱등성과 한 줄/빈 본문 무변경을 유지합니다. 제목 보호용 단발 ZWSP, 가시 문자·URL·항목 순서·제목 축약·표시 한도는 보존합니다. 사용자 template에 직접 넣은 패딩을 일괄 제거하지 않습니다. 일반 텍스트/Markdown 전송, reply outbox·pinned request·예약 payload·`PreRenderedMessage`의 저장 본문/route/ID도 재작성하지 않습니다. 새 정책은 새로 렌더하는 메시지에 적용합니다.

## 구현 시 검증 대상

| 대상 파일과 경로 | 검증할 동작 |
| --- | --- |
| shared `util/kakao_test.go`와 `markdown_test.go`의 접기 통합 사례 → API `internal/templateview`로 이동 | 길이 임계 제거, 머리 문단·패딩 1회·한 줄/빈 본문·기존 패딩 보존. shared의 순수 Markdown 테스트는 남기고 API internal에 대한 역방향 의존은 만들지 않습니다. |
| bot `formatter_fold_test.go`, `formatter_readability_test.go`와 목록·이력·뉴스 테스트 | 각 기능의 0/1/2건, 같은 건수의 짧은/긴 본문, 긴 필터의 빈 이력, 조회 미확인, 필터링 후 빈 목록, 설정 OFF. 프로필/도움말 및 이미지 성공 시 중복 텍스트 미발송도 보존합니다. |
| `internal/templateview` 뉴스 테스트, LLM `providers_membernews_routes_test.go`, `formatter_llm_scheduler_fold_test.go`, `formatter_llm_scheduler_major_member_test.go` | 기존 변환 재사용·원본 불변성·카테고리 표시명·뉴스 bot/LLM 일관성·행사 요약/목록 중복 방지. 빈 행사 반환과 nil/빈 뉴스 구분. |
| `internal/templateview` 접기 판정과 뉴스 builder 테스트 | 표시 건수 0/1/2, 뉴스 `TopItems` 1건+`MoreSummary` 없음(펼침)·있음(접기), `TopItems` 2건(접기). bot/LLM이 같은 결과를 내는지 확인합니다. |
| 행사 scheduler `scheduler_test.go`, `monthly_scheduler_test.go` | 공백 렌더 결과에서 enqueue·알림 완료 표시가 없고 오류를 반환합니다. 기존 `TestSendWeeklyNotification_FormatFailure_DoesNotEnqueueOrMark` 계열에 사례를 추가합니다. |
| 뉴스 scheduler `output_guard_test.go`, `scheduler_test.go`, `monthly_scheduler_test.go` | formatter 미주입/nil digest/렌더 오류/빈·공백 본문에서 Failed=1, Sent=0, enqueue=0. 정상 빈 뉴스는 안내문으로 처리하고 output guard·skip 계약은 보존합니다. |
| worker `workerapp/alarm_dispatch_see_more_fold_test.go`의 4개 테스트와 `youtubedispatch/send_engine_see_more_fold_test.go` | 실제 테스트 DB template→fake Iris 최종 payload에서 두 알람 경로의 단일/묶음 모두 자동 패딩 없음. 기존 제목·항목·URL·직접 패딩한 override는 보존합니다. API internal 상수를 worker 테스트로 역참조하지 않습니다. |
| `settings/config_test.go`, `config_strict_env_test.go`, `apiplane/llm_scheduler*_test.go` | 미설정/true/false/잘못된 bool과 다른 환경변수 오류 집계. |
| egress·명령 transport·delivery snapshot 회귀 | 일반 텍스트와 Markdown 최종 payload, 기존 요청의 본문/route/ID 재전송 보존. `TestAlarmRequestReplaysPinnedTemplateRouteAfterRestart` 등 기존 회귀 유지. |

검증은 kapu에서 수행합니다. 관련 패키지 테스트와 적용되는 Stage 3/prerequisites·NilAway/race는 차단 조건으로 유지합니다. 소스 식별자/문서 문자열 grep 검사나 checker 자체 테스트는 추가하지 않습니다. 커밋·게시·배포·운영 데이터 수정은 이번 구현에 포함하지 않습니다.

## 계획 수립 당시 검증과 한계

이 대화의 앞선 점검에서 공통 접기·bot 도움말/프로필·LLM toggle, worker `workerapp`의 접기 관련 4개 테스트, shared-go 패딩 보존 테스트가 통과했습니다. 코드 변경이 없어 결과를 재사용합니다. 이번 재감사에서는 뉴스 scheduler의 `TestProcessDigestForRoomCountsFormatFailureWithoutEnqueue`, `TestProcessDigestForRoomFailsClosedWithoutOutputGuard`, `TestProcessDigestForRoomBlocksRestrictedOutput`과 YouTube 경로의 `TestDispatchDeliveryRowsFoldsLongGroupedShortsAtFinalPayload`를 추가 실행해 통과했습니다. 이후 LLM 뉴스·행사 scheduler 패키지 전체를 `-count=1`로 다시 실행해 통과했습니다. 뉴스 건수 기준은 기본 템플릿(`217_template_compact_separators.sql`), 요약기 스키마, `MoreSummary` 생성 코드를 대조해 정했고, 행사 공백 렌더 영향은 `enqueueToRooms`와 `OutboxRepository.Enqueue`를 대조해 확인했습니다. 운영 DB의 사용자 템플릿은 확인하지 않았습니다.

호출부·기존 변환·정상 빈 뉴스 객체·스케줄러 실패 분기를 코드와 대조했습니다. 운영 DB·배포 바이너리·실제 카카오톡 화면은 확인하지 않았고, 위 테스트 통과는 현행 동작의 기준선입니다. 이 절은 구현 전 기준선입니다. 이후 구현과 검증 결과는 아래 진행 기록을 따릅니다.

## 구현 완료 (2026-10-01)

- 공통 `templateview`에 길이 임계 없는 접기 함수·표시 건수 판정·뉴스 builder를 추가하고, bot/LLM을 연결했습니다. 뉴스 카테고리 변환은 `slices.Clone`으로 원본을 보존합니다.
- 미사용 bot 행사 요약과 LLM의 단순 래퍼를 제거했습니다. 행사 날짜·링크 테스트는 실제 소유 패키지로 옮겼습니다.
- worker/shared 알림의 접기 호출·필드·인자를 제거했습니다. 예약 뉴스의 코드 대체 본문을 제거하고 렌더 실패를 기존 실패 집계로 보냅니다.
- `loadBotConfig`는 `LoadSeeMoreFold`를 재사용하며 `errors.Join`으로 다른 환경변수 오류를 함께 보고합니다.
- API 중심 패키지 테스트가 통과했습니다. 0/1/2건·길이 독립성·뉴스 추가 요약·원본 불변성·공백 행사 렌더의 미발송/미표시를 검증했습니다. 변경 패키지 린트는 0건이며, worker 최종 payload 회귀도 통과했습니다. 전체 local-ci의 NilAway·빌드·전체 테스트·race 검증도 최종 통과했습니다.
- 계획의 행사 공백 scheduler 회귀는 실제 formatter와 주간·월간 scheduler를 연결하는 `runtime/formatter_llm_scheduler_dispatch_test.go`에 구현했습니다. 실제 템플릿→formatter→scheduler 경로를 검증합니다.
- Fallback delta: 예약 뉴스 formatter 미주입·nil digest·빈 렌더 결과의 코드 대체 본문을 제거했습니다. 정상 빈 뉴스·상위 요약 생성·output guard·구독 멤버 없음 skip은 유지합니다.
- 전체 테스트에서 새로 추가한 `PreRenderedMessage` 기대값이 최종 text lane의 기존 Markdown 변환(불릿·말미 줄바꿈)을 반영하지 않아 실패했습니다. 저장 원문 불변과 `kakaoformat.Render` 이후 최종 payload를 각각 검증하도록 수정했습니다. workerapp 패키지 재실행과 린트는 통과했습니다. 제품의 전송 변환은 변경하지 않았습니다.

### 최종 검증

- kapu에서 `GOFLAGS=-p=2 RACE_TEST_PARALLEL=2 bash scripts/ci/local-ci.sh`를 실행하여 종료 코드 0과 `[LOCAL CI] Passed`를 확인했습니다. 실제 실행은 최대 1시간·메모리 상한을 둔 사용자 systemd scope 안에서 수행했습니다.
- 아키텍처 경계, 민감 로그 검사, module tidy/workspace 정합성, canonical/workspace/integration-tag vet, staticcheck, golangci-lint(0 issues), NilAway, Go build, PGO, production 빌드·설정, helper 테스트, 성능 예산, 전체 Go 테스트와 `-race -p 2 -count=1` 검사가 통과했습니다.
- `git diff --check`가 통과했습니다. Go module·lockfile·템플릿 본문·DB schema·전송 경로·저장 요청 ID는 변경하지 않았습니다.
- 별도 opt-in 통합 테스트(`RUN_INTEGRATION_TESTS=true`)는 실행하지 않았습니다. 기본 패키지 테스트의 testcontainers DB와 최종 payload 회귀는 실행했습니다. 운영 DB 사용자 override·실제 카카오톡 화면·배포 바이너리는 확인하지 않았고, 게시·배포·운영 데이터 변경도 수행하지 않았습니다.

## 적대적 리뷰 후속 보완 (2026-10-01)

- worker runbook 환경변수 표에 `BOT_SEE_MORE_FOLD`를 다시 두고, worker는 렌더에 쓰지 않지만 공통 설정 로딩(`settings.LoadConfig` → `loadBotConfig`)이 bool로 검증해 잘못된 값이면 기동에 실패한다고 명시했습니다. service 문서도 같은 내용으로 맞췄습니다. 공통 `Config`를 모든 프로세스가 읽는 기존 구조는 유지합니다.
- LLM `renderNotification`의 근거 없는 nil logger 방어 분기를 제거했습니다. 생성자가 logger를 항상 설정합니다.
- `cutSeeMoreHead`를 `strings.Cut` 기반으로 정리하고 패키지 내부에서만 쓰는 머리 문단 줄 수 상수를 비공개로 바꿨습니다. 동작은 기존 테스트로 확인합니다.
- `!라이브` 목록의 접기 판정을 `foldStreamList`로 모아, 실제 경로와 테스트 진입점이 같은 표시 건수(표시 한도 적용 후)를 쓰게 했습니다.
- `templateview_test.go`의 행사 view 검사는 `major_event_test.go`와 중복되어 삭제했습니다.
- worker 최종 payload 테스트는 500개 패딩이 아니라 접기 판정과 같은 연속 ZWSP 부재로 확인하도록 강화했고, 현재 동작과 맞지 않던 테스트 이름을 고쳤습니다.
- 검증: 세 모듈 빌드, 변경 패키지 vet, 변경 패키지 23개 테스트(`-count=1`), 수정 패키지 `-race` 테스트, 저장소 설정 golangci-lint(0 issues), 저장소 NilAway 바이너리 검사(종료 코드 0), `git diff --check`가 통과했습니다. 전체 `local-ci`와 opt-in 통합 테스트는 이번 보완 뒤 다시 실행하지 않았습니다.
