# Inventory schema

`check-function-budget.py --output json`의 item은 아래 필드를 갖습니다.

```json
{
  "path": "relative/path.go",
  "name": "FunctionName",
  "line": 123,
  "lines": 61,
  "complexity": 9,
  "nesting": 3,
  "key": "relative/path.go:123:FunctionName",
  "score": 11,
  "exceeded": {
    "lines": {"actual": 61, "limit": 60}
  }
}
```

Manager는 `path`와 `risk`를 기준으로 micro-shard를 만듭니다.
