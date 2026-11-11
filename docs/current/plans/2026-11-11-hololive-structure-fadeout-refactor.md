# hololive-bot 구조·성능·계약 정리와 호환 잔재 fadeout 리팩토링안

**상태: 실행 WP 구현과 로컬 검증 완료(2026-11-11). 작업 브랜치 `refactor/hololive-structure-fadeout`, 미커밋·미배포. 승인 대기 WP-62~74와 보류 항목은 남아 있습니다.**

## 목표

- 정본 문서(`docs/current/`)가 코드와 어긋난 곳을 코드 사실에 맞춰 바로잡습니다.
- 계약 위반을 고칩니다. 예: 발송 전 검증 오류가 outcome_unknown으로 분류되는 문제, 개별 발송 전 lease 예산 미확인, 계약 밖 payload 필드 허용, scan 오류를 기본값으로 삼키는 경로.
- 호출자·생산자가 없는 구형 호환 경로, 테스트에서만 쓰는 운영 코드, 읽지 않는 설정을 지웁니다.
- 중복 구현과 간접층을 줄이고, 근거가 확인된 성능 개선(N+1 조회, DB 왕복, 중복 렌더)을 합니다.

## 제약

- 범위는 hololive-bot 저장소의 코드·문서·CI 스크립트입니다. 형제 저장소(iris-client-go, shared-go, iris-console 등)와 meta 저장소는 바꾸지 않습니다.
- 다음은 별도 승인 없이 하지 않습니다: DB migration과 운영 데이터 변경, 운영 env·Quadlet·profile 변경, 배포·재시작, 공개·내부 API 계약 파괴, 신규 런타임 의존성, toolchain 변경, 커밋·push·PR.
- 2026-10-12 fadeout에서 현행 계약으로 유지하기로 한 항목은 건드리지 않습니다: `legacy_unreviewed` metric label, `legacy_unknown` 어휘, `migrationrunner/epoch.go`의 `hasLegacyResidue`, cliproxy `ChatCompletions` 옵션, 구형 저장 형식 거절, `send_request_fallback*`.
- 새 retry·fallback·default-on-error 경로를 만들지 않습니다. 운영 동작이 바뀌는 WP는 Fallback delta를 기록합니다.
- 제품 검사를 약화하지 않고, 검사기 자체 시험·문서 텍스트 grep 가드·제거 이름 재등장 가드를 추가하지 않습니다.

## 근거

- 2026-11-11 `6b54cded1`(v15.0.0)에서 15개 영역을 분석했습니다. finding 271건 가운데 반박 검증을 통과한 262건을 74개 작업 묶음(WP)으로 묶었습니다. 61개는 바로 실행할 수 있고, 13개(WP-62~74)는 승인이나 운영 증거가 필요합니다.
- 실행 가능한 61개는 구현 전에 현재 코드에서 다시 확인했습니다. 확인 대상은 삭제 대상의 호출자·생산자 없음 주장, 인용 위치, 정본 문서와의 충돌, 승인 필요 여부입니다. 60개는 진행하고 WP-53은 보류합니다. 진행하는 WP 가운데 상당수는 범위를 줄였으며 아래 "범위 조정"에 적었습니다.
- finding ID(`ADM-4`, `YTD-8` 등)는 이 분석의 식별자입니다. 영역 접두어는 ADM(API 조립·admin), BOT(bot plane), LLM(LLM plane), YTA(API YouTube 관측), YTD(YouTube 발송), WAS(worker alarm 서비스), WEG(worker egress), SHR·SHS(shared 설정·서비스), COL(collector Go), CJS(collector youtubejs), XDOC·XOWN·XCOMPAT·XGATE(문서·소유권·호환·게이트 횡단)입니다.
- 변경 전 기준선: worktree에서 `scripts/ci/local-ci.sh`가 전 단계 통과했습니다(architecture gates, `go mod tidy -diff`, canonical vet, golangci-lint, NilAway, build, collector JS·prod, race, non-race). 통합 테스트는 실행하지 않았습니다.

## 진행 방식

