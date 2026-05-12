# TASK-D0-02. current/history 문서 분리

## Phase

D0. 문서 기준선 복구

## 목표

`docs/current`에는 현재 운영 기준만 남기고, 완료/이력 문서는 `docs/history`로 이동합니다.

## 왜 필요한가

`docs/current/README.md`는 현재 운영 기준 문서만 둔다고 말하지만, current 안에는 `CLOSED / HISTORICAL` 상태 문서가 섞여 있습니다. 이 상태는 LLM에게 현재 기준과 과거 기록을 혼동시킵니다.

## 먼저 읽을 파일

- `docs/current/README.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`
- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md`
- `docs/current/RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md`
- `docs/history/README.md`

## 수정 또는 생성할 파일

- `docs/current/README.md`
- `docs/history/alarm-dispatch/ALARM_DISPATCH_REMEDIATION_20260414.md`
- `docs/history/runtime-split/RUNTIME_SPLIT_HANDOFF_20260416.md`
- `docs/history/runtime-split/RUNTIME_SPLIT_PR07_BLOCKERS_20260416.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md 또는 bridge 파일`
- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md 또는 bridge 파일`

## 작업 단계

1. historical 성격 문서를 history 하위로 이동합니다.
2. 기존 경로 참조가 깨질 가능성이 있으면 current 위치에 짧은 bridge 문서를 남깁니다.
3. current README에서 historical 문서를 현재 기준 문서처럼 나열하지 않습니다.
4. history README에 새 위치를 등록합니다.
5. current에 남긴 bridge 문서는 '원문은 history로 이동'만 말하고 운영 기준을 담지 않습니다.

## 금지 사항

- historical 문서 내용을 새로 해석하지 마십시오.
- 과거 문서를 삭제하지 마십시오.
- 현재 운영 기준을 historical 문서에서 그대로 복사하지 마십시오. 필요한 내용은 별도 current 기준 문서에서 요약합니다.

## 완료 조건

- `docs/current`에 `CLOSED / HISTORICAL` 본문 문서가 남아 있지 않습니다.
- `docs/history`에서 과거 문서를 찾을 수 있습니다.
- current README는 현재 기준 문서만 나열합니다.
- 기존 링크 호환성이 필요한 경우 bridge가 존재합니다.

## 검증 명령

```bash
find docs/current -name '*.md' -print | xargs grep -n 'CLOSED / HISTORICAL' || true
./scripts/architecture/check-project-map.sh
```

## 주의할 리스크

- 기존 문서 링크가 깨질 수 있으므로 bridge 파일을 신중하게 둡니다.

## LLM 작업 프롬프트

```text
Task TASK-D0-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
