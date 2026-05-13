# MANAGER-04 — Worker handoff 형식

Worker에게는 아래 형식으로 넘깁니다.

```text
당신은 hololive-bot baseline 제거 big-bang 작업의 Worker입니다.
전체 작업 중 하나의 micro-shard만 처리합니다.

읽을 문서:
- 03-prompts/worker-prompt.md
- 05-shards/<module>/<shard>.md
- 04-patterns/<관련 pattern>.md

수정 범위:
- <exact file/path>

대상 함수:
- <function>@<line>
- <function>@<line>

금지:
- baseline 관련 코드 수정/복구 금지
- threshold 완화 금지
- go.mod/go.sum 변경 금지

완료 후 보고:
- 변경 파일
- 리팩터링 요약
- 실행한 명령
- 남은 over-budget 여부
```

Worker에게 전체 v8 패키지를 통째로 주지 마십시오. 정보가 많으면 LLM이 다른 shard까지 건드립니다.
