package pollers

import (
	"context"
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
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return false
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
	err := pgxscan.Get(ctx, db, &watermark, mustSQL("pgx_helpers_0077_01.sql"),
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
