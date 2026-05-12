# TASK-D1-02. Deployment Baseline 문서 추가

## Phase

D1. 운영 인벤토리

## 목표

`docs/current/DEPLOYMENT_BASELINE.md`를 추가하여 현재 Docker Compose 운영 기준을 문서화합니다.

## 왜 필요한가

루트 README에는 Docker Compose 기준이 언급되어 있지만, runtime 7개 기준의 현재 배포 baseline 문서가 분리되어 있지 않습니다.

## 먼저 읽을 파일

- `README.md`
- `docs/current/PROJECT_MAP.md`
- `docker-compose.prod.yml`
- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`

## 수정 또는 생성할 파일

- `docs/current/DEPLOYMENT_BASELINE.md`
- `docs/current/README.md`

## 작업 단계

1. 현재 배포 기준이 Docker Compose임을 명시합니다.
2. runtime service 7개와 infra service를 구분해서 표로 작성합니다.
3. 각 service의 port, env group, volume, dependency를 요약합니다.
4. Iris, Postgres, Valkey, Docker proxy, deunhealth 같은 외부/인프라 의존성을 정리합니다.
5. 상세 배포 절차는 기존 deployment guide로 링크합니다.
6. current README에 새 문서를 등록합니다.

## 금지 사항

- 배포 절차 전체를 중복 작성하지 마십시오.
- docker-compose 설정을 수정하지 마십시오.

## 완료 조건

- DEPLOYMENT_BASELINE.md가 생성됩니다.
- Project Map과 runtime 수가 일치합니다.
- deployment guide와 역할이 중복되지 않고 연결됩니다.
- 운영 기준이 k8s/k3s가 아니라 Docker Compose임을 명확히 합니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D1-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
