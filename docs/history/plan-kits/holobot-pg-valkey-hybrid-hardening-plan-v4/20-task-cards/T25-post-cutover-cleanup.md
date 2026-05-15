# T25. Post-cutover cleanup

## 목적

pg_first/pg가 안정화된 뒤 legacy path와 shadow 문서를 정리합니다.

## 작업

1. legacy active Valkey queue publish 경로를 더 이상 production default로 두지 않습니다.
2. shadow mode는 관측/rollback 목적만 문서화합니다.
3. old outbox 또는 deprecated docs를 archive합니다.
4. metric dashboard 기준값을 갱신합니다.

## 완료 기준

- production runbook 기본 경로는 pg_first/pg입니다.
- legacy Valkey queue가 durable queue로 오해되지 않습니다.
- cleanup 후에도 rollback 절차는 남아 있습니다.

## LLM 프롬프트

post-cutover 문서를 정리하십시오. legacy queue를 source of truth처럼 설명하는 문구를 제거하십시오.
