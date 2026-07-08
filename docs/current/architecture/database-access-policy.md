# Database Access Policy

## Verified Baseline

On 2026-07-08, the active-code inventory command returned no matches:

```bash
rg -n 'gorm\.io|gorm\.DB|gorm\.Open|GetGormDB|AutoMigrate\(' --glob '*.go' --glob 'go.mod' hololive admin-dashboard scripts
```

That means current active code has no `gorm.io`, `gorm.DB`, `gorm.Open`,
`GetGormDB`, or `AutoMigrate(` usage in the owned Go source/module surfaces
covered by this policy. The current work is therefore a verification and
guardrail task, not a fake migration task.
The guard also scans the root `go.mod` module manifest so dependency
reintroduction cannot bypass the active source-tree scan.

## Policy

Hololive database access uses explicit SQL through `pgx`, `pgxpool`, and
`pgxscan`. The long-running GORM-removal direction remains:

- tests create schema from production migration SQL or a documented
  migration-derived schema subset before any future removal work;
- `pgxscan` is the preferred replacement for GORM-era row scanning;
- writes and transactions use explicit SQL and explicit transaction ownership;
- `sqlc` is not part of the GORM-removal path and may be considered only after
  the GORM-removal path is complete and separately documented.

## Allowed SQL Surfaces

Migration SQL remains the schema source of truth. Package-local SQL assets,
Go `//go:embed` query files, `pgx`/`pgxpool` calls, `pgxscan` row scanning, and
allowlisted SQL templates remain allowed. This policy does not ban SQL asset
embedding or reviewed SQL templates.

## Disallowed Defaults

Do not reintroduce broad ORM/query-framework defaults that hide or infer
database behavior. In active Go code and module manifests, the architecture
gate rejects:

- `gorm.io`
- `gorm.DB`
- `gorm.Open`
- `GetGormDB`
- `AutoMigrate(`
- `github.com/uptrace/bun`
- `entgo.io/ent`
- `github.com/go-gorm`

The banned behavior is hidden or implicit transaction boundaries, auto
migration as runtime or test schema source of truth, implicit association
loading, model inference, and broad ORM/query framework defaulting.
