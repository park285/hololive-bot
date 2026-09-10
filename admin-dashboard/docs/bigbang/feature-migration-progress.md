# T07 기능 이관 진행 기록

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. T07 기능 이관·집중 검증 완료 기록입니다. production bundle·실제 구형 artifact·출시 게이트 완료 기록은 아닙니다.

멤버·알람은 T06의 실제 편집창/조회 상태를 사용합니다. 채팅방은 조회 전에 aclEnabled=true/aclMode=blacklist를 만들지 않으며 최신 조회가 없는 변경을 막습니다. 참여 방 조회는 독립적인 조회 상태를 표시하고, 확인된 ACL 목록을 계속 조회하는 기존 계약을 유지합니다. 변경 후 목록 조회 실패가 입력 초안이나 확인된 업무 결과를 덮어쓰지 않게 합니다.

방송은 조직 변경 시 이전 조직의 placeholderData를 표시하지 않습니다. 실패·오프라인·이전 값은 QueryView/QuerySection이 구분합니다. 달력도 실패/이전 값/빈 월을 구분합니다. 통계는 미확인 값을 0건이나 Offline으로 만들지 않습니다. WS는 생성 SystemStats validator만 수용하며 legacy goroutines·snake_case alias·Number 강제 변환·현재 시각 대체를 제거했습니다. 관측 실패는 이전 값과 함께 표시하고 서비스 관측 실패를 차트의 실제 0값으로 바꾸지 않습니다.

Docker는 `features/docker/{api,hooks,components}`에서 생성 SDK/operations를 사용합니다. api/core.ts의 수동 validator·오류 wrapper와 재노출 barrel, 설정/Docker 재노출 component를 제거했습니다. 설정 폼의 단일 owner는 `features/settings/components/SettingsForm.tsx`입니다. 검증되지 않은 initialData를 주입하던 경로도 제거했습니다. Docker의 현재 상태·managed/stopBlocked를 확인해야 변경할 수 있고 확인창은 취소에 최초 focus를 둡니다. 운영 Docker 명령은 실행하지 않았습니다.

집중 시험 83/83이 통과했습니다(`/tmp/hololive-admin-t07-focused-fixed.log`). Chromium에서 실제 ContainerList 버튼과 확인창으로 3행위×4결과, 취소 효과 0회, 변경 각 1회, 인프라 stop 거부, 조회 실패 뒤 버튼 차단을 확인했습니다(`/tmp/hololive-admin-t07-docker-browser.log`). 지원되지 않는 가짜 컨테이너 이름과 initialData만 사용하던 hook browser fixture는 이 실제 화면 시험으로 교체했습니다. 오류 wrapper를 제거한 뒤 timeout은 원래 Axios 오류와 공통 unknown 결과를 함께 검증합니다. 502 응답에 FORBIDDEN 코드가 섞여도 사전 거부로 주장하지 않는 검증을 추가했습니다.

`scripts/architecture/check-admin-feature-parity.mjs`는 T01의 33개 route가 보존되는지, 35개 SDK operation이 실제 frontend owner에 연결되는지 AST로 확인하고 `candidate-parity.json`과 대조합니다. source mapping 통과는 기능 게이트의 대체가 아닙니다. 화면 없는 names/user와 community-shorts 진단은 별도 SDK 호출 시험에서 큰 문자열 ID·정상 응답·호출 1회를 확인했습니다(`/tmp/hololive-admin-t07-hidden-apis.log`, 10/10 중 2개 명시적 hidden API 시험).

세 브라우저에서 통계·방송·달력·채팅방의 최초 오류·이전 값·offline·empty/정상 0값을 확인했고 조직 변경, 연도 이동, ACL 토글/모드 변경과 방 추가를 실행했습니다. 멤버/알람은 외부 편집 충돌·거부 뒤 초안·저장 중 새 창 보존 외에 알람 삭제, 별명 추가/삭제, 졸업/복귀를 각 1회 실행했습니다. Docker는 3행위×4결과와 취소 0회·인프라 stop 차단을 실제 화면에서 확인했습니다.

390px 모바일에서 7개 메뉴 이동, drawer의 Tab/Shift+Tab 내부 순환과 focus 복귀를 확인했습니다. 최초에는 Shift+Tab이 배경 버튼으로 빠졌고 현재 메뉴 재선택 시 drawer가 닫히지 않아, 공통 `dialogFocus`와 선택 시 닫힘으로 수정했습니다. 120개 멤버 fixture의 가상 목록에서 48번째 항목을 선택해 `9007199254741040`과 이름이 일치함을 확인했습니다. 200% 글자 크기에서도 편집 dialog와 취소 버튼이 viewport 안에 표시됩니다(`/tmp/hololive-admin-t07-text200-{chromium,firefox,webkit}.png`). CSS 글자 확대 검증이며 브라우저 자체 zoom 조작이라는 주장은 하지 않습니다. 기존 BaseModal의 focus·중첩·ARIA 회귀도 통과했습니다(`...-modal-focus.log`).

ASVS 1.2.2에 따라 방송/기념일 링크·이미지는 HTTP(S) URL을 검사하고 URL 성분을 인코딩합니다. javascript/data/file/credential URL 및 가짜 hostname 표시는 거부합니다. 해당 fixture 4/4가 통과했습니다(`...-url-tests.log`). 지원하지 않는 URL을 다른 플랫폼 성공 링크로 바꾸지 않습니다.

최신 C04 후보는 전체 frontend 173/173, 세 브라우저 3/3, skip 0입니다. 전체 Go CI·생성 계약·stack retry도 C04 근거대로 통과했습니다. C04 이전 frontend 전체 실행은 settings의 HTTP 400/FORBIDDEN 불일치 fixture 1건이 실패했으며 실제 BFF의 BAD_REQUEST와 최초 선점 근거에 맞춰 교정했습니다. C04는 마지막 HTTP 거부가 이전 효과를 부정하지 못하는 재현과 보완입니다. T08 retirement, production build/CSP, 실제 old artifact 혼합, 성능·전환·운영 인수 G01~G12는 여전히 별도 검증 대상입니다.

C04 적용 뒤 lint와 build도 통과했습니다(`/tmp/hololive-admin-c04-lint-final.log`, `...-build.log`). 최초 선점 근거의 403/rejected와 503/failed 분기는 추가 집중 시험 8/8로 확인했습니다(`...-outcomes-final.log`). 초기 JS chunk 크기 경고는 남아 있으며 G10의 실제 기준/후보 전송량 비교로 판정해야 합니다.

Fallback delta: none. 조직 placeholder와 구형 WS 해석 경로, 미확인 통계/ACL 기본값을 제거했습니다. 기존에 소유한 독립 조회·제한된 WS reconnect와 이미지 placeholder만 유지합니다.
