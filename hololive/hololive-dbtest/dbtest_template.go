// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbtest

import (
	"context"
	"errors"
	"fmt"
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
	sync.Mutex
	byKey map[string]*templateState
}{byKey: make(map[string]*templateState)}

// NewPool은 격리된 데이터베이스를 가진 *pgxpool.Pool을 반환한다.
//
// 동작:
//   - TEST_DATABASE_URL이 있으면 그 DSN을, 없으면 testcontainers ephemeral PG를 base로 쓴다.
//     base는 sync.Once로 테스트 바이너리당 1회만 확보된다(컨테이너 재기동 없음).
//   - migration-complete template은 manifest fingerprint별로 한 번 만들고, 호출마다
//     고유 데이터베이스(test_<unique>)를 template clone으로 생성한다.
//   - t.Cleanup에 DROP DATABASE와 pool close를 등록한다.
//
// manifest 전체가 빈 DB에서 재생되는 이유: 006-base-runtime-tables.sql이 레거시
// 초기 DB 생성 경로의 base 테이블(members, alarms 등)을 manifest 최초 단계에서
// 멱등 복원한다. 따라서 과거의 base-schema gap이 사라졌고 manifest 전체 chain을
// 그대로 적용한다.
//
// per-schema가 아닌 per-database 격리를 쓰는 이유: prod migration 다수가 idempotent guard로
// information_schema를 table_schema 한정 없이 조회한다(예: 037이 acl_rooms.list_type 존재 여부를
// 전체 카탈로그에서 확인). 단일 DB 내 여러 스키마로 격리하면 한 스키마의 변경이 다른 스키마의
// guard 판정을 오염시킨다. DB 단위로 격리하면 카탈로그가 완전히 분리되어 guard가 정확히 동작한다.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()

	baseDSN := acquireBaseDSN(t)
	templateName := acquireMigrationTemplate(t, baseDSN)
	return newIsolatedPool(t, baseDSN, templateName)
}

func acquireMigrationTemplate(t testing.TB, baseDSN string) string {
	t.Helper()

	fingerprint, err := migrationFingerprint()
	if err != nil {
		t.Fatalf("dbtest: fingerprint migrations: %v", err)
	}
	key := baseDSN + "\x00" + fingerprint
	sharedTemplates.Lock()
	state := sharedTemplates.byKey[key]
	if state == nil {
		state = &templateState{}
		sharedTemplates.byKey[key] = state
	}
	sharedTemplates.Unlock()

	state.once.Do(func() {
		state.name, state.err = prepareMigrationTemplate(baseDSN, fingerprint)
	})
	if state.err != nil {
		t.Fatalf("dbtest: prepare migration template: %v", state.err)
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
		return "", err
	}
	defer func() {
		cleanupCtx, cleanupCancel := migrationCleanupContext(ctx)
		defer cleanupCancel()
		err = errors.Join(err, unlock(cleanupCtx))
	}()

	return prepareMigrationTemplateBody(ctx, conn, baseDSN, name, fingerprint)
}

func lockMigrationTemplate(ctx context.Context, conn *pgxpool.Conn, name string) (func(context.Context) error, error) {
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", name); err != nil {
		return nil, fmt.Errorf("lock migration template: %w", err)
	}
	return func(ctx context.Context) error {
		_, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext($1))", name)
		return err
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
		return "", err
	}
	if ready {
		return name, nil
	}
	if err := resetMigrationTemplate(ctx, conn, baseDSN, name); err != nil {
		return "", err
	}
	templateCreated = true

	if err := applyMigrationTemplate(ctx, baseDSN, name); err != nil {
		return "", err
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
