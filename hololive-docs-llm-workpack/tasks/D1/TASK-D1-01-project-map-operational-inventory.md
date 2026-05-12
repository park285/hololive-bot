# TASK-D1-01. Project Map을 운영 인벤토리로 확장

## Phase

D1. 운영 인벤토리

## 목표

`docs/current/PROJECT_MAP.md`를 단순 module 목록에서 runtime 운영 인벤토리로 확장합니다.

## 왜 필요한가

현재 Project Map은 module/path/role/port 중심입니다. 운영자가 실제로 필요한 compose service, binary, health/readiness, queue role, runbook link까지 포함해야 합니다.

## 먼저 읽을 파일

- `docs/current/PROJECT_MAP.md`
- `go.work`
- `docker-compose.prod.yml`
- `docs/current/runbooks/README.md`

## 수정 또는 생성할 파일

- `docs/current/PROJECT_MAP.md`

## 작업 단계

1. 기존 module inventory 표는 유지하되 compose service, binary, health, ready, runbook link 열을 추가하거나 별도 runtime 표를 확장합니다.
2. runtime 7개 각각에 service doc link와 runbook link를 넣습니다.
3. shared library와 compose file은 runtime과 구분합니다.
4. Project Map maintenance rule에 contract/runbook/doc gate 실행 기준을 추가합니다.
5. 현재 확정되지 않은 항목은 'TBD'가 아니라 '검토 필요'로 표시하고 후속 task를 연결합니다.

## 금지 사항

- runbook 파일 본문을 이 task에서 작성하지 마십시오.
- service ownership 파일 본문을 이 task에서 작성하지 마십시오.
- compose 파일을 수정하지 마십시오.

## 완료 조건

- Project Map만 봐도 7 runtime 운영 구성이 이해됩니다.
- 각 runtime에 runbook link가 있습니다.
- 각 runtime에 service ownership doc link가 있습니다.
- go.work와 불일치하지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D1-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
