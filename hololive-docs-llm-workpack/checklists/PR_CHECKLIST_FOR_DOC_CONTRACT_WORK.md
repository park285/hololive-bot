# PR Checklist for 문서/계약 작업

## 기본

- [ ] Task ID를 PR 본문에 적었다.
- [ ] 범위 밖 파일을 수정하지 않았다.
- [ ] RPC/gRPC 관련 내용을 추가하지 않았다.
- [ ] current/history/design 규칙을 지켰다.

## Project Map

- [ ] Project Map과 go.work가 불일치하지 않는다.
- [ ] Project Map과 docker-compose.prod.yml의 runtime service가 불일치하지 않는다.
- [ ] runtime별 runbook link가 존재한다.

## Contract

- [ ] Contract Map을 갱신했다.
- [ ] 해당 contract 상세 문서를 갱신했다.
- [ ] 코드 contracts package와 문서 path/error code가 일치한다.
- [ ] 확인되지 않은 내용은 '검토 필요'로 표시했다.

## Runbook

- [ ] 영향받는 runtime runbook을 갱신했다.
- [ ] Diagnosis, Mitigation, Rollback 섹션이 있다.

## 검증

- [ ] `./scripts/architecture/check-project-map.sh`
- [ ] `./scripts/architecture/ci-boundary-gate.sh`
- [ ] 새로 추가한 문서 gate
