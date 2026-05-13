# Failure smell catalog

Reviewer는 아래 냄새가 보이면 rework를 요청합니다.

## Gate 우회 냄새

- `DEFAULT_MAX_*` 값이 바뀜.
- `EXCLUDED_DIR_NAMES` 또는 file filter에 production path가 추가됨.
- `docs/architecture/go-function-budget-baseline.txt`가 다시 생김.
- `--baseline` 또는 `--write-baseline` 옵션이 남아 있음.
- file LOC threshold가 올라감.

## 나쁜 리팩터링 냄새

- 큰 함수 내용이 그대로 `doEverything` helper로 이동함.
- helper 이름이 의미 없이 `part1`, `part2`임.
- 에러 메시지가 광범위하게 바뀜.
- test expected가 편의상 바뀜.
- scope 밖 module까지 건드림.
- `go.mod` 또는 `go.sum`이 변경됨.

## behavior drift 냄새

- HTTP status가 달라짐.
- JSON field 이름이 달라짐.
- DB query bind order가 바뀜.
- cache key 또는 queue key가 바뀜.
- retry attempt 증가 위치가 바뀜.
- context done 처리 위치가 뒤로 밀림.
