# TASK-D6-08. Runbook 추가: youtube-scraper

## Phase

D6. Runbook

## 목표

`docs/current/runbooks/youtube-scraper.md`를 작성하여 YouTube scraping/polling runtime 장애 대응 절차를 문서화합니다.

## 왜 필요한가

runtime별 runbook이 없으면 운영자는 장애 시 Project Map과 compose를 뒤져야 합니다. LLM도 어떤 진단 명령을 써야 하는지 알 수 없습니다.

## 먼저 읽을 파일

- `docs/current/PROJECT_MAP.md`
- `docs/current/services/youtube-scraper.md`
- `docker-compose.prod.yml`
- `templates/TEMPLATE_RUNBOOK.md`

## 수정 또는 생성할 파일

- `docs/current/runbooks/youtube-scraper.md`
- `docs/current/runbooks/README.md`
- `docs/current/PROJECT_MAP.md`

## 작업 단계

1. Runbook 템플릿을 사용합니다.
2. Role, Dependencies, Ports, Env, Health, Ready, Logs, Metrics, Failure modes, Diagnosis, Mitigation, Rollback, Smoke test를 작성합니다.
3. 확인된 endpoint만 확정적으로 씁니다.
4. Project Map과 runbook index에 링크를 맞춥니다.
5. 모르는 metric은 '검토 필요'로 표시합니다.

## 금지 사항

- 새 monitoring metric을 코드에 추가하지 마십시오.
- compose 설정을 변경하지 마십시오.

## 완료 조건

- `docs/current/runbooks/youtube-scraper.md`가 생성됩니다.
- Runbook index에 등록됩니다.
- Project Map에서 링크됩니다.
- Diagnosis와 Rollback 섹션이 있습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D6-08만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
