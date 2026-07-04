package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// bootstrapScraperRole은 bootstrap-and-apply.sh의 role bootstrap 블록을 Go로 옮긴 것이다.
// 배포 이미지(hololive-api:prod)가 distroless라 shell/psql이 없어 shell wrapper를
// 재사용할 수 없으므로 pgx로 직접 수행한다.
//
// POSTGRES_ADMIN_PASSWORD가 명시되지 않으면 순수 migrate(롤백) 경로로 보고 bootstrap을
// 생략한다 — PGPASSWORD로 fallback하면 migrator 자격증명으로 admin 접속을 시도해 순수
// migrate가 깨지기 때문이다. compose는 POSTGRES_ADMIN_PASSWORD를 항상 설정한다.
func bootstrapScraperRole(ctx context.Context) error {
	adminPassword := os.Getenv("POSTGRES_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		return nil
	}

	adminUser := envDefault("POSTGRES_ADMIN_USER", "postgres_admin")
	scraperUser := envDefault("HOLOLIVE_SCRAPER_USER", "hololive_scraper")
	scraperPassword := envDefault("HOLOLIVE_SCRAPER_PASSWORD", adminPassword)
	database := envDefault("PGDATABASE", "hololive")

	if err := ensureScraperRole(ctx, adminConnString("postgres", adminUser, adminPassword), scraperUser, scraperPassword, database); err != nil {
		return err
	}
	return grantScraperSchemaUsage(ctx, adminConnString(database, adminUser, adminPassword), scraperUser)
}

func ensureScraperRole(ctx context.Context, connString, scraperUser, scraperPassword, database string) (err error) {
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return fmt.Errorf("bootstrap: connect admin: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("bootstrap: close admin: %w", closeErr)
		}
	}()

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", scraperUser).Scan(&exists); err != nil {
		return fmt.Errorf("bootstrap: check scraper role: %w", err)
	}

	roleIdent := pgx.Identifier{scraperUser}.Sanitize()
	passwordLiteral := quoteSQLLiteral(scraperPassword)
	if !exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s", roleIdent, passwordLiteral)); err != nil {
			return fmt.Errorf("bootstrap: create scraper role: %w", err)
		}
	}

	alter := fmt.Sprintf(
		"ALTER ROLE %s WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %s",
		roleIdent, passwordLiteral,
	)
	if _, err := conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("bootstrap: alter scraper role: %w", err)
	}

	grant := fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", pgx.Identifier{database}.Sanitize(), roleIdent)
	if _, err := conn.Exec(ctx, grant); err != nil {
		return fmt.Errorf("bootstrap: grant connect: %w", err)
	}
	return nil
}

func grantScraperSchemaUsage(ctx context.Context, connString, scraperUser string) (err error) {
	conn, err := pgx.Connect(ctx, connString)
	if err != nil {
		return fmt.Errorf("bootstrap: connect database: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil && err == nil {
			err = fmt.Errorf("bootstrap: close database: %w", closeErr)
		}
	}()

	stmt := fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", pgx.Identifier{scraperUser}.Sanitize())
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("bootstrap: grant schema usage: %w", err)
	}
	return nil
}

func adminConnString(database, user, password string) string {
	parts := make([]string, 0, 7)
	parts = append(parts,
		connPart("host", envDefault("PGHOST", "postgres")),
		connPart("port", envDefault("PGPORT", "5432")),
		connPart("dbname", database),
		connPart("user", user),
		connPart("password", password),
	)
	parts = append(parts, sslConnParts()...)
	return strings.Join(parts, " ")
}

func quoteSQLLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
