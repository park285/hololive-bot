# MANAGER-02 — Inventory와 ledger 생성

A00/A01을 적용한 뒤 최신 over-budget 목록을 만듭니다.

```bash
mkdir -p artifacts/function-budget-baseline-removal
python3 scripts/architecture/check-function-budget.py \
  --root . \
  --report-over-budget \
  --output json \
  --sort-by score \
  > artifacts/function-budget-baseline-removal/over-budget.json

python3 docs/hololive-bot-baseline-bigbang-llm-docs-v8/tools/generate-function-budget-shards.py \
  --input artifacts/function-budget-baseline-removal/over-budget.json \
  --output-dir artifacts/function-budget-baseline-removal
```

생성 파일은 다음입니다.

```text
artifacts/function-budget-baseline-removal/summary-by-module.tsv
artifacts/function-budget-baseline-removal/summary-by-package.tsv
artifacts/function-budget-baseline-removal/summary-by-file.tsv
artifacts/function-budget-baseline-removal/shard-ledger.tsv
artifacts/function-budget-baseline-removal/auto-shard-cards.md
```

## Ledger 상태값

```text
open        작업 전
assigned    Worker 배정됨
needs_review Worker 완료, Reviewer 대기
rework      Reviewer가 재작업 요청
verified    prefix report와 package test 통과
done        final validation에서도 통과
```

## Shard 분배 원칙

정적 shard 문서는 시작점입니다. 실제 최신 inventory에서 한 파일에 over-budget 함수가 5개를 넘으면 auto-shard 기준으로 쪼갭니다.

R4/R5 shard는 1~2개 함수 단위로 제한합니다.
