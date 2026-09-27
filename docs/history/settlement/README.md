# Settlement History

이 디렉터리는 제거된 `settlement-go` 런타임의 과거 설계/계획 문서를 보관합니다.

- active runtime inventory와 현재 운영 절차의 SSOT로 사용하지 않습니다.
- schema 퇴역은 끝났습니다. 2026-09-26 T18 운영 증거에서 운영 DB의 `settlement_*` 테이블 0개를 확인했고, 퇴역 runbook과
  수동 drop 스크립트(`settlement_drop.sql`), archive migration(`038_create_settlement.sql`, `039_create_settlement_v2.sql`)과
  그 보안 테스트는 stack-audit 2026-09-26(PLN-20260926-stack-audit-refactoring T11)에서 삭제했습니다. 정의가 필요하면 그 이전
  리비전의 git 기록을 봅니다.
