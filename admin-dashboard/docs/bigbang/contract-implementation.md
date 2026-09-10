# T02 계약 생성·연동 검증

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. 원래 HEAD `2323f98c8446f42de581c6f8d2e67e408e3d92df`에서 작업한 로컬 후보입니다. 커밋·발행·배포는 수행하지 않았습니다.

## 결과와 권한

`backend/internal/contract/openapi.json`으로 정본을 옮기고 Swagger mirror와 export 명령을 제거했습니다. baseline의 33개 operation을 유지하며 public metadata와 인증된 OpenAPI 조회를 명세에 추가해 35개 operation이 되었습니다. 등록 route는 baseline 고정 목록 및 명세와 독립 대조합니다. 생성 접근 목록·SDK·108개 validator가 같은 정본을 사용합니다.

`ajv@8.20.0`, `ajv-formats@3.0.1`의 정확한 버전과 브라우저 runtime helper 포함은 사용자의 **“승인 ㄱㄱ”** 응답에 근거합니다. `@playwright/test@1.63.0`은 시험 도구이며 swagger-typescript-api는 기존 13.12.6을 고정했습니다. 추가 업그레이드·MFA·운영 변경 승인은 포함하지 않습니다. compiler는 생성 시에만 실행하고 ucs2length/formats helper만 standalone ESM에 묶습니다.

현재 generation은 `f13edd0d0708aa232aa4283f22642203a9f8b7de76c8cf8b1c54da6daf519de5`입니다. schema·SDK·validator 소유자는 `frontend/scripts/generate-api.mjs`이며 template도 같은 디렉터리에서 관리합니다. SDK는 기존 Axios client를 생성 시 주입받으며 transport를 재할당하지 않습니다. Docker frontend build는 정본 및 Go generation을 복사해 생성 세대 일치를 먼저 확인합니다. PR gate도 새 경로와 계약 fixture를 사용합니다.

요청 query/path/header/body, 응답 status/content-type/header/body를 검증하며 coercion/default/removal은 하지 않습니다. safe 오류 envelope는 code/message/requestId이며 raw upstream 오류를 전달하지 않습니다. upstream 인증 오류는 BFF 세션 401과 구분한 502입니다. 큰 ID는 Go에서 정수 정밀도를 보존한 뒤 decimal string으로 투영합니다. metadata는 no-store이며 API generation 거절은 인증 조회·upstream 호출 전에 수행합니다.

## 실제 검증

| 명령·범위 | 관측 결과 |
|---|---|
| `bash scripts/architecture/check-admin-contract.sh` | 실제 등록·접근 거부·generation·생성 fixture 통과 |
| frontend `corepack npm run test:contract` | schema/Go DTO/별도 깨끗한 fixture 트리의 생성 2회 해시 대조 6개, transport 4개 통과 |
| `go test -count=1 ./admin-dashboard/backend/...` | 시험이 있는 10개 package 통과 |
| backend `golangci-lint run --fix -c .golangci.yml ./admin-dashboard/backend/...` | 0 issues |
| frontend `corepack npm run lint && corepack npm run build` | 통과; 후속 heartbeat 수정 파일의 ESLint도 통과 |
| frontend 전체 `corepack npm test` | 160개 중 159개 통과, 두 탭 회전 1개 실패 후 아래 수정·재검증 |
| `node --test src/hooks/session-tabs.browser.test.mjs` | 수정 후 실제 Chromium 두 탭 1개 통과, 변경 호출 1회·logout 호출 2회 확인 |
| `tsx --tsconfig tsconfig.app.json --test src/api/auth-results.integration.test.ts` | 신규 회귀를 포함해 19개 통과 |
| frontend `corepack npm audit --json` | info/low/moderate/high/critical 및 total 0 |
| Go-only architecture, 변경 shell의 `bash -n`, `git diff --check` | 통과 |

생성 재현 fixture는 임시 트리에 입력·생성 결과를 복사하고 현재 lock으로 설치된 node_modules만 공유합니다. 두 차례 생성 후 모든 generated 파일 및 Go operation 파일의 SHA-256이 원래 후보와 동일하고 원래 작업 트리도 바뀌지 않는지 확인합니다. 새 의존성을 다시 다운로드한 공급망 재현 시험은 아닙니다. dynamic code generation을 금지한 Node에서 standalone validator를 실행했습니다.

브라우저 회귀는 CSRF 조회 중 heartbeat의 전송 전 거부를 장애로 누적하여 다른 탭까지 로그아웃시키던 상태 분류 문제였습니다. CSRF를 아직 확인하지 못한 heartbeat는 AbortError로 중단하고, 다음 예정 heartbeat가 확인된 상태를 사용합니다. 전송 재시도나 업무 큐를 추가하지 않았습니다. 3회 예약 호출에서 POST 0회, 세션 조회 후 다음 호출에서 POST 1회를 실행형 시험으로 확인했습니다. 브라우저는 uid 1000의 user bus와 RuntimeMaxSec/KillMode가 있는 task 전용 cgroup으로 실행·정리했습니다.

## 후속 검증 경계

T03~T08의 config·typed adapter·bootstrap·세션 세대·업무 잠금·화면 이관·구형 코드 제거는 아직 미완료입니다. schema 정본 검증은 서버 입력 검증을 대체하지 않습니다. 실제 production CSP 브라우저 시험, 세 엔진 및 실제 구형 bundle 양방향 시험, 이미지·성능·전환/복구·독립 검토는 T09/T10에서 수행합니다. 이 T02 결과를 최종 후보의 V01~V10 또는 G01~G12 PASS로 해석하지 않습니다.

Fallback delta: none. 새로운 변경 재시도·대체 provider·오류 은폐 경로는 없습니다.
