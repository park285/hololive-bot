# 02. 실행 순서

아래 순서대로 진행하십시오. 앞 phase가 완료되지 않으면 뒤 phase의 문서가 다시 흔들립니다.

## Wave 1. 문서 기준선 복구

1. `TASK-D0-01`: 루트 README와 current Project Map 정합성 복구
2. `TASK-D0-02`: current/history 문서 분리
3. `TASK-D0-03`: current README와 docs README 인덱스 정리

목표: “현재 기준”과 “과거 기록”을 분리합니다.

## Wave 2. 현재 운영 구조 문서화

4. `TASK-D1-01`: Project Map 운영 인벤토리 확장
5. `TASK-D1-02`: Deployment Baseline 문서 추가
6. `TASK-D2-01`: Service Ownership 문서 추가
7. `TASK-D2-02`~`TASK-D2-08`: 서비스별 문서 추가

목표: runtime 7개가 모두 운영 문서와 연결됩니다.

## Wave 3. 내부 계약 문서화

8. `TASK-D3-01`: Contract Map 추가
9. `TASK-D3-02`: contracts README 추가
10. `TASK-D3-03`~`TASK-D3-08`: 계약별 문서 추가
11. `TASK-D4-01`: Error Contract 추가
12. `TASK-D5-01`: Queue/PubSub Contract 추가

목표: 내부 API, Queue, Pub/Sub, Iris boundary를 문서화합니다.

## Wave 4. Runbook coverage 보강

13. `TASK-D6-01`: Runbook index 재정리
14. `TASK-D6-02`~`TASK-D6-08`: runtime별 runbook 추가
15. `TASK-D6-09`: DLQ replay runbook 추가
16. `TASK-D6-10`: release/rollback runbook 추가

목표: 각 runtime 장애 대응 절차를 균일화합니다.

## Wave 5. 문서 게이트 강화

17. `TASK-D7-01`: current historical 문서 검사 gate 추가
18. `TASK-D7-02`: runbook coverage gate 추가
19. `TASK-D7-03`: contract map gate 추가
20. `TASK-D7-04`: internal route hardcoding gate 일반화
21. `TASK-D7-05`: error contract gate 추가
22. `TASK-D7-06`: ci-boundary-gate에 문서 gate 연결

목표: 문서가 코드와 불일치하면 CI가 실패합니다.

## Wave 6. PR/Release Governance 연결

23. `TASK-D8-01`: PR template 강화
24. `TASK-D8-02`: release governance assets 확장
25. `TASK-D8-03`: LLM 작업 규칙 문서 추가

목표: 이후 모든 PR이 문서/계약 영향도를 확인하게 합니다.
