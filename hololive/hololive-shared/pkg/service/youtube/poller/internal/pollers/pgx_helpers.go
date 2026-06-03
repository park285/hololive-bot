package pollers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type pollerDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func normalizePollerDB(db any) pollerDB {
	if isNilPollerDB(db) {
		return nil
	}
	if typed, ok := db.(pollerDB); ok {
		return typed
	}
	return nil
}

func isNilPollerDB(db any) bool {
	if db == nil {
		return true
	}
	value := reflect.ValueOf(db)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func loadContentWatermark(
	ctx context.Context,
	db dbx.Querier,
	channelID string,
	watermarkType domain.WatermarkType,
) (domain.YouTubeContentWatermark, bool, error) {
	if db == nil {
		return domain.YouTubeContentWatermark{}, false, fmt.Errorf("load %s watermark: db is nil", watermarkType)
	}

	var watermark domain.YouTubeContentWatermark
	err := pgxscan.Get(ctx, db, &watermark, `
		SELECT channel_id, watermark_type, initialized, last_content_id, updated_at
		FROM youtube_content_watermarks
		WHERE channel_id = $1 AND watermark_type = $2`,
		channelID,
		watermarkType,
	)
	if err != nil {
		if pgxscan.NotFound(err) {
			return domain.YouTubeContentWatermark{}, false, nil
		}
		return domain.YouTubeContentWatermark{}, false, fmt.Errorf("load %s watermark: %w", watermarkType, err)
	}
	return watermark, watermark.Initialized, nil
}

func inPollerTx(ctx context.Context, db pollerDB, fn func(tx dbx.Querier) error) error {
	if db == nil {
		return fmt.Errorf("pgx db is nil")
	}
	if fn == nil {
		return nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin pgx transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	return finishPollerTx(ctx, tx, fn(tx))
}

func finishPollerTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("pgx transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}
		return fmt.Errorf("pgx transaction failed: %w", fnErr)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pgx transaction: %w", err)
	}
	return nil
}
