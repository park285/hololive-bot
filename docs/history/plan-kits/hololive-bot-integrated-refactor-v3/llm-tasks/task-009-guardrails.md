# Task 009. guardrails

## 목표

리팩토링 중 실수로 admin 영역을 수정하거나 민감 로그를 추가하지 못하게 한다.

## 스크립트

- `scripts/refactor/validate-no-admin-touch.sh`
- `scripts/refactor/grep-sensitive-logs.sh`
- `scripts/refactor/test-non-admin-go.sh`

## 검증

```bash
chmod +x scripts/refactor/*.sh
./scripts/refactor/validate-no-admin-touch.sh
./scripts/refactor/grep-sensitive-logs.sh
./scripts/refactor/test-non-admin-go.sh
```