- 문서 WP(01~06)와 계약 위반 WP(04, 07, 08, 09~11, 12, 13~15, 17, 29)를 먼저 하고, 구조·성능 WP를 이어서 합니다.
- 한 worktree에서 `depends_on`과 파일·패키지 겹침을 지켜 WP를 병렬로 구현합니다. WP마다 구현 뒤 읽기 전용 독립 리뷰를 거치고, 리뷰 지적은 같은 WP 안에서 고칩니다.
- WP별 검증은 해당 패키지의 `go test`·`go vet`(DB 테스트는 rootless podman), JS는 `npm test`·`npm run typecheck`입니다. 전체 검증은 `scripts/ci/local-ci.sh`, `./build-all.sh --build-only --no-bump`, 영향받는 통합 테스트입니다. 게시용 `scripts/ci/pre-push-gate.sh`는 게시 승인 뒤에 실행합니다.
- WP-55(YTA-2)와 WP-56(COL-5)은 측정 후 효과가 작으면 진행하지 않습니다.

## 실행 WP

| WP | 주제 | 제목 | 의존 | 재확인 판정 |
| --- | --- | --- | --- | --- |
| WP-01 | docs | 소유권·계약 지도 정본 정정과 CONTRACT_MANIFEST.txt 삭제 | - | proceed |
| WP-02 | docs | hololive-api 서비스 문서와 migration CONVENTIONS 정정 | - | modify |
| WP-03 | docs | alarm·worker·Iris 경계 문서와 egress lifecycle 계약 문서 정정 | - | modify |
| WP-04 | contract | ERROR_CONTRACT를 실제 오류 응답에 맞추고 common.ErrorResponse 삭제 | - | proceed |
| WP-05 | docs | YouTube 수집·관측 계약 문서 정정 | - | modify |
| WP-06 | docs | 날짜 붙은 architecture 문서의 삭제 경로 인용 정리 | - | modify |
| WP-07 | gates | security.yml 형제 모듈 checkout SHA를 go.mod 핀으로 갱신 | - | proceed |
| WP-08 | contract | 링크 차단 판정의 오류 문자열 substring 검사 제거 | - | modify |
| WP-09 | compat_fadeout | youtubedispatch 전환 잔재·죽은 상태·관대한 파서 제거 | - | modify |
| WP-10 | contract | grouped fallback의 lease 예산 규칙 구현과 미문서 실패 경로 정리 | WP-09, WP-03 | modify |
| WP-11 | contract | youtubedispatch 실패 전이를 한 트랜잭션으로 묶고 그룹 해석 중복 제거 | WP-09, WP-03 | proceed |
| WP-12 | compat_fadeout | member 패키지의 호출자 없는 다건 조회·V1 잔재 제거와 scan 오류 노출 | - | proceed |
| WP-13 | compat_fadeout | youtubejs helper의 SDK 별칭·대역 전용 분기 제거와 썸네일 추출 결함 수정 | - | proceed |
| WP-14 | contract | live-check 초기화 실패 분류와 raw player 해석 중복 정리 | WP-13 | proceed |
| WP-15 | contract | collection RPC 요청 검증과 pagination 불변식 단일화 | WP-13, WP-14, WP-05 | proceed |
| WP-16 | structure | collector lease terminal 전이·계약 집합 이중 정의 통합 | WP-05 | proceed |
| WP-17 | compat_fadeout | collector 테스트 전용 상태·중복 기본값·문자열 종료 판정 정리 | WP-16 | proceed |
| WP-18 | compat_fadeout | helper 배포물에서 테스트 대역 분리와 기동 인자 기본값 정리 | WP-15, WP-17 | proceed |
| WP-19 | compat_fadeout | bot plane 조립의 죽은 설정·확장점·도달 불가 분기 제거 | - | modify |
| WP-20 | structure | Bot→commandInitView 패스스루와 AlarmModeComponents 간접층 축소 | WP-19 | proceed |
| WP-21 | compat_fadeout | bot ResponseFormatter 렌더 오류 노출과 member news 대체 문구 경로 통일 | - | modify |
| WP-22 | performance | matcher 토큰 색인 키 결함 수정과 LLM 파서 시절 잔재 제거 | - | modify |
| WP-23 | performance | 방송 이력 조회 행 수 축소와 시험 전용 seam·중복 helper 정리 | WP-19 | modify |
| WP-24 | performance | webhook inbox head 전진 CTE 중복 제거와 명령 정산 왕복 병합 | WP-19 | modify |
| WP-25 | compat_fadeout | 항상 빈 RoomName·IsGroupChat·Timestamp 필드와 privacylog 폴백 제거 | WP-19, WP-23 | proceed |
| WP-26 | compat_fadeout | 사용하지 않는 Pretendard SemiBold 폰트와 생성 JSON 제거 | - | proceed |
| WP-27 | structure | API 조립의 퇴역 모드 설정·간접층·lifecycle 잔재 정리 | WP-01, WP-02 | proceed |
| WP-28 | structure | admin handler 조립 간접층과 readiness 선택적 API 정리 | WP-27 | proceed |
| WP-29 | contract | settings 계약 패키지를 admin plane으로 옮기고 shared settings.Service 정리 | WP-28, WP-01 | proceed |
| WP-30 | compat_fadeout | migrationrunner의 중복 epoch baseline 검증 제거 | - | proceed |
| WP-31 | performance | ACL RemoveRoom 전체 재적재 제거와 dispatchops SQL projection 중복 정리 | - | proceed |
| WP-32 | structure | OpenAI 호환 클라이언트 두 벌 통합과 provider 기본값 정규화 단일화 | WP-27 | proceed |
| WP-33 | structure | consensus 검토 파이프라인 단일화와 major event fallback 잔재 타입 제거 | - | modify |
| WP-34 | structure | 신뢰 출처·X 계정 파싱 정책 단일화와 Exa client 소유 정리 | WP-33 | proceed |
| WP-35 | structure | 주간·월간 scheduler 중복과 subscription 단일 구현 인터페이스 정리 | WP-01 | modify |
| WP-36 | compat_fadeout | API live check 소비 경로의 구세대 수명 분기와 시험 전용 API 제거 | WP-05 | proceed |
| WP-37 | compat_fadeout | reader 없는 subject head 갱신 제거와 UTF-8 절단 구현 통합 | WP-36 | modify |
| WP-38 | compat_fadeout | dispatchoutbox 호출자 없는 API와 구형 inline payload 분기 제거 | - | proceed |
| WP-39 | compat_fadeout | 빈 구독 marker 죽은 경로와 구독 저장·이름 캐시 이중 경로 정리 | WP-03 | proceed |
| WP-40 | performance | checker·tier·dedup의 N+1 조회와 효과 없는 분기·시험 전용 코드 정리 | - | modify |
| WP-41 | structure | Iris handoff reconciler 3중 구현 통합과 handoff 상태 관용 처리 정리 | WP-03 | proceed |
| WP-42 | compat_fadeout | alarm dispatch 소비 측 도달 불가 재검사·nil 분기·이중 clamp 제거 | WP-38, WP-03 | modify |
| WP-43 | structure | alarm dispatch wakeup을 공용 Waiter 구현으로 통합 | WP-42, WP-38, WP-03 | proceed |
| WP-44 | structure | 발송 오류→attempt outcome·409 재발급 판정 단일화 | WP-42, WP-43, WP-10 | proceed |
| WP-45 | compat_fadeout | shared settings의 죽은 설정·검증과 grep 가드 테스트 제거 | WP-19 | modify |
| WP-46 | structure | workerapp의 직접 env 읽기와 원장 SQL·gauge 소유 정리 | WP-43, WP-45, WP-27, WP-42 | proceed |
| WP-47 | compat_fadeout | shared의 호출자 없는 export·옵션·잔재 규칙 제거 | - | modify |
| WP-48 | compat_fadeout | Holodex provider의 호출자 없는 GetChannel 경로와 apiclient 키 회전 잔재 제거 | WP-02, WP-27 | proceed |
| WP-49 | structure | API 전용 설정 구획·상수를 hololive-api 내부로 이전 | WP-45, WP-27, WP-28, WP-32, WP-34, WP-35, WP-19, WP-21, WP-01 | proceed |
| WP-50 | structure | PhotoSync 등 admin 단일 runtime 전용 Holodex 구현을 API admin plane으로 이전 | WP-49, WP-48, WP-46, WP-12, WP-42, WP-01 | modify |
| WP-51 | compat_fadeout | trustedlink의 생산자 없는 Twitch·치지직 prefix와 결합 분기 제거 | WP-02, WP-48 | modify |
| WP-52 | performance | 발송 렌더의 템플릿 resolve 중복과 표시명 색인 재생성 제거 | WP-09, WP-10, WP-42, WP-44, WP-51 | modify |
| WP-54 | structure | alarm dispatch 큐 envelope·검증 로직을 worker internal로 이전 | WP-47, WP-29, WP-38 | proceed |
| WP-55 | performance | consume 1건당 payload 이중 canonical화·해시·디코드 축소 | WP-36, WP-37 | proceed |
| WP-56 | performance | collector job당 계약 세대·target snapshot 조회 왕복 묶기 | WP-17 | modify |
| WP-57 | gates | 도구 버전 핀 단일화와 보안 스캔·빌드 스크립트 잔재 정리 | WP-07 | modify |
| WP-58 | gates | local-ci·pre-push 단계 정리와 topology 검사 소비처 보강 | WP-57, WP-07 | modify |
| WP-59 | gates | SQL 안전 게이트의 COUNT(*) 금지 규칙 제거 | WP-58 | modify |
| WP-60 | gates | migration manifest 검사의 Python 재구현을 dbmigrate 파서로 교체 | WP-30 | modify |
| WP-61 | gates | CI 경계 문서와 게이트 주석을 현재 동작에 맞춤 | WP-06, WP-57, WP-58 | proceed |

