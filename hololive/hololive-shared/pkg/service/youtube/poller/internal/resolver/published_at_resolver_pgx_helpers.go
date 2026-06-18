package resolver

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/kapu/hololive-shared/internal/dbx"
)

type publishedAtResolverDB interface {
	dbx.Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func normalizePublishedAtResolverDB(db any) publishedAtResolverDB {
	if isNilPublishedAtResolverDB(db) {
		return nil
	}
	if typed, ok := db.(publishedAtResolverDB); ok {
		return typed
	}
	return nil
}

func isNilPublishedAtResolverDB(db any) bool {
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

func inPublishedAtResolverTx(ctx context.Context, db publishedAtResolverDB, fn func(tx dbx.Querier) error) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if fn == nil {
		return nil
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
				panic(fmt.Errorf("panic during transaction and rollback failed: %w", errors.Join(fmt.Errorf("%v", p), rollbackErr)))
			}
			panic(p)
		}
	}()

	return finishPublishedAtResolverTx(ctx, tx, fn(tx))
}

func finishPublishedAtResolverTx(ctx context.Context, tx pgx.Tx, fnErr error) error {
	if fnErr != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			return fmt.Errorf("transaction failed and rollback failed: %w", errors.Join(fnErr, rollbackErr))
		}
		return fnErr
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
