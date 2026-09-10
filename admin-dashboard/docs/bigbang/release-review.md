# 관리자 교체 출시 전 적대적 리뷰

2026-09-10. `DEC-20260909-hololive-admin-bigbang-replacement`, `PLN-20260909-hololive-admin-bigbang-replacement` T10/T11의 리뷰 기록입니다. 코드와 미추적 신규 파일을 포함한 작업 트리를 검토했습니다. 운영 반영 결과와 최종 artifact 검증은 [출시 실행 기록](release-progress.md)에서 관리합니다.

## 범위와 작성자

부모 구현 담당자는 프런트의 generation gate, transport, cookie lock, 인증 세대·탭 사건, 업무 변경·unknown 결과, query·draft·WS 수명, 정비·session purge·no-build 전환 경계를 검토했습니다. 기존에 사용자가 승인한 읽기 전용 보조 검토자 `admin_performance_review`는 백엔드의 인증·세션·claim·adapter·JSON·관측·종료 경계를 독립적으로 읽고 보고했습니다. 보조 검토자는 파일·Git·운영 상태를 변경하거나 build/test/load를 실행하지 않았습니다. 독립 AI 코드 검토와 부모의 실행 검증을 구분하며 이를 독립 인적 인수로 표시하지 않습니다.

기준 HEAD는 `2323f98c8446f42de581c6f8d2e67e408e3d92df`입니다. 이전 최적화 후보 `b9e9210a…`에 아래 I/O 수정이 추가됐으므로, 이전 이미지의 성공을 새로운 이미지의 검사 완료로 표시하지 않습니다.

## P2 · 투영 응답의 읽기 오류 원인 유실: 수정

멤버·알람 custom decoder가 읽기 오류를 감싸면 Go 1.27.1의 `json/v2`는 이를 `SemanticError`로 바꿉니다. 상위 client의 JSON 원문 비노출 처리에서 실제 취소·I/O 원인까지 유실됐습니다. `PeekKind`의 오류 상태를 타입 불일치로 처리한 경로도 있었습니다. 별도로 Go의 custom decoder 진입 전 `AtEOF`와 데이터가 함께 온 read 처리에서 일회성 I/O 오류를 소비하는 경우를 공식 로컬 toolchain 소스와 진단으로 확인했습니다.

`adapters/holo/client.go`의 응답 reader가 첫 non-EOF 오류를 보존합니다. `n>0`과 함께 반환된 오류도 저장하고, 기존 크기 상한 판정 뒤 JSON 오류 정제 전에 이를 반환합니다. body close 오류의 결합과 64 KiB drain 상한은 유지합니다. 투영 decoder는 원래 decoder 오류를 직접 반환하고, 문자열·boolean은 실제 읽기 성공 뒤 타입을 검사합니다. 성공 본문의 UTF-8·중복 이름·필수 값·ID 범위·HTML escape와 독립 행 소유권은 유지합니다.

`TestProjectedResponsesPreserveReadFailuresAtEveryBoundary`는 두 합성 응답의 모든 바이트 절단점에서 일회 오류 후 EOF, 데이터와 오류의 동시 반환, close 성공·실패를 교차 검사합니다. drain에서 같은 오류가 다시 발생하여 결함을 가리지 않게 구성했습니다. 수정 전에는 두 응답 모두 첫 경계에서 원인을 잃어 실패했고, 수정 후 Holo adapter 패키지 전체 검사가 통과했습니다. 최종 publish gate는 별도로 기록합니다.

보조 검토자는 수정 소스와 부모 실행의 전후 로그를 다시 읽어 위 수정 경계를 확인했습니다. 이 결함은 진단·오류 계약 위반이며 인증 우회나 외부 효과의 재실행으로 분류하지 않습니다.

## 확인한 보존 경계

- 세대 확인→admission→인증→CSRF→감사/원자적 claim→외부 전송 순서, family 유실 후 인증·claim 거부, 회전 중 claim 유지, 취소 후 logout 정리를 확인했습니다.
- WS 세대·Origin·family별 4개/전체 16개 제한, 폐기 감시·종료 회수, Holo/Docker 전송 전 마킹·redirect 금지, Docker의 정확한 이름·동작 허용 정책을 확인했습니다.
- JSON 행과 출력 buffer의 소유권·상한, 전체 응답 준비 전 부분 출력 금지, 프런트의 전송 전 validator 준비·인증 세대 확인, 자동 업무 재시도와 offline 대기 금지를 확인했습니다.
- 탭 사이 cookie 쓰기 순서와 늦은 응답 차단, 변경 결과의 confirmed/partial/unknown 구분, 조회 실패 중 초안 보존과 외부 값 충돌 표시, 인증 경계의 민감 상태 정리를 확인했습니다.
- 정비 503만으로 기존 연결 차단을 주장하지 않고 구형 BFF의 실제 종료를 확인하는 순서, 관리자 session prefix로 한정된 purge, 새 signing secret과 구형 전체 artifact를 사용하는 rollback 절차를 확인했습니다.

