# T08 구형 실행 경로 제거와 현재 owner

`DEC-20260909-hololive-admin-bigbang-replacement`, 2026-09-09. T08의 로컬 구현·검증 기록이며 T09/T10 출시 gate의 완료 선언이 아닙니다. [제거 manifest](retirement.json)의 최종 status는 동일 출시 후보 검증까지 pending을 유지합니다.

## 변경

- R01~R18의 구형 패키지·barrel·wrapper·hook·store를 제거했습니다. `SessionState`가 인증·정책·절대 만료·CSRF를 한 번에 갱신하는 유일한 상태입니다. warning store는 활동 시각과 표시 상태만 소유합니다. `AuthenticatedSession`의 auth generation key가 이전 편집·modal·timer를 폐기합니다.
- heartbeat와 activity/warning hook은 `session/`, Query client·keys·WS 구독은 `queries/`, 멤버 변경은 해당 feature가 소유합니다. 비동기 HTTP 계층은 Router/store/hook을 import하지 않습니다. 로그인·업무 변경의 오류 표시는 각각의 owner로 모았습니다.
- 사용하지 않는 WS application ping·send API와 validator 없는 generic cast를 제거했습니다. metadata/protocol 확인과 기존 재연결 5회 상한은 유지합니다.
- R19의 누락 필드 보정과 WS session ID 대체를 제거했습니다. 필수 ID/family/time 필드가 없는 레코드는 조회·갱신·회전·삭제에서 인증되거나 보정되지 않습니다. 기존 rotation·family 폐기와 mutation claim 회귀를 유지합니다. 실제 전환 전 관리자 session purge 증거는 T09에 남아 있습니다.
- Vite는 metadata 경로도 BFF로 proxy합니다. production public 자산은 favicon·theme initializer만 포함하며 개발 MSW worker를 제외합니다. `node_modules/.cache/admin-build-inventory.json`에 실제 번들 module graph·입력·출력 SHA-256을 남기고 공개 assets에서는 제외합니다.
- `check-admin-retirement.sh`는 manifest의 source 경로, TS AST import/단일 Axios owner, Go 연결 패키지, 실제 등록 route, 필수 필드 거부, production module·파일 hash와 image rootfs·binary·embed 자산을 대조합니다. 같은 경로를 재사용한 R05/R07/R09/R17/R18은 폐기한 동작·생성물도 별도로 검사합니다.
- AGENTS·문서 인덱스·OpenAPI pipeline·CI consumer·운영 runbook을 현재 owner로 정리했습니다. F07의 중앙 빌드/업무 서비스 동반 재기동 안내를 제거하고 `prod → admin-security → live-compat`, 원격 `--no-build --no-deps`로 맞췄습니다. O02의 실제 firewall 미적용은 정의와 구분해 기록했습니다. 과거 handoff는 historical 표기를 유지합니다.

## 실제 검증

| 검사 | 관찰 결과 |
|---|---|
| 프런트 전체 | 172/172, skip 0; 내부 Chromium/Firefox/WebKit 3/3, skip 0 (`/tmp/hololive-admin-t08-front-test.log`) |
| 프런트 lint·build | exit 0. 초기 index 718.27 kB, gzip 추정 121.84 kB; 500 kB 경고 유지. 실측 성능 gate 아님 |
| Go 전체 CI | exit 0; lint·NilAway·build·전체 test/race·govulncheck (`/tmp/hololive-admin-t08-go-ci.log`) |
| 공급망 Go 관찰 | 호출 코드 0건, import package 0건; 기존 module-only 미사용 openpgp advisory 1건은 유지 |
| source/bundle/image 제거 | 19개 항목, source 142개·import 374개·module 312개, 실제 route/세션 회귀 통과 |
| 제거 검사의 거부 동작 | 구형 `api/core.ts`를 넣은 fixture와 자산 byte 변경을 모두 거부; fixture 원복 후 정상 검사 통과 |
| feature parity | baseline 33개·candidate 35개·업무 변경 16개·메뉴 7개 대응 통과 |
| 문법·diff | shell syntax, Node parser, `git diff --check` 통과 |

image 대조의 상세 identity는 [로컬 시험 증거](retirement-local-fixture.json)에 기록했습니다. image의 Go binary에서 구형 패키지/함수가 없고, 검증한 자산 bytes가 모두 embed되어 있음을 확인했습니다. 실행 중인 운영 image를 변경하거나 운영 credential을 사용하지 않았습니다. 이후 입력/출력 hash가 달라지면 이 evidence를 현재 후보의 성공으로 재사용하지 않습니다.

GitHub frontend gate는 ephemeral runner에 pinned Playwright Firefox/WebKit과 user systemd bus를 준비하고 source/bundle 제거 검사를 실행하도록 갱신했습니다. 해당 원격 workflow는 이 세션에서 실행하지 않았습니다. 브라우저 준비를 skip으로 대체하지 않으며 full local CI·race·NilAway·security workflow의 기존 책임은 유지합니다.

Fallback delta: 구형 세션·WS data·중복 store/transport 경로와 클라이언트 policy 기본값을 제거했습니다. 업무 재시도·compat alias·자동 대체 경로를 추가하지 않았습니다. 전환 시 구형 session을 폐기하는 조건은 T09 리허설에서 검증해야 합니다.