## 범위 조정

재확인에서 줄이거나 다른 WP로 넘긴 부분입니다. 적지 않은 WP는 분석 때 범위 그대로 진행합니다.

- WP-01: 구형 bridge 문서 6개를 모두 지우고 `contracts/trigger.md`의 MANIFEST 참조도 정리합니다. `iris-boundary.md`·`contracts/alarm.md`·`services/alarm-worker.md`는 WP-03, `services/hololive-api.md`는 WP-02, deploy/podman을 topology 검사 대상에 넣는 일은 WP-58이 맡습니다.
- WP-02: BOT-3은 `send_failed` 정의 정정만 합니다(90일 지표 판정은 WP-73). ADM-2는 CONVENTIONS에 `transition.go`·`epoch2_head.sha256`의 소유자와 제거 조건만 적고, 기한과 epoch-2 복원 경로는 WP-63에 남깁니다.
- WP-03: 빈 구독 marker(WAS-1)는 현재 코드 사실만 적고, 동작 서술 변경은 코드를 지우는 WP-39에서 함께 합니다. `contracts/alarm.md`에 `/count` 경로와 `alarm_advance_update_failed`·`count_alarm_entries_failed` 오류 코드를 추가합니다.
- WP-04: `contracts/common` 패키지는 `APIKeyHeader` 때문에 남기고 `errors.go`·`errors_test.go`만 지웁니다. ERROR_CONTRACT에는 middleware 코드 다섯 개(401·403·503·404·500)를 적습니다.
- WP-05: 단일 알림과 celebration은 기존 링크를 유지한다는 서술을 남깁니다. BOT-5는 bot 표시 보정이 `legacy_unknown`에 기대는 사실과 데이터 기준 제거 조건만 기록합니다.
- WP-06: `scripts/ci/test-infra-images.sh`는 고칠 인용이 없어 제외합니다.
- WP-08: 오류 문자열 substring 판정 한 줄만 지웁니다. 차단 sentinel은 이미 `errors.Is`로 판정합니다.
- WP-09: YTD-8은 계약 밖 `url` 대체 키 제거와 계약 타입 디코드 통일만 합니다. 검증에 실패한 행을 개별 발송으로 보내는 라우팅은 유지합니다.
- WP-10: YTD-1은 개별 발송 전 잔여 lease 예산 확인만 합니다. `Config.Validate`의 관계식 추가는 운영 profile 확인 뒤로 미룹니다. YTD-20은 발송 전 검증 오류를 known-not-delivered로 분류하되 grouped fallback의 trigger로 쓰지 않고, 구형 route 거절은 유지합니다.
- WP-14: CJS-4는 상수·패턴 공유와 소비자 없는 raw live metadata 출력 필드 제거만 합니다.
- WP-19: BOT-16은 `apperrors` 패키지, Iris 오류 분기, Alarm nil 분기만 지웁니다(메시지 키와 `message_strings`는 WP-62). BOT-20은 `stopCh`·`doneCh` 삭제, 로그 문구, 지연 생성 정리만 하고, degraded 기동을 기동 실패로 바꾸지 않습니다.
- WP-21: `render()`의 공통 nil guard는 남기고 메서드별 중복 nil 분기만 지웁니다.
- WP-22: 항상 빗나가는 `tokenIndex`를 지웁니다. 매칭 결과는 바뀌지 않습니다. 실제 부분 토큰 색인 신설은 제품 결정이라 하지 않습니다.
- WP-23: 방송 이력 조회 행 수 축소는 Type·TopicID가 빈 조회에만 적용합니다. 정수 파서는 handlers 패키지 안에서만 합치고 `getStringParam`은 제외합니다.
- WP-24: ordering key 조회와 advisory lock을 한 문장으로 합칩니다. command·inbox 정산을 한 트랜잭션으로 합치는 일은 제외합니다.
- WP-25: bot 내부 필드와 privacylog 폴백만 지웁니다. 구독 SQL과 구독 시그니처의 roomName 제거는 계약 변경이라 WP-67에 남깁니다.
- WP-32: provider 기본값 정규화 중복만 정리합니다. 모델명 substring으로 temperature를 빼는 분기는 WP-66에 남깁니다.
- WP-33: 검토 스키마와 설정 정규화 중복만 정리합니다. 단계 실행기 통합은 degraded 경로 결정(WP-66) 뒤로 미룹니다.
- WP-34: Exa 빈 `results` 응답 파싱 결함만 고칩니다. 인증 헤더 이중 설정과 degraded 경로 존폐는 WP-66에 남깁니다.
- WP-35: 월간 scheduler의 clock 이중 읽기만 고칩니다. 주간·월간 scheduler 공용화는 하지 않습니다.
- WP-37: `boundedErrorDetail`만 공유합니다. 상한과 소유가 다른 `boundedVideoTitle`은 유지합니다.
- WP-38·39·41·42: 같은 finding(WAS-20, WEG-2, WEG-14)을 파일 기준으로 나눴습니다. claim SQL·스캔과 `contracts/alarm.md:9`는 WP-38, 구독 조회 메서드는 WP-39, handoff 주석은 WP-41, egress 쪽 정리는 WP-42가 맡습니다. WP-42는 `ALARM_DISPATCH_RETENTION_LIMIT` 초과를 기동 오류로 바꾸지 않습니다.
- WP-39: 파일 목록에 `contracts/alarm.md`를 추가합니다.
- WP-40: 방 단위 dedup 묶음 조회는 현재 checker 오류 정책(오류 난 방 skip)을 유지합니다. 구형 저장 형식 거절 테스트는 지우지 않고 운영 메서드 기준으로 옮깁니다. 빈 제목 대체 문구는 WP-69에 남깁니다.
- WP-45: shared `ValidateIrisEgressInputs`의 webhook token 필수 검사는 bot plane이 쓰므로 유지하고 worker 전용 검증만 분리합니다. 전송 별칭을 h3 하나로 줄이는 일은 운영 env 확인이 필요해 주석만 고칩니다. MajorEvent·LLM timeout 설정의 모듈 이전은 하지 않습니다.
- WP-47: `messagestrings.GetMap`, `cache.Config`의 테스트 연결 옵션, `CacheFieldAttr`·`KeyCacheField`는 사용처가 있어 남깁니다.
- WP-50: PhotoSync 이전만 하고 재시도(3회)는 유지합니다.
- WP-51: YouTube·치지직 결합 분기와 그 전용 상수만 지웁니다. Twitch·치지직 단일 prefix 삭제는 보류합니다.
- WP-52: 사전 렌더 대상을 활성 행으로 좁히고, 표시명 memo와 `RenderBatch` 전환만 합니다. 사전 렌더 구조 재편은 하지 않습니다.
- WP-56: 계약 세대 조회와 target snapshot 조회는 의존 관계라 `SendBatch` 한 번으로 합칠 수 없습니다. snapshot 트랜잭션 안의 projection guard 잠금과 lease membership 판정만 한 batch로 묶습니다. 측정 후 이득이 작으면 진행하지 않습니다.
- WP-57: NilAway·govulncheck·Trivy 판본 핀 중복만 정리합니다. `scripts/ci/nilaway-models/`는 meta `tools/nilaway` 정본의 사본이라 바꾸지 않고(XGATE-13 보류), Node 판본 단일화와 final image manifest 형식 단순화도 하지 않습니다.
- WP-58: changed 모드에서 단계를 건너뛰는 변경은 제품 검사 축소라 하지 않습니다. PostgreSQL 한 번 기동 공유, 설정자 없는 변수·호출자 없는 분기 정리, topology 검사 대상 보강만 합니다. 스택 공통 `python-runner.sh`는 바꾸지 않습니다.
- WP-59: COUNT(*) 패턴과 그 루프만 지웁니다. SQL 되돌리기와 문서 수정은 하지 않습니다.
- WP-60: manifest 일치 검사를 `migrations.FS`를 쓰는 DB 없는 Go 시험 하나로 옮깁니다. 규칙별 위반 fixture 시험은 커밋하지 않습니다.

