# 00 — 시작 문서

## 목표

`hololive-bot` 저장소의 Go production 함수가 baseline 없이 기본 function budget gate를 통과하게 만듭니다.

최종 상태는 아래 네 가지를 동시에 만족해야 합니다.

1. `docs/architecture/go-function-budget-baseline.txt`가 삭제되어 있습니다.
2. `scripts/architecture/check-function-budget.py`에 baseline 개념이 없습니다.
3. `scripts/architecture/check-function-budget.sh`가 baseline 인자를 넘기지 않습니다.
4. 모든 production Go 함수가 `lines <= 60`, `complexity <= 8`, `nesting <= 5`를 만족합니다.

## Big-Bang 의미

여기서 big-bang은 작업을 한 사람이 한 번에 끝내라는 뜻이 아닙니다. 내부 작업은 아주 잘게 나눕니다. 다만 main에 들어가는 최종 PR은 baseline 제거, checker 전환, 모든 함수 리팩터링이 함께 들어가야 합니다.

중간 단계에서 baseline 파일만 줄이거나, 일부 module만 strict gate로 바꾸거나, threshold를 올리는 방식은 금지입니다.

## LLM에게 문서를 주는 방식

Worker LLM에게 전체 문서를 주지 마십시오. Worker에게는 다음 3개만 줍니다.

```text
03-prompts/worker-prompt.md
05-shards/<해당 shard>.md
04-patterns/<필요한 pattern>.md
```

Reviewer LLM에게는 다음 3개만 줍니다.

```text
03-prompts/reviewer-prompt.md
05-shards/<해당 shard>.md
06-review/reviewer-checklist.md
```

Validator LLM에게는 다음 2개만 줍니다.

```text
03-prompts/validator-prompt.md
07-validation/final-validation.md
```
