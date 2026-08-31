# PR-12 RC / PR-level DoD record

> Historical verification capture. Do not use as a current CI or runtime contract.

`HEAD` `81883b7fad0483a7b1563779b294073d450fac76`. Working tree is dirty with uncommitted PR-00..PR-12. Production change performed: false.

This is a record. It is not a fake green.

## §28.1 PR-level DoD

| Item | Record |
|---|---|
| baseline SHA, merge base, related drift | `baseline_sha` = `merge_base_sha` = `head_sha` = `81883b7fad0483a7b1563779b294073d450fac76` (branch `HEAD` is origin/main changelog-only). Behavior lives in the dirty tree. |
| FACT/invariant/test traceability | `traceability.json` is an inventory of §23.1 IDs, not a claim that every test name matches |
| §19 file/symbol manifest vs diff | not re-audited as a freeze PR; PR-12 is link/drift/cleanup |
| before/red evidence | inherited from PR-00..11 on this tree; PR-12 did not add behavior |
| public API/SQL freeze | PR-12 did not change freeze APIs |
| success/failure/cancel/cleanup/concurrency tests | not re-run as a full matrix this session |
| impossible state constructors | not in PR-12 scope |
| no new arbitrary string / unbounded / mutable getter | PR-12 did not add APIs |
| DB terminal proof | not executed |
| config/deploy/docs with behavior PR | docs/link cleanup only |
| default/race/sonic/Node/DB/deploy applicable gates | subset executed; race/sonic/local-ci unexecuted |
| unexpected skip and unexecuted blocking command 0 | **not met**. `results.unexecuted` > 0 |
| evidence manifest and raw log hash | filled for commands actually run |
| secret scan | not a dedicated scanner run; evidence files contain no credentials by construction |
| exact rollback revision and mixed-version impact | recorded in `docs/current/runbooks/rollback.md`; production not executed |
| no production deploy/restart/secret/DB write | true |
| `git diff --check` / architecture/link/contract gates | `git diff --check` in the PR-12 command list; full architecture gate not run |
| unresolved design markers | none introduced by PR-12 |

## §28.3 release-candidate DoD

| Item | Record |
|---|---|
| PR-00..PR-12 merged | **not met**. Uncommitted on this branch |
| FACT-01..40 re-checked on latest main | **not met**. Not re-walked this session |
| closed-by-baseline findings with commits | n/a for this dirty tree |
| P0/P1 unresolved 0 | not independently re-triaged |
| applicable P2 blocking 0 | not independently re-triaged |
| helper 100-cycle leak gate | unexecuted this session |
| proxy request / one-request cancel isolation | unexecuted this session |
| closed error/status/retry vocabulary | inherited; not re-run full ERR/RPC suite |
| PublishBatchAndDefer atomicity | unexecuted this session |
| stale/expired fence side effect 0 | unexecuted this session |
| no-data Complete / ordinary Defer current target | unexecuted this session |
| every acquired lease terminal or verified fence loss | unexecuted this session |
| canonical JobContract duplication removed | inherited; not re-run TGT suite |
| TargetSnapshot query-plan gate | live EXPLAIN unexecuted |
| scheduler queue/fairness/fatal tests | unexecuted this session |
| readiness nil fail-closed / handoff recovery | inherited; collectorruntime tests in the PR-12 list |
| readiness does not claim freshness | inherited |
| pagination tuple | unexecuted this session |
| provider HTTP bounded / credential leak 0 | unexecuted this session |
| collector Valkey dependency 0; other consumers unchanged | CFG leftover test + render asserts this session; full CFG-009 matrix is in settings tests |
| actual `CGO_ENABLED=0 -tags sonic` | **unexecuted** |
| Docker/native artifact same revision/protocol/lock | **image digest unexecuted** |
| full repository gate green; unexpected skip/unexecuted 0 | **not met**. `scripts/ci/local-ci.sh` unexecuted |
| canary/rollback change record draft at exact revision | draft in `docs/current/runbooks/rollback.md`. Production canary not executed |

`results.passed` in `manifest.json` is a mixed count of `[PASS]` lines, hardening-contract `OK` rows, and Go package `ok` lines from commands that actually ran. It is not a go-test-json parser total.

Traceability keys `live_explain`, `image_digest`, `go_race`, `go_sonic`, and `local_ci` use status `blocked` (contract §23.5). They are applicable and were not run. `results.unexecuted` remains 6 for the commands below.

## Unexecuted blocking commands

1. `scripts/ci/local-ci.sh`
2. `go test -race` (collector / shared)
3. `CGO_ENABLED=0 go test -tags sonic`
4. live `EXPLAIN` for exact-target snapshot
5. live `EXPLAIN` for projection-target snapshot
6. Docker image digest / collector image build
