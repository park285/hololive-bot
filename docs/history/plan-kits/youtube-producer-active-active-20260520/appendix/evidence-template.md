# Evidence Template

Use this template after each phase. Do not claim success without fresh evidence.

## Header

- Phase:
- Date/time:
- Host:
- Branch/commit:
- Operator:
- Live system touched: none/read-only/mutation
- Live mutation performed: yes/no
- Live mutation approval: approved/not approved/n/a; evidence:
- Required approval for live mutation or sensitive access: approved/not approved/n/a; scope/evidence:
- If blocked by missing approval, blocked operation or access:

## Commands

```bash
# command 1
```

Exit code:

Important output:

```text
paste or summarize key lines
```

## Checks

| Check | Result | Evidence |
|---|---|---|
| compose render | pass/fail/not run | command/output |
| targeted tests | pass/fail/not run | command/output |
| `/ready` AP-A | pass/fail/not run | payload |
| `/ready` AP-B | pass/fail/not run | payload |
| metrics | pass/fail/not run | series/query |
| log scan | pass/fail/not run | command/output |
| duplicate SQL | pass/fail/not run | query result |

## Findings

- Completed:
- Blocked:
- Inconclusive:
- Follow-up:

## Completion Claim

Use one:

- “This phase is complete. Evidence: ...”
- “This phase is blocked by ...”
- “This phase is inconclusive because ...”

If any required approval is missing, including live mutation, env modification, secret read/use/write, OpenBao KV write, or authenticated metrics access, name the blocked operation or access and do not use the complete claim.
