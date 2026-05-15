# hololive-bot baseline 제거 Big-Bang LLM 작업문서 v8-split

이 패키지는 `docs/architecture/go-function-budget-baseline.txt` 제거 작업을 하나의 큰 문서가 아니라, LLM에게 나누어 줄 수 있는 작은 작업문서 묶음으로 재구성한 것입니다.

핵심 원칙은 하나입니다. 작업은 내부적으로 잘게 쪼개되, 최종 반영은 baseline 제거와 모든 함수 strict gate 통과가 함께 들어가는 big-bang PR이어야 합니다.

## 사용 순서

1. `00-global/00-start-here.md`를 먼저 읽습니다.
2. Manager LLM에게 `01-manager/` 문서 전체를 줍니다.
3. A00/A01 patch는 `02-patches/` 문서만 보고 적용합니다.
4. Worker LLM에게는 `03-prompts/worker-prompt.md`, `05-shards/<module>/<shard>.md`, 필요한 경우 `04-patterns/<pattern>.md`만 줍니다.
5. Reviewer LLM에게는 `03-prompts/reviewer-prompt.md`, 해당 shard 문서, `06-review/` 체크리스트를 줍니다.
6. Validator LLM에게는 `03-prompts/validator-prompt.md`, `07-validation/` 문서를 줍니다.

모든 command 예시는 repository root에서 실행하는 것을 전제로 합니다.

## 문서 묶음 구조

```text
00-global/      전체 원칙, 완료 정의, 금지 사항, risk model
01-manager/     inventory, ledger, shard 분배, 통합 순서
02-patches/     baseline 제거와 strict checker 전환 patch
03-prompts/     Worker/Reviewer/Validator에게 복붙할 prompt
04-patterns/    함수 유형별 코드 분리 패턴
05-shards/      실제 LLM 작업 shard 카드
06-review/      reviewer 체크리스트와 실패 냄새
07-validation/  최종 검증 명령과 PR 템플릿
08-ledger/      ledger 템플릿과 schema
tools/          inventory와 auto-shard 생성을 돕는 스크립트
```

## 작업 분할 기준

각 Worker shard는 원칙적으로 최대 5개 over-budget 함수만 처리합니다. R5 고위험 shard는 1~2개 함수 단위로 제한합니다. 정적 shard 문서가 여러 함수를 포함해도, 실제 최신 inventory에서 5개를 넘으면 Manager가 `AUTO-###` micro-shard로 다시 쪼갭니다.
