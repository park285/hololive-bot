package durability

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/sqlite/sqlitedb"
)

//go:embed migrations/*.sql
var migrationAssets embed.FS

const (
	storeReadConns          = 4
	storeConnMaxIdleTime    = 30 * time.Second
	storeBusyTimeout        = 5 * time.Second
	storeJournalSizeLimit   = 64 << 20
	storeCheckpointInterval = 5 * time.Minute
)

type Store struct {
	db  *sqlitedb.DB
	now func() time.Time
}

func StoreOptions(path string, logger *slog.Logger) sqlitedb.Options {
	return sqlitedb.Options{
		Path:               path,
		ReadConns:          storeReadConns,
		ConnMaxIdleTime:    storeConnMaxIdleTime,
		BusyTimeout:        storeBusyTimeout,
		JournalSizeLimit:   storeJournalSizeLimit,
		CheckpointInterval: storeCheckpointInterval,
		Logger:             logger,
	}
}

func OpenStore(ctx context.Context, opts sqlitedb.Options) (*Store, error) {
	migrations, err := sqlitedb.LoadMigrations(migrationAssets, "migrations")
	if err != nil {
		return nil, fmt.Errorf("durability: load migrations: %w", err)
	}

	db, err := sqlitedb.Open(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("durability: open store: %w", err)
	}

	if err := db.Migrate(ctx, migrations); err != nil {
		return nil, errors.Join(fmt.Errorf("durability: migrate store: %w", err), db.Close(ctx))
	}

	return &Store{db: db, now: time.Now}, nil
}

func AttachStore(ctx context.Context, opts sqlitedb.Options) (*Store, error) {
	db, err := sqlitedb.Attach(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("durability: attach store: %w", err)
	}

	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close(ctx context.Context) error {
	if err := s.db.Close(ctx); err != nil {
		return fmt.Errorf("durability: close store: %w", err)
	}

	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.Reader().PingContext(ctx); err != nil {
		return fmt.Errorf("durability: ping store: %w", err)
	}

	return nil
}

func (s *Store) WithClock(now func() time.Time) *Store {
	return &Store{db: s.db, now: now}
}

func (s *Store) write(ctx context.Context, fn func(tx *sql.Tx, now int64) error) error {
	if err := s.db.WriteTx(ctx, func(tx *sql.Tx) error {
		return fn(tx, s.now().UnixMilli())
	}); err != nil {
		return fmt.Errorf("durability: write: %w", err)
	}

	return nil
}

func (s *Store) read() *sql.DB {
	return s.db.Reader()
}

func (s *Store) nowMS() int64 {
	return s.now().UnixMilli()
}

func timeFromMS(ms int64) time.Time {
	return time.UnixMilli(ms)
}

func rowsAffected(result sql.Result) (int64, error) {
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	return n, nil
}
