# Non-secret Operational History Risk Decisions

이 문서는 current tree 통제와 과거 Git history의 reconnaissance metadata를 구분합니다.
과거 commit을 수정하거나 remote object를 삭제하는 작업은 이 결정의 범위가 아닙니다.

## #087 — Osaka topology history

Decision: accept the non-secret Git-history reconnaissance risk.

- Current paths under `docs/agent-workflows/` and `docs/history/plan-kits/` are
  excluded by repository `.gitignore` rules and are not tracked in the current index.
- The three retained workstation-local copies are public-safe summary/evidence
  stubs. A bounded static scan found no private-key header, common provider token
  form, or credential-like assigned value.
- The old public history may still reveal non-secret topology categories. That
  reconnaissance value is accepted because no current secret or live identifier
  was found in the reviewed current paths.
- No history rewrite, credential or endpoint rotation, or remote deletion is authorized by this decision.
- If a real secret is later identified, this acceptance is void and the finding
  moves to a separately authorized incident-response workflow.

## #088 — Rollout evidence history

Decision: accept the non-secret Git-history reconnaissance risk.

- This finding is recorded separately even though it cites the same three current
  paths, because the input finding and its closure evidence remain one-to-one.
- Current paths under `docs/agent-workflows/` and `docs/history/plan-kits/` are
  excluded by repository `.gitignore` rules and are not tracked in the current index.
- The retained local evidence is reduced to public-safe stubs. The same bounded
  static scan found no actual credential value; old history contains operational
  categories rather than known secret material.
- No history rewrite, credential or endpoint rotation, or remote deletion is authorized by this decision.
- If a real secret is later identified, this acceptance is void and the finding
  moves to a separately authorized incident-response workflow.
