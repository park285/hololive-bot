# Hololive Scraper Plan v2

이 패키지는 이전 diff pack을 보완한 실행 계획 중심 문서입니다.

구성:

- `planning/`: master plan, PR gates, telemetry, test, rollout, risk
- `diff-phases/`: 기존 phase별 diff 수준 구현안
- `llm-tasks/`: LLM 작업자용 task 분해
- `runbooks/`: 운영 대응 runbook

권장 사용 순서:

1. `planning/00_MASTER_EXECUTION_PLAN.md`
2. `planning/01_DECISION_RECORDS_AND_INVARIANTS.md`
3. `planning/02_PR_SEQUENCE_AND_GATES.md`
4. `llm-tasks/10_LLM_TASK_BREAKDOWN.md`
5. `diff-phases/phase-01-failure-taxonomy.md`부터 순서대로 적용
6. 각 PR마다 `planning/04_TEST_MATRIX_AND_CI_PLAN.md` 확인
7. 배포 시 `planning/05_ROLLOUT_AND_ROLLBACK_PLAN.md` 확인
8. 운영 시 `runbooks/09_OPERATIONS_RUNBOOK.md` 확인
