# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

## r1.2.0 - 2026-11-24

- **`!달력` 월간 포스터 고도화·분리 앨범·테마 회전:** `!달력` 이미지를 세로 목록형에서 7열 월간 격자 포스터(`1024px` 정수 격자, 페이지당 최대 `1536px`)로 교체했습니다. 생일과 데뷔 기념일이 모두 있는 달은 `1/2 생일 달력`과 `2/2 데뷔 기념일 달력` 2장 앨범(`SendImages`)으로 분리 전송하고, 한 종류만 있거나 빈 달은 1장을 보냅니다. 라이트(`prism_daylight`)와 다크(`starlight_orbit`, `!스케쥴` `Orbit` 패밀리룩) 두 테마를 같은 달 요청마다 직전과 겹치지 않게 교차 회전하며 한 번의 호출에서 반환되는 2장은 동일 테마로 묶습니다. 구형 단일 이미지 경로·`render/cardkit`·`render/theme`를 제거했고, 공유 Valkey 캐시 키를 `bot:calendar:album:v6:<YYYY-MM>:<theme>`으로 변경해 구 `v5` 항목과 분리했습니다.

- **의존성:** Go 모듈(klauspost/compress 1.20.5, mongo-driver/v2 2.10.1, x/net 0.63.0, google.golang.org/api 0.316.0, otel auto/sdk 1.2.2 등)과 xspaces·po-sandbox·youtubejs의 npm lockfile을 최신 안정판으로 올렸습니다. Node는 26.13.1(이미지 `NODE_VERSION`, CI, 이벤트 등급 helper의 정확한 버전 검사와 `engines`), npm `packageManager`는 12.2.3, Dockerfile 프런트엔드는 1.29.0입니다. ingress nginx 기반은 1.31.9로 올리고 이미지 태그를 `localhost/iris-stack-nginx:1.31.9-zlib1.4-r0`로 바꿨으며, Quadlet의 `NGINX_IMAGE`도 같은 태그를 가리킵니다. PostgreSQL 이미지의 gosu는 1.20(moby/sys/user 0.5.0, x/sys 0.50.0)으로 빌드하고, golang·distroless·node 기반 이미지 digest와 govulncheck 기본값(v1.10.0)을 갱신했습니다. ChatBotGo·TwentyQ가 쓰는 PostgreSQL 이미지는 `5e9b9b1f8` 빌드를 계속 요구하므로, 새 recipe는 다시 빌드하고 두 저장소의 provenance 값을 옮길 때 반영됩니다. Valkey 9.1.3은 스택 계약 고정값이라 그대로 둡니다.

- **CI:** 배포 스크립트 시험이 `ap-host-native-cutover_test.sh`를 실행하는 user unit의 시간 상한을 120초에서 300초로 늘렸습니다. 이 시험은 단독으로 67~86초가 걸리는데, 호스트 부하가 높을 때 120초에서 끊겨 pre-push 게이트가 실패한 일이 있었습니다. 멈춘 시험을 끊는 역할은 그대로입니다.

## r1.1.0 - 2026-11-22

- PostgreSQL 18 전용이던 pg_jsonschema 실험의 재현 파일(`Dockerfile.jsonschema`, `jsonschema.py`, `testqueries/jsonschema.sql`)을 지웠습니다. pg_jsonschema v0.3.4에는 PostgreSQL 19용 바이너리가 없습니다. [실험 README](scripts/experiments/postgres-extensions/README.md)에는 측정 결과와 보류 판단만 남깁니다. 운영 이미지와 배포는 바뀌지 않습니다.

- **`!스케쥴` 카드 시안 회전:** 쿄코·소피아·메라·츠즈리의 3:4 classic 일정 카드가 요청마다 기존 디자인과 새 포스터 시안(쿄코 2종, 나머지 멤버 각 3종) 중 하나를 무작위로 고르며, 같은 멤버에게 직전에 고른 시안은 다시 고르지 않습니다. 직전 시안은 프로세스 메모리에만 남으므로 재시작 뒤에는 전체에서 다시 고릅니다. orbit 템플릿, 16:9·4:3 형식, 다른 멤버는 기존 디자인 하나를 그대로 씁니다. 렌더 캐시는 시안별로 따로 두고 메모리 상한을 32장으로 늘렸으며, 공유 캐시 키가 `bot:schedule:card:v4:…`로 바뀌어 v3 항목은 읽지 않습니다. 저장된 카드 양식이 orbit인 멤버는 회전하지 않으며, 기본 스키마가 소피아를 orbit으로 넣으므로 운영 값이 orbit이면 Iris Console에서 classic으로 바꿔야 소피아 카드가 회전합니다.

## r1.0.1 - 2026-11-22

- **collector 이름 전환 배포 경로:** native a에 `ap-host-native-deploy.sh osaka --install-initial`을 추가했습니다. 이전 이름의 설치를 퇴역시킨 osaka1에서 새 root의 `current`·`previous`·복구 guard와 collector·issuer unit 파일·drop-in이 모두 없을 때만 첫 설치를 하고, 관찰 검사가 통과하면 현재 릴리스를 수용 표식으로 고정해 다음 `--apply`의 원점으로 씁니다. 일반 `deploy`는 계속 osaka1을 거부합니다. [collector runbook](docs/current/runbooks/collector.md#이름-전환)에 slot별 퇴역·첫 설치, b·c의 다음 배포 경로(`quiesce-verified`→`resume`), d의 image·profile 위치를 적었고, [PostgreSQL major 전환](docs/current/runbooks/postgres-major-upgrade.md#순서)에 epoch3 원장의 002·003 적용 경로를 적었습니다.
