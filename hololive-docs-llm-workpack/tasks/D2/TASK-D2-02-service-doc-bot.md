# TASK-D2-02. 서비스 문서 추가: bot

## Phase

D2. 서비스 소유권

## 목표

`docs/current/services/bot.md`를 작성하여 `bot`의 현재 책임과 경계를 문서화합니다.

## 왜 필요한가

`bot`는 `Kakao/Iris webhook ingress, command routing, user-facing reply orchestration` 역할을 갖습니다. 이 역할을 문서화해야 새 기능이 잘못된 서비스에 들어가는 일을 줄일 수 있습니다.

## 먼저 읽을 파일

- `docs/current/PROJECT_MAP.md`
- `docs/current/SERVICE_OWNERSHIP.md`
- `docker-compose.prod.yml`

## 수정 또는 생성할 파일

- `docs/current/services/bot.md`
- `docs/current/SERVICE_OWNERSHIP.md`

## 작업 단계

1. 서비스 문서 템플릿 `templates/TEMPLATE_SERVICE_DOC.md`를 사용합니다.
2. Runtime identity 섹션에 module, binary, compose service, port를 적습니다.
3. Owns, Provides, Consumes, Must not own을 분리합니다.
4. Health/readiness endpoint와 dependency를 정리합니다.
5. 관련 contracts와 runbook 링크를 추가합니다.
6. 불확실한 부분은 '검토 필요'로 표시합니다.

## 금지 사항

- 서비스 코드를 수정하지 마십시오.
- 아직 확인하지 못한 API를 확정된 것처럼 쓰지 마십시오.
- 다른 서비스 문서를 이 task에서 수정하지 마십시오. 링크 보정만 허용합니다.

## 완료 조건

- `docs/current/services/bot.md`가 생성됩니다.
- SERVICE_OWNERSHIP.md에서 링크됩니다.
- Project Map의 role과 모순되지 않습니다.
- Must not own 섹션이 존재합니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D2-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
