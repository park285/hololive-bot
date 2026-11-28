package pgimport

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/durability"
	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlAssets embed.FS

var (
	mustSQL   = sqlassets.MustStatementReader(sqlAssets, "queries")
	sourceSQL = map[durability.ImportTable]pgxdb.Statement{
		durability.ImportWebhookInbox:         mustSQL("webhook_inbox.sql"),
		durability.ImportWebhookHeads:         mustSQL("webhook_heads.sql"),
		durability.ImportCommandExecutions:    mustSQL("command_executions.sql"),
		durability.ImportReplyOutbox:          mustSQL("reply_outbox.sql"),
		durability.ImportReplyReplayAudit:     mustSQL("reply_replay_audit.sql"),
		durability.ImportReplyResolutionAudit: mustSQL("reply_resolution_audit.sql"),
		durability.ImportWebhookNonces:        mustSQL("webhook_nonces.sql"),
	}
	summarySQL = mustSQL("summary.sql")
)

type Source interface {
	BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error)
}

func Copy(ctx context.Context, source Source, target *durability.Store) (durability.ImportSummary, error) {
	rows, summary, err := readSnapshot(ctx, source)
	if err != nil {
		return nil, err
	}

	if err := target.Import(ctx, rows, summary); err != nil {
		return nil, fmt.Errorf("pgimport: %w", err)
	}

	return summary, nil
}

func readSnapshot(ctx context.Context, source Source) (_ map[durability.ImportTable][][]any, _ durability.ImportSummary, err error) {
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, fmt.Errorf("pgimport: begin snapshot: %w", err)
	}

	defer pgxdb.RollbackDeferred(ctx, tx, &err)

	rows := make(map[durability.ImportTable][][]any, len(sourceSQL))

	for _, table := range durability.ImportTables() {
		statement, ok := sourceSQL[table]
		if !ok {
			return nil, nil, fmt.Errorf("pgimport: no source query for %s", table)
		}

		tableRows, readErr := readTable(ctx, tx, statement)
		if readErr != nil {
			return nil, nil, fmt.Errorf("pgimport: read %s: %w", table, readErr)
		}

		rows[table] = tableRows
	}

	summary, err := readSummary(ctx, tx)
	if err != nil {
		return nil, nil, err
	}

	return rows, summary, nil
}

func readTable(ctx context.Context, tx pgx.Tx, statement pgxdb.Statement) ([][]any, error) {
	result, err := pgxdb.Query(ctx, tx, statement)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	rows, err := pgx.CollectRows(result, func(row pgx.CollectableRow) ([]any, error) {
		return row.Values()
	})
	if err != nil {
		return nil, fmt.Errorf("collect: %w", err)
	}

	return rows, nil
}

func readSummary(ctx context.Context, tx pgx.Tx) (durability.ImportSummary, error) {
	result, err := pgxdb.Query(ctx, tx, summarySQL)
	if err != nil {
		return nil, fmt.Errorf("pgimport: query summary: %w", err)
	}

	summary := durability.ImportSummary{}

	var (
		relation, bucket string
		count            int64
	)

	_, err = pgx.ForEachRow(result, []any{&relation, &bucket, &count}, func() error {
		summary.Add(relation, bucket, count)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pgimport: read summary: %w", err)
	}

	return summary, nil
}
