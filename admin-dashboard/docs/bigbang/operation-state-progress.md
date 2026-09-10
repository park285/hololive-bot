# T06 작업·조회·편집 상태 진행 기록

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. **T06 구현·집중 검증 완료**입니다. 전체 기능 이관이나 출시 게이트 완료 기록이 아닙니다.

`operations/controller.ts`가 generated inventory의 16개 업무 변경을 식별하고 앱 수명 동안 하나만 실행합니다. BUSY는 대기열 없이 거부하며 offline 제출과 실제 전송 직전 offline/authGeneration 변경은 I/O 전에 거부합니다. 인증·조회는 이 업무 잠금을 사용하지 않습니다. 전송 후 실패는 unknown으로 기록하며 앱이 자동 재전송하지 않습니다. 인증 경계가 바뀌어도 진행 중 요청의 잠금을 먼저 풀지 않고 민감한 이름·결과만 숨깁니다. 실제 브라우저의 네트워크 계층 재전송은 별도로 재현되어 [C02](mutation-replay-evidence.md)의 BFF family claim으로 차단합니다. 앱 호출 1회만으로 효과 중복 방지를 주장하지 않습니다.

`operations/outcome.ts`는 succeeded/partial/accepted/rejected/failed/unknown과 효과 상태를 정의합니다. 현재 소유 API에는 accepted 응답이 없으므로 202나 임의 trackingId를 완료/접수 근거로 삼지 않습니다. Settings는 저장·runtime 적용·publish를 각각 보존하고 누락한 flag를 false로 만들지 않습니다. 확인된 사전 거부 코드 외 upstream 거부는 효과를 보장하지 않아 unknown으로 남깁니다. requestId는 상관 ID이며 receipt로 사용하지 않습니다. 오류 객체별 기록을 WeakMap에 연결하여 UI가 다른 요청의 마지막 결과를 읽지 않게 합니다.

`useBusinessMutation`은 제출 시 인증 세대를 고정하며 retry=false·networkMode=always를 소유합니다. scope queue나 caller override를 받지 않습니다. 현재 업무 mutation hook들은 이 경계를 사용합니다. 로그인은 별도 native useMutation 경로를 유지합니다. `OperationNotice`는 AppLayout 안에서 화면 이동 뒤에도 마지막 결과를 표시합니다.

`queries/state.ts`는 pending/paused/offline/error/empty/success와 이전 값의 현재성을 분리합니다. 오류의 빈 배열과 data 없는 success를 정상 empty로 바꾸지 않습니다. `editors/*`는 base/latest/draft를 분리하여 작성 중 외부 변경을 자동 덮어쓰지 않고 사용자의 선택을 요구합니다. Settings는 조회 실패/오프라인/미확인 외부 변경에서 저장을 막습니다. 읽기 0~1440을 보존하고 편집 UX 1~60을 유지합니다. 이 상태 비교는 atomic CAS가 아닙니다.

집중 시험 116/116과 설정 화면의 실제 Chromium 시험이 통과했습니다. 설정 시험은 잘못된 최초 응답·오래된 값·편집 중 외부 변경·초안 선택·저장 후 조회 실패·부분 적용·거부·실패 후 초안 보존을 실행했습니다. 사용자 화면의 오류 확인 방식에 맞춰 이전 소스 문자열 의존을 실행형 검사로 이관합니다.

로그: `/tmp/hololive-admin-t06-focused.log`, `/tmp/hololive-admin-t06-settings-browser.log`. 세 브라우저의 BUSY/offline/화면 이동/서버 효과 후 응답 유실은 C02 수정 후 통과했습니다. 이름·채널·멤버 추가 modal은 실패 시 초안을 보존하고, 저장 응답 전 닫지 않습니다. 편집 중 최신 값은 query에서 별도로 읽으며 충돌을 자동 덮어쓰지 않습니다. 이전 modal의 응답이 새 modal을 닫지 않도록 제출 당시 인스턴스를 비교합니다. 별명 초안도 성공 전 지우지 않으며 멤버 optimistic reducer는 제거했습니다. 멤버·알람 조회 상태를 적용했고 다른 기능의 unknown/default 표시는 T07에서 이관합니다.

전체 frontend 시험 172/172, 내장 Chromium/Firefox/WebKit 3/3, skip 0입니다. 실제 멤버·알람 페이지에서 외부 이름/채널 변경, 최신 값/초안 선택, 거부 뒤 초안 보존, 조회 실패 저장 차단, 저장 중 닫은 뒤 새 창을 연 경우를 검증했습니다. lint/typecheck/build도 통과했습니다. 로그: `/tmp/hololive-admin-t06-front-test-final.log`, `...-front-lint-final.log`, `...-front-build.log`. 생성 계약 시험은 추가 헤더 없는 이전 fixture를 교정한 뒤 6+4개 모두 통과했습니다(`...-contract-fixed.log`). 전체 Go CI와 stack retry gate는 C02 증거를 참조합니다. 초기 index chunk 746.35 kB/gzip 131.27 kB 경고는 남아 있으며 frozen baseline 성능 예산 PASS가 아닙니다. T10에서 실제 비교합니다.

Fallback delta: 업무 재시도·offline 자동 재개·새 호환 경로 없음. 실패를 빈 값/기본 ACL/확정 실패로 표시하던 소비자 경로는 이관 중입니다.