검토 범위에서 미해결 P0/P1 또는 추가 출시 차단 결함은 확인하지 못했습니다. 실행하지 않은 검사를 통과로 표시하지 않으며, RSS 회복 실패 수용을 누수 부재의 증거로 사용하지 않습니다.

## 발행 파일의 secret 점검

전용 scanner가 설치돼 있지 않아 현재 발행 대상의 존재하는 423개 파일을 수동 패턴 분석했습니다. 압축된 증거도 메모리에서 풀어 확인했고 과거 Git 이력은 검사 범위에 넣지 않았습니다. provider credential·private key 패턴은 없었으며, URL/자격증명 관련 38개 일치는 합성 테스트·mock·문서 placeholder·파일 경로·전환 결과 라벨임을 주변 사용처에서 확인했습니다. 실제 자격증명 노출은 발견하지 못했습니다.

`.gitignore`는 env·key·PEM을 제외하고 정적 secret 소비 절차가 있습니다. 현재 pre-commit은 Go 포맷 검사이며 별도 secret scanner는 아니고, 확인한 security workflow에도 전용 secret scan은 없습니다. 이번 변경에 새 scanner 의존성이나 광범위한 CI 정책 변경을 추가하지 않았습니다.

## 발행 guard의 확인된 오탐

`check-crosscutting-guardrails.py`는 같은 파일에 고정된 recovery 함수 이름이 있는지 문자열로 검사합니다. `httpapi/routes.go`는 `recoverPanics()`를 실제 미들웨어로 등록하지만 그 이름이 목록에 없어 차단됐습니다. 이 생성 한 줄에만 `crosscutting:allow`와 원인을 기록했습니다. `TestPanicResponseDoesNotDumpSecrets`가 등록된 라우터에서 panic의 500 JSON 응답과 panic·쿠키 값 비노출을 검증하며, 이 시험과 crosscutting 검사가 통과했습니다. 실제 미들웨어나 검사를 제거하거나 전역 제외 목록을 넓히지 않았습니다.

폐기 이름 검사에서는 과거 native artifact 4개의 불변 입력 목록이 탐지됐습니다. 각 파일의 일치 4개가 `source_manifest.files[].path`의 기존 폐기 검증/provider 파일 경로뿐임을 JSON 구조로 확인했습니다. 원본과 SHA를 바꾸지 않고 기존 retirement 정책의 이력 자료 범주에 정확한 파일 4개를 등록했으며, 전체 retirement 검사가 통과했습니다. 실행 소스나 디렉터리 전체를 제외하지 않았습니다.

## 게시 검사에서 확인한 구조 상한

M4 검사가 JSON 투영·HTML 이스케이프·proxy 설정 검증의 네 함수에서 복잡도 상한 16 초과를 확인했습니다. 목록 필드 해석과 proxy 설정 검증을 각 소유 함수로 나누고, 이스케이프의 세 ASCII 문자를 같은 6바이트 Unicode 표기로 작성하며, 불필요한 오류 분기를 제거했습니다. 허용 입력·출력·오류 순서와 원인은 유지합니다. 구조 hard 검사에서 상한 위반 0건이며 Holo adapter·config 전체 테스트와 해당 패키지 lint가 통과했습니다. 스트림 회귀 테스트의 공백 규칙도 함께 정리했습니다.

읽기 전용 보조 검토자는 이 네 파일의 후속 diff도 검토하여 출력 escape, decoder 오류, 필수 필드 표시, proxy 검증 순서에서 확인된 회귀나 출시 차단 결함이 없다고 보고했습니다. 부모가 실행한 검사와 보조 검토자의 소스 검토를 구분합니다.

## 브라우저 테스트의 자원 소유권과 상태 순서

같은 소스의 전체 검사는 통과했지만, 다음 발행 검사에서 Chromium 초기화가 10초 뒤 한 번 실패했습니다. 단독 Chromium 및 차가운 캐시의 두 테스트 병렬 재현은 통과하여 원인을 확정하지 못했습니다. 별도로 설치된 Vite 8.2.2가 plugin 이름을 설정 hash에 넣고, hash 불일치 시 공유 `deps`를 삭제·교체하는 경로를 확인했습니다. BaseModal과 session 테스트는 다른 fixture plugin으로 같은 기본 캐시를 사용했습니다.

BaseModal이 이미 소유하고 종료 시 정리하는 임시 profile 안에 전용 `cacheDir`를 두었습니다. 변경 후 차가운 캐시에서 두 테스트의 병렬 검사가 통과했습니다. timeout이나 assertion을 완화하지 않았고, 이 격리 개선만으로 최초 timeout 원인이 입증됐다고 표시하지 않습니다. 임시 진단 파일은 발행 소스에서 제거합니다.

