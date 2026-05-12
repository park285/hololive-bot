# hololive-bot LLM 친화 문서 안정화 작업팩

작성일: 2026-05-12

이 압축 파일은 `hololive-bot` 저장소의 문서 체계를 **현재 운영 기준에 맞게 단단하게 정리**하기 위한 LLM 작업 문서 모음입니다.

범위는 명확합니다.

- RPC/gRPC 도입은 제외합니다.
- 코드 리팩토링보다 문서 SSOT, 계약 문서, 런타임 경계, 운영 Runbook, CI 문서 게이트를 우선합니다.
- 각 작업은 LLM이 단독 PR 단위로 수행할 수 있도록 잘게 나누었습니다.
- 각 작업 문서는 “읽을 파일”, “수정할 파일”, “금지 사항”, “검증 명령”, “완료 조건”을 포함합니다.

가장 먼저 읽을 문서:

1. `00_START_HERE.md`
2. `01_REVIEW_FINDINGS.md`
3. `02_EXECUTION_ORDER.md`
4. `03_GLOBAL_RULES.md`
5. `tasks/README.md`

권장 작업 방식:

- 한 번에 전체를 수정하지 마십시오.
- `tasks/` 아래의 단일 Task 문서 하나를 LLM에게 제공하고, 해당 Task만 수행하게 하십시오.
- 각 PR은 `04_VALIDATION_MATRIX.md`의 공통 검증을 통과해야 합니다.