## 보류·승인 대기

### 재확인에서 보류한 항목

- WP-53(SHR-15, targetpolicy 진입점 단일화): worker 구독 lifecycle이 생성과 advance 갱신에서 의미가 다른 진입점을 의도적으로 씁니다. 결과를 바꾸지 않고 진입점을 하나로 줄일 수 없으므로 제품 결정이 필요합니다. 운영 `settings.json`의 목록 형태도 확인하지 못했습니다.
- XGATE-13(nilaway-models 해시 재고정 제거): meta `tools/nilaway` 정본과 다른 사본을 함께 바꿔야 해서 이 저장소 범위를 넘습니다.

### 승인·운영 증거가 필요한 WP

| WP | 내용 | 필요한 승인·증거 |
| --- | --- | --- |
| WP-62 | message_strings 고아 키와 bot 원장 열·인덱스 정리 | 공유 DB migration 승인, 운영 인덱스 사용 통계 확인 |
| WP-63 | 일회성 이행 잔재(epoch-2 전환, legacy_unknown 보정, absence hint 등) 제거 | 운영 DB 읽기 전용 확인, data migration·배포 승인 |
| WP-64 | 방송 이력의 worker 원장 대체 읽기와 reply outbox 이미지 저장 형식 | 운영 DB 집계, migration과 payload 형식 변경 승인 |
| WP-65 | YouTube 관측·발송 원장 SQL·인덱스·보존 | 운영 데이터와 EXPLAIN, migration·계약 변경 승인 |
| WP-66 | major event·member news 제품 결정 항목 | 운영 DB·로그·env 확인, 제품 결정, 외부 LLM 비용 승인 |
| WP-67 | 내부·공개 API 호환 envelope·alias·route 퇴역 | 내부 alarm.http·공개 admin API 계약 변경 승인, iris-console 호출자 전환 확인 |
| WP-68 | collector·helper 계약 결정 항목 | 계약 결정과 운영 데이터 확인 |
| WP-69 | worker 원장 구형 행 fallback·보존 처리량·CHECK 축소 | 운영 DB 읽기, migration·profile 배포 승인 |
| WP-70 | 공식 일정 fallback·htmlscraper·Holodex 재시도 scheduler 존폐 | 90일 운영 지표, 배포 승인 |
| WP-71 | 운영 env·profile 확인이 필요한 설정 호환 정리 | 호스트 env 키·형식 확인, 배포 승인 |
| WP-72 | 문서 동기화·manifest 게이트 퇴역과 보안 스캔 근본 조치 | release runbook 계약 확인, 대체 수단 결정 |
| WP-73 | 90일 검토 조건 예외 경로 판정 | 2026-12-31 이후 운영 지표 |
| WP-74 | admin plane의 alarm-worker 발송 원장 직접 접근 소유권 | 내부 계약 확장 또는 소유권 문서 변경 승인 |

