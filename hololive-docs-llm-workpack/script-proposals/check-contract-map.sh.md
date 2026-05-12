# Proposed script: check-contract-map.sh

목적: Contract Map이 실제 서비스와 contract package를 가리키는지 검사합니다.

검사 항목:

- `docs/current/CONTRACT_MAP.md` 존재
- Contract Map에 `Provider`, `Consumers`, `Package`, `Runbook` 열 존재
- Package path가 실제 존재
- Provider/Consumer service name이 Project Map에 존재
- Runbook link가 실제 존재
- contract별 상세 문서가 `docs/current/contracts/*.md`에 존재
