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
	"testing"
)

func TestNewPool_AppliesProdMigrations(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	for _, table := range []string{"acl_settings", "acl_rooms", "major_events"} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM information_schema.tables
				WHERE table_schema = current_schema()
				  AND table_name = $1
			)`, table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}

		if !exists {
			t.Fatalf("table %s not present after migrations", table)
		}
	}
}

// TestNewPool_RestoresBaseSchema는 006-base-runtime-tables.sql이 적용되고 이후 chain의
// ALTER까지 반영된 결과(domain struct와 일치하는 컬럼)가 존재하는지 검증한다. 이 테스트가
// 통과하면 base-schema gap이 없고 manifest 전체가 빈 DB에서 끝까지 적용됐음을 보장한다.
func TestNewPool_RestoresBaseSchema(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	// base 테이블이 실제로 존재하는지 to_regclass로 확인한다.
	for _, table := range []string{"members", "alarms", "youtube_milestones", "youtube_notification_outbox"} {
		var oid *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, table).Scan(&oid); err != nil {
			t.Fatalf("to_regclass(%s): %v", table, err)
		}
		if oid == nil {
			t.Fatalf("base table %s not present after full-chain migrations", table)
		}
	}

	// base 컬럼(006이 생성) + 이후 migration이 ADD한 컬럼이 모두 존재해야 한다.
	// members: aliases(006 base) + photo(009) + org(016) + twitch_user_id(018).
	// alarms: room_id(006 base) + alarm_types(010).
	wantColumns := map[string][]string{
		"members": {"id", "slug", "channel_id", "english_name", "aliases", "photo", "org", "sync_source", "twitch_user_id"},
		"alarms":  {"id", "room_id", "user_id", "channel_id", "created_at", "alarm_types"},
	}

	for table, columns := range wantColumns {
		for _, column := range columns {
			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM information_schema.columns
					WHERE table_schema = current_schema()
					  AND table_name = $1
					  AND column_name = $2
				)`, table, column,
			).Scan(&exists)
			if err != nil {
				t.Fatalf("check column %s.%s: %v", table, column, err)
			}
			if !exists {
				t.Fatalf("column %s.%s missing after full-chain migrations", table, column)
			}
		}
	}

	// base 테이블에 실제 write가 가능한지(NOT NULL/제약 정합성) 한 행으로 확인한다.
	if _, err := pool.Exec(ctx,
		`INSERT INTO members (slug, english_name, status, is_graduated, org, sync_source)
		 VALUES ('smoke-test', 'Smoke Test', 'active', false, 'Hololive', 'manual')`,
	); err != nil {
		t.Fatalf("insert into members: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE slug = 'smoke-test'`).Scan(&count); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 smoke-test member, got %d", count)
	}
}

// TestNewPool_IsolatesPerCall은 호출마다 격리된 데이터베이스를 받는지 검증한다.
func TestNewPool_IsolatesPerCall(t *testing.T) {
	ctx := context.Background()

	poolA := NewPool(t)
	poolB := NewPool(t)

	var dbA, dbB string
	if err := poolA.QueryRow(ctx, "SELECT current_database()").Scan(&dbA); err != nil {
		t.Fatalf("current_database A: %v", err)
	}

	if err := poolB.QueryRow(ctx, "SELECT current_database()").Scan(&dbB); err != nil {
		t.Fatalf("current_database B: %v", err)
	}

	if dbA == dbB {
		t.Fatalf("expected isolated databases, both got %q", dbA)
	}

	// 한 DB에 쓴 데이터가 다른 DB에 보이지 않아야 한다.
	if _, err := poolA.Exec(ctx, "INSERT INTO acl_settings (key, value) VALUES ('enabled', 'true')"); err != nil {
		t.Fatalf("insert into database A: %v", err)
	}

	var countB int
	if err := poolB.QueryRow(ctx, "SELECT count(*) FROM acl_settings").Scan(&countB); err != nil {
		t.Fatalf("count database B: %v", err)
	}

	if countB != 0 {
		t.Fatalf("database B should be empty, got %d rows", countB)
	}
}
