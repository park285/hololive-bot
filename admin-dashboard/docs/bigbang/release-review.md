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

Fallback delta: none. 읽기 오류 보존 수정에 retry·대체 경로·강제 GC는 추가하지 않았습니다. 기존 정비 개방 실패의 1회 보상 동작은 [전환 기록](cutover-progress.md)에 별도로 유지합니다.
