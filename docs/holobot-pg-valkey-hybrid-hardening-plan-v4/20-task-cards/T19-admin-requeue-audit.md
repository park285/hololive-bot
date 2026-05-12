# T19. Admin requeue audit

## 목적

quarantined/sending ambiguous row를 운영자가 실수로 중복 발송하지 않게 합니다.

## 작업

1. manual requeue는 `force_duplicate_risk_ack=true`가 필요합니다.
2. admin action audit row를 남깁니다.
3. terminal row requeue는 기본 금지입니다.
4. quarantined row requeue는 reason 필수입니다.

## 완료 기준

- ack 없이 requeue 실패.
- audit row에 operator, reason, from_status, to_status가 남습니다.
- runbook에 duplicate risk가 명시됩니다.

## LLM 프롬프트

manual requeue tooling을 안전하게 보강하십시오. ambiguous send outcome은 자동 replay하지 마십시오.