이후 게시 경로에서 Chromium 준비 timeout이 다시 발생해 실패 시 문서·계약 준비 상태와 최근 15개 자원의 경로·응답 상태를 수집하고, 요청 실패·HTTP 오류는 최대 16개까지 기록했습니다. 요청 본문·쿠키·자격증명은 수집하지 않고 준비 대기 종료 시 관측 listener를 해제합니다. 실제 실패는 visible/complete 문서에서 앱 모듈 요청이 `net::ERR_NETWORK_CHANGED`로 중단되어 `window.contract`가 생성되지 않은 상태였습니다.

같은 실행의 Docker 수명 사건을 대조하니 앞선 YouTube 벤치마크의 PostgreSQL과 Ryuk 회수가 Chromium 초기 모듈 실패와 겹쳤습니다. 기존 dbtest는 프로세스 종료 뒤 Ryuk가 비동기로 정리하는 계약입니다. 벤치마크 소유 세션만 정리될 때까지 기다리는 진단은 후속 프런트 175건을 통과했지만, 전체 Go·race 검사 뒤에는 같은 오류가 재현됐습니다. 전체 실행의 제한된 잔여 event만으로 최초 실패와 일치하는 컨테이너를 다시 특정하지는 못했습니다.

최종 수정은 `pre-push-gate.sh`의 독립적인 frontend 품질 검사를 `local-ci.sh`보다 먼저 완료하도록 순서를 바꿉니다. 같은 게시 실행의 Go·컨테이너 시험이 브라우저 검사와 겹치지 않으며, 기존 reusable/freshness/ambient 책임과 검사 조건·내용은 유지합니다. 벤치마크에 임시로 추가했던 정리 대기와 자기 검사는 제거하여 SDK·dbtest의 세션 및 종료 계약을 유지합니다. 앱 동작, 벤치마크 횟수·예산, 브라우저 timeout·assertion도 바꾸지 않습니다.

기존 게시 프로필 자기 검사에 frontend build 완료 후 Go 시작, frontend 실패의 gate 실패 및 Go 미실행을 추가했고 전체 프로필 검사가 통과했습니다. 실제 게시 순서의 최종 결과는 출시 실행 기록에 남깁니다.

보조 검토자는 최종 순서 변경·프로필 검사·benchmark 원래 계약 복원을 읽어 확인된 결함이나 필수 검사 유실이 없다고 보고했습니다. 재배열한 전체 게이트의 실행은 부모가 담당합니다.

WebKit의 달력 조회 시나리오에서는 route 전환 직후 전역의 동일한 오류 문구가 이전 화면에서 충족될 수 있었습니다. 그 상태에서 새 query 등록 전 refetch하면 새 화면의 최초 오류가 남습니다. 각 feature의 고유한 QueryNotice label로 오류 영역을 한정하여 해당 화면의 실패가 확인된 뒤 재조회합니다. 달력 날짜 값은 실패 trace에서도 `2026년 9월`이어서 날짜 파싱 실패로 분류하지 않았습니다. 이 후속 테스트 수정의 검토와 실행 검증은 부모가 담당합니다.

수정 후 세 엔진의 세션·세대·읽기/편집 브라우저 검사가 모두 통과했고 skip은 0건입니다. 게시 경로의 최종 결과는 별도로 확인합니다.

첫 PR 검사에서는 WebKit의 재연결 후 빈 달력 확인이 실패했습니다. 오류 상태를 유지한 채 네트워크를 먼저 열어 TanStack Query의 자동 재조회를 시작한 뒤에야 fixture 응답을 빈 목록으로 바꾸는 순서였습니다. 설치된 query-core의 `onOnline()` 재조회 경로와 PR의 이전 데이터·오류 표시를 확인하고, 빈 응답을 먼저 준비한 뒤 네트워크를 여는 순서로 수정했습니다. 앱의 재연결 동작·오류 처리와 검사 대기 상한은 유지합니다. 수정 후 WebKit의 해당 전체 세션 시나리오는 통과했고 skip은 0건입니다.

후속 PR 실행 `34463967832`에서는 Chromium 55.6초, Firefox 60.3초가 통과한 뒤 WebKit이 엔진 전체의 65초 제한에 걸렸습니다. 11개 시나리오를 한 테스트에 모은 전체 실행 상한을 엔진별 120초로 조정하고, 세 엔진과 정리를 감싸는 cgroup 375초·자식 대기 390초·부모 테스트 405초를 순서에 맞춰 설정했습니다. 각 시나리오의 시작과 완료 시간을 남겨 이후 지연 위치를 확인할 수 있게 했습니다. assertion, 준비 대기 10초, 엔진·시나리오 수, 순서, 재시도 정책은 유지합니다. RSS 성능 판정이나 운영 timeout은 변경하지 않습니다.

Fallback delta: none. 읽기 오류 보존 수정에 retry·대체 경로·강제 GC는 추가하지 않았습니다. 기존 정비 개방 실패의 1회 보상 동작은 [전환 기록](cutover-progress.md)에 별도로 유지합니다.
