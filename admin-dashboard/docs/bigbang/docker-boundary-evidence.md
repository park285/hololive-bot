# T09 실제 Docker proxy 경계와 C05 정정

2026-09-09, `DEC-20260909-hololive-admin-bigbang-replacement`. 운영 Engine 대신 소유한 Unix socket fake Engine, 격리 internal Docker network, 별도 Valkey 9.1.2, 로컬 fixture BFF를 사용합니다. Docker socket 원본은 시험 대상에 mount하지 않습니다.

## C05: proxy의 검사 범위

고정 image `wollomatic/socket-proxy:1.12.3@sha256:74e770f5ed3cfc9ecb6350e177d2aa55873568c85bc953079834e68607dbf71b`에서 `GET /containers/json?all=true&filters={}`가 fake Engine으로 1회 전달되어 HTTP 200을 반환했습니다. query를 포함한 정규식으로 403을 예상한 시험은 실패했습니다.

[1.12.3의 handleHTTPRequest](https://github.com/wollomatic/socket-proxy/blob/bcb95c8f067dfb62f70f2cdfc00154b175e9e4e9/cmd/socket-proxy/handlehttprequest.go#L28)는 `r.URL.Path`를 검사합니다. method 미허용은 405, 경로 미허용은 403입니다. query는 이 검사 입력이 아니며 실제 Engine으로 전달됩니다. 원본 파일은 `/tmp/hololive-admin-proxy-1.12.3-handler.go`에 확인했습니다.

이에 생성 설정의 효과 없는 query 부분을 제거하고 proxy의 권한 경계를 정확한 method·9개 container name·허용 action으로 명시했습니다. 이름 prefix, container ID, AP 대상, inspect/logs/exec/create/delete 등 금지 경로는 별도로 시험합니다. 정책의 container/action 목록이나 운영 dependency는 바꾸지 않았습니다. query 제한을 proxy의 보장으로 주장하지 않습니다.

BFF는 사용자 query를 Docker 요청으로 전달하지 않고 자신의 고정 경로와 stop/restart `t=30`만 사용합니다. `/admin/api/docker/containers/hololive-api/restart?t=-1&signal=SIGKILL`을 호출해 fake Engine에 `/containers/hololive-api/restart?t=30`만 도착하는 것을 확인했습니다. BFF를 완전히 우회할 수 있는 주체의 query까지 이 보호로 제한한다고 주장하지 않습니다.

## 실행

```bash
env XDG_RUNTIME_DIR=/run/user/1000 \
  DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus \
  bash scripts/deploy/test-admin-bigbang-boundaries.sh \
  sha256:5429c13e2a10713002975d3dc074a27d2ce7701224c6c3b6e7015f34bec07c45
```

`systemd-run`의 300초 상한·control-group 종료, 정확한 task label을 가진 Docker 자원의 EXIT 정리를 사용합니다. 공개 포트를 만들지 않고 호스트에서 해당 internal network의 fixture IP로 접근합니다. secret materializer는 모든 경로를 `/tmp/admin-lab-*` 안으로 지정하고 synthetic 값만 사용합니다. 파일은 root:1002/0640, 무관한 UID의 읽기는 거부되며 BFF는 supplementary group 1002로 파일 인증을 수행합니다.

111개 proxy/BFF 권한 사례와 네 파일의 준비·권한·인증 검사가 통과했습니다. 상세 결과는 `frontend/node_modules/.cache/admin-boundaries.json`, 로그는 `/tmp/hololive-admin-t09-boundaries.log`입니다. service env 파일을 읽지 않는 canonical Compose overlay 대조도 같은 harness에서 통과했습니다. prod → admin-security → live-compat 순서, secret env 제거, 전용 network·mount·healthcheck·loopback port를 확인했습니다. 이는 actual first cutover·rollback·arm64 검증의 완료가 아닙니다.

Fallback delta: none. C05는 고정 dependency의 검사 범위에 대한 잘못된 전제를 정정합니다. 실행 경로·provider·업무 재시도를 추가하지 않았습니다.
