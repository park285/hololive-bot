# Database Access Policy

## Verified Baseline

Active Go source and module manifests have no `gorm.io`, `gorm.DB`,
`gorm.Open`, `GetGormDB`, or `AutoMigrate(` usage. Re-check the owned surfaces
with:

```bash
rg -n 'gorm\.io|gorm\.DB|gorm\.Open|GetGormDB|AutoMigrate\(' --glob '*.go' --glob 'go.mod' go.mod hololive scripts
```

## Workspace Scope Note

This inventory covers the root `go.mod` and owned in-repo Go surfaces under
`hololive` and `scripts`. Sibling modules referenced by `go.work`, such as
`../shared-go` and `../iris-client-go`, follow their own repository policy.

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
database behavior. Active Go code and module manifests must not use (enforced
in review and dependency approval; there is no grep gate):

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
