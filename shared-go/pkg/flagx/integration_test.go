//go:build integration

package flagx_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/flagx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgresContainer(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		container.Terminate(ctx)
		t.Fatalf("failed to create pool: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS user_flags (
			entity_id   TEXT NOT NULL,
			flag        TEXT NOT NULL,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_by  TEXT NOT NULL,
			PRIMARY KEY (entity_id, flag)
		)
	`)
	if err != nil {
		pool.Close()
		container.Terminate(ctx)
		t.Fatalf("failed to create table: %v", err)
	}

	cleanup := func() {
		pool.Close()
		container.Terminate(ctx)
	}

	return pool, cleanup
}

func TestPostgresRepository_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	repo, err := flagx.NewPostgresRepository(pool, "user_flags")
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}

	ctx := context.Background()
	entityID := "user123"
	flag := flagx.Flag("premium")
	createdBy := "trace-abc"

	t.Run("Set creates new flag", func(t *testing.T) {
		err := repo.Set(ctx, entityID, flag, createdBy)
		if err != nil {
			t.Errorf("Set() error = %v", err)
		}
	})

	t.Run("Has returns true for existing flag", func(t *testing.T) {
		has, err := repo.Has(ctx, entityID, flag)
		if err != nil {
			t.Errorf("Has() error = %v", err)
		}
		if !has {
			t.Error("Has() = false, want true")
		}
	})

	t.Run("Has returns false for non-existing flag", func(t *testing.T) {
		has, err := repo.Has(ctx, entityID, "nonexistent")
		if err != nil {
			t.Errorf("Has() error = %v", err)
		}
		if has {
			t.Error("Has() = true, want false")
		}
	})

	t.Run("Set is idempotent", func(t *testing.T) {
		err := repo.Set(ctx, entityID, flag, "new-trace")
		if err != nil {
			t.Errorf("Set() error = %v", err)
		}
	})

	t.Run("List returns all flags for entity", func(t *testing.T) {
		err := repo.Set(ctx, entityID, "verified", createdBy)
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		records, err := repo.List(ctx, entityID)
		if err != nil {
			t.Errorf("List() error = %v", err)
		}
		if len(records) != 2 {
			t.Errorf("List() returned %d records, want 2", len(records))
		}
	})

	t.Run("ListByFlag returns all entities with flag", func(t *testing.T) {
		err := repo.Set(ctx, "user456", flag, createdBy)
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		records, err := repo.ListByFlag(ctx, flag)
		if err != nil {
			t.Errorf("ListByFlag() error = %v", err)
		}
		if len(records) != 2 {
			t.Errorf("ListByFlag() returned %d records, want 2", len(records))
		}
	})

	t.Run("Unset removes flag", func(t *testing.T) {
		err := repo.Unset(ctx, entityID, flag)
		if err != nil {
			t.Errorf("Unset() error = %v", err)
		}

		has, err := repo.Has(ctx, entityID, flag)
		if err != nil {
			t.Errorf("Has() error = %v", err)
		}
		if has {
			t.Error("Has() = true after Unset, want false")
		}
	})

	t.Run("Unset is idempotent", func(t *testing.T) {
		err := repo.Unset(ctx, entityID, flag)
		if err != nil {
			t.Errorf("Unset() error = %v", err)
		}
	})

	t.Run("FlagRecord has correct fields", func(t *testing.T) {
		records, err := repo.List(ctx, entityID)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(records) == 0 {
			t.Skip("no records to check")
		}

		rec := records[0]
		if rec.EntityID != entityID {
			t.Errorf("EntityID = %q, want %q", rec.EntityID, entityID)
		}
		if rec.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
		if time.Since(rec.CreatedAt) > time.Minute {
			t.Error("CreatedAt is too old")
		}
	})
}

func TestPostgresRepository_Concurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	repo, err := flagx.NewPostgresRepository(pool, "user_flags")
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}

	ctx := context.Background()
	entityID := "concurrent_user"
	flag := flagx.Flag("test_flag")

	const goroutines = 10
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			err := repo.Set(ctx, entityID, flag, "trace-"+string(rune('a'+id)))
			errCh <- err
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent Set() error = %v", err)
		}
	}

	has, err := repo.Has(ctx, entityID, flag)
	if err != nil {
		t.Errorf("Has() error = %v", err)
	}
	if !has {
		t.Error("Has() = false after concurrent Sets, want true")
	}
}