## 검증 결과

2026-11-11 kapu의 작업 worktree에서 실행한 결과입니다.

- `./build-all.sh --build-only --no-bump`를 `RUN_INTEGRATION_TESTS=true`로 실행해 통과했습니다. 이 실행에는 `scripts/ci/local-ci.sh` 전 단계가 들어 있습니다: architecture gates, `go mod tidy -diff`, canonical vet, integration-tag vet, golangci-lint, NilAway, build, collector JS·production 검사, X Spaces helper, race·non-race 테스트, PostgreSQL·Valkey를 쓰는 통합 테스트. 그 뒤 운영 이미지 7종 빌드도 성공했습니다.
- `npm test`(433건)와 `npm run typecheck`를 `hololive/hololive-youtube-collector/youtubejs`에서 실행해 통과했습니다.
- 여러 WP가 겹쳐 고친 결과를 영역 다섯 곳(API bot·조립, API LLM·admin·YouTube, alarm-worker, shared·collector, 문서·스크립트)으로 나눠 읽기 전용으로 다시 리뷰했고, 남은 지적은 없습니다.
- collector의 `TestHLP012`(child-exit, no-child)는 `TMPDIR`가 28바이트 이상이면 socket 경로가 100바이트 상한을 넘어 실패했습니다. 같은 실패가 변경 전 `6b54cded1` 사본에서도 재현되는 기존 시험 결함이었습니다. 위 `build-all.sh` 검증은 `TMPDIR=/home/kapu/.cache/tmpdir`로 실행했습니다. 잔존 이슈 처리에서 시험이 짧은 runtime base를 쓰게 고쳤고, 그 뒤에는 기본 `TMPDIR=/home/kapu/.cache-ssd/tmpdir`에서도 통과합니다.
- 잔존 이슈 처리 뒤 기본 `TMPDIR`로 `RUN_INTEGRATION_TESTS=true scripts/ci/local-ci.sh`를 다시 실행해 전 단계와 통합 테스트가 통과했습니다.
- `scripts/ci/pre-push-gate.sh`는 게시 승인 전이라 실행하지 않았습니다. 미커밋 상태에서는 작업 트리에서만 지운 youtubejs 파일이 index에 남아 있어 `image-runtime-tree-permissions_test.sh`가 실패합니다. 이 실패는 커밋하면 사라집니다.

