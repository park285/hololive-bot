# Task 006. llm-scheduler flow

## 목표

LLM scheduler에서 prompt build, provider request, result validation, notification intent write를 분리한다.

## 금지

- prompt 원문 로그 금지
- provider response 전문 로그 금지
- API key/token 로그 금지

## 허용 필드

```text
provider
model
prompt_len
prompt_sha256_8
duration_ms
result_count
```
