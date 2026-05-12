# Proposed script: check-runbook-coverage.sh

목적: Project Map의 runtime들이 runbook 링크를 가지고 있고, 해당 파일이 존재하는지 검사합니다.

구현 방향:

1. `docs/current/PROJECT_MAP.md`에서 Runtime Binaries 섹션을 읽습니다.
2. 각 binary에 대해 `docs/current/runbooks/<binary>.md` 또는 Project Map에 명시된 runbook link가 존재하는지 확인합니다.
3. runbook 파일에 필수 섹션이 있는지 확인합니다.

필수 섹션:

- Role
- Dependencies
- Health
- Failure modes
- Diagnosis
- Mitigation
- Rollback
- Smoke test
