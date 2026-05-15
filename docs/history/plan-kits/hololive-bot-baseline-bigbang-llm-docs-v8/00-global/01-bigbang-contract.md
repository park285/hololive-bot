# 01 — Big-Bang 계약

## 최종 PR의 계약

최종 PR은 다음을 모두 포함해야 합니다.

- baseline 파일 삭제.
- baseline loader/writer 제거.
- report mode가 있는 strict checker 유지.
- 모든 over-budget 함수 리팩터링.
- baseline 예외 정책 문서 제거.
- 전체 architecture gate 통과.

## 중간 local commit 허용 범위

작업 branch 안에서는 중간 commit을 만들 수 있습니다. 하지만 다음 상태는 main에 merge하면 안 됩니다.

- baseline 파일이 아직 존재하는 상태.
- baseline 파일만 삭제했지만 over-budget 함수가 남아 있는 상태.
- checker 기준값을 완화한 상태.
- scanner exclude로 특정 path를 숨긴 상태.
- LOC threshold를 올려서 파일 분리 문제를 숨긴 상태.

## Manager의 판단 기준

Manager는 각 shard를 merge하기 전에 그 shard의 prefix report가 깨끗한지 확인합니다. 전체 `over_budget=0`이 되기 전까지는 final PR을 만들지 않습니다.
