# 02 — 저장소 사실 기준

현재 작업은 다음 사실을 전제로 합니다.

- architecture boundary gate는 M4에서 Go module LOC, Go function budget, file LOC gate를 실행합니다.
- function budget shell wrapper는 현재 Python checker에 baseline 파일을 전달합니다.
- Python checker의 기본 기준은 `lines <= 60`, `complexity <= 8`, `nesting <= 5`입니다.
- baseline 파일은 `path:start_line:function:max_lines:max_complexity:max_nesting` 형식입니다.
- baseline entry는 기존 debt ceiling이므로, 파일을 삭제하려면 모든 해당 함수가 기본 기준을 통과해야 합니다.
- current CI gate 문서에 남아 있는 baseline 예외 정책도 제거해야 합니다.

이 사실 중 하나라도 바뀌었으면 Manager가 A00 이전에 문서를 갱신해야 합니다.
