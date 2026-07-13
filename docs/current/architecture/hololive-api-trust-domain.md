# hololive-api Process Trust Domain

## Decision

`#023`은 현재의 통합 구조를 수용합니다. Bot, admin, LLM plane은 하나의
`hololive-api` binary와 address space를 공유하며 별도의 OS 보안 경계로 취급하지 않습니다.

One process is one trust domain.

## Required controls

- Admin과 LLM listener는 host의 `127.0.0.1`에만 TCP/UDP로 publish하고 기본 transport를
  `h3`로 유지합니다.
- Admin과 LLM internal route는 비어 있지 않은 `API_SECRET_KEY`를 요구합니다.
- `hololive-api`는 `docker-proxy-net`에 가입하거나 Docker socket 또는 `DOCKER_HOST`를
  받아서는 안 됩니다.
- No admin or LLM endpoint may load native plugins, spawn processes, invoke shells, or execute
  user-controlled native commands. LLM output과 일반 사용자 입력은 data로만 처리하며 runtime
  extension, shell command, script, 또는 plugin entry point로 해석하지 않습니다.

Admin template update and preview are an explicit, authenticated, capability-bounded interpretation
surface. `PUT /api/holo/templates/:key`와 `POST /api/holo/templates/:key/preview`는 비어 있지 않은
`API_SECRET_KEY` 인증 뒤 user-supplied Go `text/template` body를 parse하고 execute합니다. Save는
고정 sample data로 실행 검증한 뒤 저장하고, preview는 같은 종류의 sample data로 즉시 실행합니다.
저장된 template은 이후 app-owned notification data에 대해 renderer가 실행합니다. 사용할 수 있는
`FuncMap`은 문자열·형식 변환 helper의 고정 allowlist이며 file, network, native command, plugin,
process primitive를 제공하지 않습니다. They do not provide native command, plugin, or process
execution. 따라서 이는 user-supplied template interpretation surface이지, 사용자 입력을 전혀
해석하지 않는 endpoint라는 의미는 아닙니다.

Repository security contract tests는 위 Compose listener/network/secret wiring과 production
Go source의 직접적인 `os/exec` 및 `plugin` import 부재, 그리고 authenticated admin template
route에서 `req.Body`가 고정 `text/template` parse/execute sink로 이어지는 계약을 검증합니다.

## Accepted consequence

A process-level compromise in any plane exposes credentials available to all three planes, including bot egress credentials.

따라서 plane별 route auth는 네트워크 요청 경계를 보호하지만, 같은 프로세스 안에서 침해가
발생한 뒤의 credential isolation을 제공하지는 않습니다.

## Split trigger

Split trigger: if admin-plane or LLM-plane compromise must not expose bot egress credentials,
`hololive-api`를 별도 process/container로 분리하고 각 runtime에 필요한 최소 credential만
주입하는 migration을 먼저 설계해야 합니다. 이 요구가 생기면 통합 구조에 기능을 덧붙여
우회하지 않고 별도 migration plan과 rollout 승인을 거칩니다.
