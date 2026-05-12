# TASK-D0-01. 루트 README와 Project Map 정합성 복구

## Phase

D0. 문서 기준선 복구

## 목표

루트 `README.md`가 현재 7 runtime 기준과 일치하도록 갱신합니다.

## 왜 필요한가

현재 루트 README는 5 runtime/6 module 설명을 일부 유지하고, `docs/current/PROJECT_MAP.md`는 7 runtime 기준입니다. 이 불일치는 신규 작업자와 LLM에게 잘못된 구조를 주입합니다.

## 먼저 읽을 파일

- `README.md`
- `docs/current/PROJECT_MAP.md`
- `go.work`
- `docker-compose.prod.yml`

## 수정 또는 생성할 파일

- `README.md`

## 작업 단계

1. 루트 README의 모듈/런타임 표를 `docs/current/PROJECT_MAP.md`의 7 runtime 기준으로 맞춥니다.
2. `admin-api`, `alarm-worker`가 현재 runtime임을 명시합니다.
3. README의 상세 설명은 줄이고, 상세 기준은 `docs/current/PROJECT_MAP.md`로 연결합니다.
4. 배포 기준이 Docker Compose라는 내용은 유지하되 `docs/current/DEPLOYMENT_BASELINE.md`가 아직 없으면 '추가 예정'으로 표시하지 말고 Project Map과 deployment guide를 연결합니다.
5. 루트 README가 SSOT가 아니라 gateway라는 문구를 추가합니다.

## 금지 사항

- Project Map 자체를 확장하지 마십시오. 이 task는 README 정합성만 다룹니다.
- docker-compose.prod.yml을 수정하지 마십시오.
- 코드를 수정하지 마십시오.

## 완료 조건

- README가 7 runtime을 정확히 설명합니다.
- README가 `docs/current/PROJECT_MAP.md`를 참조합니다.
- README와 Project Map의 runtime 수가 다르지 않습니다.
- 과거 5 runtime 기준 문구가 남아 있지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
go test . -run TestRuntimeSplitStandaloneModulesContract
```

## LLM 작업 프롬프트

```text
Task TASK-D0-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