## 진행 기록

- 2026-11-11: 분석과 재확인을 마치고 이 계획을 작성했습니다.
- 2026-11-11: 실행 WP 60개를 세 묶음으로 구현했습니다. 59개는 변경을 반영했고, WP-55는 측정 결과에 따라 변경하지 않았습니다. 모든 WP가 구현 뒤 독립 리뷰를 거쳤고 남은 high·medium 지적은 없습니다.
  - WP-42는 처음에 일부만 반영했습니다. `alarm_dispatch_metrics.go`의 전역 metric nil 분기를 지우면 NilAway가 `sync.Once` 초기화를 추적하지 못해 지적 17건이 나왔기 때문입니다. 잔존 이슈 처리에서 metric 묶음을 `sync.OnceValue` 구조체 하나로 바꿔 nil 분기를 지웠습니다. metric 이름·label·bucket은 그대로입니다.
  - WP-55는 `BenchmarkPublishConsumeCommunityObservation` CPU 프로파일로 측정했습니다. 줄일 수 있는 kind별 재디코드는 consume 1건(약 8~10ms)당 1~10µs로 0.1% 이하였습니다. typed payload API를 새로 추가하는 비용에 비해 이득이 작아 진행하지 않았습니다.
  - WP-56은 격리 PostgreSQL에서 왕복 수를 쟀습니다. target snapshot 조회가 호출당 5왕복에서 4왕복으로 줄었습니다. 원격 AP collector는 중앙 DB에 WAN으로 붙으므로, 줄어든 왕복 하나만큼 guard share 잠금과 연결을 붙잡는 시간도 줄어듭니다. AP collector와 중앙 DB 사이의 실제 RTT는 확인하지 못했습니다.
  - 구현 중 바뀐 운영 동작은 `CHANGELOG.md`의 `미출시` 항목에 적었습니다.
- 2026-11-11: 구현 기록과 리뷰에 남은 잔존 이슈를 다시 분류했습니다. 승인 없이 고칠 수 있는 40건(시험 결함·공백 보강, 도달하지 않는 대체 분기와 중복 interface 정리, 정본 문서 drift 정정)은 반영하고 리뷰를 거쳤습니다. migration·운영 데이터·운영 env 확인·계약 변경·제품 결정이 필요한 14건은 고치지 않았습니다. lease 예산 관계식 검증, `invalid_send_request` FAILED 행의 revive 차단, 운영 Valkey의 `alarm:member_names` 정리, Valkey ACL 확인, govulncheck가 보고한 Go 1.27.3 표준 라이브러리 취약점 등이 여기에 들어가며, 위 승인 대기 표와 함께 승인 뒤 처리합니다.
