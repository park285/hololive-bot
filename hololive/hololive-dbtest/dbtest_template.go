package dbtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type templateState struct {
	once sync.Once
	name string
	err  error
}

var sharedTemplates = struct {
	mu    sync.Mutex
	byKey map[string]*templateState
}{byKey: make(map[string]*templateState)}

func NewPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()

	baseDSN := acquireBaseDSN(tb)
	templateName := acquireMigrationTemplate(tb, baseDSN)

	return newIsolatedPool(tb, baseDSN, templateName)
}

func acquireMigrationTemplate(tb testing.TB, baseDSN string) string {
	tb.Helper()

	files, err := readMigrations()
	if err != nil {
		tb.Fatalf("dbtest: fingerprint migrations: %v", err)
	}

	digest := sha256.New()

	for _, file := range files {
		for _, part := range [...]string{file.Checksum, file.Name, "\x00", file.Contract, "\x00"} {
			if _, err := io.WriteString(digest, part); err != nil {
				tb.Fatalf("dbtest: fingerprint %s: %v", file.Name, err)
			}
		}
	}

	fingerprint := hex.EncodeToString(digest.Sum(nil))
	key := baseDSN + "\x00" + fingerprint

	sharedTemplates.mu.Lock()

	state := sharedTemplates.byKey[key]
	if state == nil {
		state = &templateState{}
		sharedTemplates.byKey[key] = state
	}

	sharedTemplates.mu.Unlock()

	state.once.Do(func() {
		state.name, state.err = prepareMigrationTemplate(baseDSN, fingerprint)
	})

	if state.err != nil {
		tb.Fatalf("dbtest: prepare migration template: %v", state.err)
	}

	return state.name
}

func prepareMigrationTemplate(baseDSN, fingerprint string) (name string, err error) {
	name = "dbtest_template_" + fingerprint[:16]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	defer cancel()

	adminPool, err := poolForDatabase(ctx, baseDSN, "")
	if err != nil {
		return "", fmt.Errorf("connect template admin: %w", err)
	}
	defer adminPool.Close()

	conn, err := adminPool.Acquire(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire template lock connection: %w", err)
	}
	defer conn.Release()

	unlock, err := lockMigrationTemplate(ctx, conn, name)
	if err != nil {
		return "", fmt.Errorf("lock migration template: %w", err)
	}

	defer func() {
		cleanupCtx, cleanupCancel := migrationCleanupContext(ctx)
		defer cleanupCancel()

		err = errors.Join(err, unlock(cleanupCtx))
	}()

	out, err := prepareMigrationTemplateBody(ctx, conn, baseDSN, name, fingerprint)
	if err != nil {
		return out, fmt.Errorf("prepare migration template body: %w", err)
	}

	return out, nil
}

func lockMigrationTemplate(ctx context.Context, conn *pgxpool.Conn, name string) (func(context.Context) error, error) {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", name); err != nil {
		return nil, fmt.Errorf("lock migration template: %w", err)
	}

	return func(ctx context.Context) error {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", name); err != nil {
			return fmt.Errorf("unlock migration template: %w", err)
		}

		return nil
	}, nil
}

func prepareMigrationTemplateBody(ctx context.Context, conn *pgxpool.Conn, baseDSN, name, fingerprint string) (resultName string, err error) {
	templateCreated := false

	defer func() {
		cleanupCtx, cleanupCancel := migrationCleanupContext(ctx)
		defer cleanupCancel()

		err = cleanupMigrationTemplate(cleanupCtx, baseDSN, name, templateCreated, err)
	}()

	ready, err := migrationTemplateReady(ctx, conn, name, fingerprint)
	if err != nil {
		return "", fmt.Errorf("migration template ready: %w", err)
	}

	if ready {
		return name, nil
	}

	if err := resetMigrationTemplate(ctx, conn, baseDSN, name); err != nil {
		return "", fmt.Errorf("reset migration template: %w", err)
	}

	templateCreated = true

	if err := applyMigrationTemplate(ctx, baseDSN, name); err != nil {
		return "", fmt.Errorf("apply migration template: %w", err)
	}

	comment := "dbtest-template:" + fingerprint
	if _, err := conn.Exec(ctx, fmt.Sprintf("COMMENT ON DATABASE %s IS '%s'", quoteIdent(name), comment)); err != nil {
		return "", fmt.Errorf("mark migration template complete: %w", err)
	}

	templateCreated = false

	return name, nil
}

func cleanupMigrationTemplate(ctx context.Context, baseDSN, name string, templateCreated bool, err error) error {
	if !templateCreated || err == nil {
		return err
	}

	cleanupErr := dropDatabase(ctx, baseDSN, name)
	if cleanupErr != nil {
		return errors.Join(err, fmt.Errorf("cleanup incomplete migration template: %w", cleanupErr))
	}

	return err
}

func migrationCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}

func resetMigrationTemplate(ctx context.Context, conn *pgxpool.Conn, baseDSN, name string) error {
	if err := dropDatabase(ctx, baseDSN, name); err != nil {
		return fmt.Errorf("drop incomplete migration template: %w", err)
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(name))); err != nil {
		return fmt.Errorf("create migration template: %w", err)
	}

	return nil
}

func applyMigrationTemplate(ctx context.Context, baseDSN, name string) error {
	templatePool, err := poolForDatabase(ctx, baseDSN, name)
	if err != nil {
		return fmt.Errorf("connect migration template: %w", err)
	}
	defer templatePool.Close()

	if err := ApplyMigrations(ctx, templatePool); err != nil {
		return fmt.Errorf("apply migration template: %w", err)
	}

	return nil
}

func migrationTemplateReady(ctx context.Context, conn *pgxpool.Conn, name, fingerprint string) (bool, error) {
	var comment *string

	err := conn.QueryRow(ctx, `
		SELECT shobj_description(oid, 'pg_database')
		FROM pg_database
		WHERE datname = $1`, name).Scan(&comment)

	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("inspect migration template: %w", err)
	}

	return comment != nil && *comment == "dbtest-template:"+fingerprint, nil
}
