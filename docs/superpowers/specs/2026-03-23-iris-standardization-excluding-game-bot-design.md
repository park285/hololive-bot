# Iris Standardization Excluding Game-Bot Design

## Goal

`game-bot-go`를 제외한 Iris 사용 레포들의 클라이언트/웹훅/문서/배포 계약을 표준화합니다.

대상 레포:
- `/home/kapu/gemini/hololive-bot`
- `/home/kapu/gemini/chat-bot-go-kakao`
- `/home/kapu/gemini/iris-client-go`
- `/home/kapu/gemini/Iris`

## Current Problems

1. `iris-client-go` preset이 너무 얇아서 각 소비 레포가 transport/timeout/webhook 옵션을 직접 다시 묶습니다.
2. `Iris` 운영 문서가 실제 webhook 소비자 목록을 완전히 반영하지 않습니다.
3. `hololive-bot`는 실제 빌드에 쓰지 않는 로컬 `shared-go/` 복사본 기준으로 docs/scripts/governance가 돌아갑니다.
4. 배포 compose의 `IRIS_BASE_URL` 기본값이 레포별로 달라 운영 토폴로지 계약이 분명하지 않습니다.
5. `hololive-bot`는 legacy `github.com/park285/iris-client-go/client|webhook` 표면을 쓰고, 다른 소비자는 `github.com/park285/iris-client-go/iris/*` 표면을 써서 preset 도입 시 호환 경로가 필요합니다.

## In Scope

- `iris-client-go` preset 확장
- `chat-bot-go-kakao`와 `hololive-bot`의 Iris 초기화 패턴을 preset 기준으로 정리
- `Iris` README의 webhook 소비자 inventory 갱신
- `hololive-bot`의 `shared-go` 관련 문서/스크립트를 실제 active source 기준으로 수정
- 비게임 Iris 소비 레포의 배포 계약을 `IRIS_BASE_URL`/`IRIS_WEBHOOK_TOKEN` 명시 방식으로 통일
- 구현 전제를 `/home/kapu/gemini/go.work` 또는 임시 local `replace` 전략으로 명시
- 관련 테스트와 compose/doc 검증

## Out Of Scope

- `/home/kapu/gemini/llm/game-bot-go`
- `llm/docker-compose.prod.yml`의 game lane 정리
- `shared-go`를 최상위 새 경로로 물리 이동하는 대규모 마이그레이션
- `redroid-config` 변경

## Required Outcomes

1. Iris client와 webhook 기본값이 `iris-client-go` preset으로 재사용 가능해야 합니다.
2. `chat-bot-go-kakao`와 `hololive-bot`는 중복 `iris.With*` 체인이 줄고 repo-specific override만 남아야 합니다.
3. `Iris` README는 현재 실제 webhook 소비자를 모두 나열해야 합니다.
4. `hololive-bot`의 shared-go 관련 스크립트/문서는 실제 build source를 기준으로 동작해야 합니다.
5. 비게임 Iris 소비 레포의 배포 계약은 다음으로 고정합니다:
   - `IRIS_BASE_URL`: compose 기본값에 의존하지 않고 명시 env로 주입
   - `IRIS_WEBHOOK_TOKEN`: inbound webhook 소비자는 명시 env로 주입
   - `IRIS_BOT_TOKEN`: outbound reply/send 전용 토큰으로 유지
6. 새 preset/helper는 legacy `client`/`webhook` 소비자와 `iris/*` 소비자 둘 다 단계적으로 사용할 수 있어야 합니다.

## Acceptance Criteria

- `iris-client-go`의 새 preset/adapter 테스트가 추가되고 통과한다.
- `chat-bot-go-kakao`와 `hololive-bot`의 Iris factory 관련 테스트가 통과한다.
- `hololive-bot`의 shared-go boundary/package/import graph 스크립트가 active shared-go path를 기준으로 성공/실패를 올바르게 낸다.
- `/home/kapu/gemini/go.work` 기준에서 `iris-client-go` 변경이 `chat-bot-go-kakao`에서 즉시 참조 가능하거나, 임시 local `replace`가 계획대로 추가/제거된다.
- 관련 compose/doc 검증이 통과한다.
