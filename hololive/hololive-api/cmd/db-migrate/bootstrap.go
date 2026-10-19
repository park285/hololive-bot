package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// bootstrapScraperRole은 migration과 collector가 공유하는 고정 scraper 역할을 준비한다.
//
// POSTGRES_ADMIN_PASSWORD가 명시되지 않으면 순수 migrate(롤백) 경로로 보고 bootstrap을
// 생략한다 — PGPASSWORD로 fallback하면 migrator 자격증명으로 admin 접속을 시도해 순수
// migrate가 깨지기 때문이다. 참고로 compose는 POSTGRES_ADMIN_PASSWORD를 항상 설정한다.
//
// 역할 bootstrap을 할 때 HOLOLIVE_SCRAPER_PASSWORD는 필수다. 비어 있을 때 admin 비밀번호로 scraper role을 ALTER하면
// 역할 분리가 깨지므로, 대신할 값을 찾지 않고 실패한다.
func bootstrapScraperRole(ctx context.Context) error {
	adminPassword := os.Getenv("POSTGRES_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		return nil
	}

	scraperPassword := os.Getenv("HOLOLIVE_SCRAPER_PASSWORD")
	if strings.TrimSpace(scraperPassword) == "" {
		return errors.New("HOLOLIVE_SCRAPER_PASSWORD is required for scraper role bootstrap")
	}

	if err := validatePrivilegedTransport(); err != nil {
		return err
	}

	adminUser := envDefault("POSTGRES_ADMIN_USER", "postgres_admin")
	scraperUser := runtimepolicy.PostgresScraperRoleUser
	database := envDefault("PGDATABASE", "hololive")

	if err := ensureScraperRole(ctx, adminConnString("postgres", adminUser, adminPassword), scraperUser, scraperPassword, database); err != nil {
		return fmt.Errorf("ensure scraper role: %w", err)
	}

	if err := grantScraperSchemaUsage(ctx, adminConnString(database, adminUser, adminPassword), scraperUser); err != nil {
		return fmt.Errorf("grant scraper schema usage: %w", err)
	}

	return nil
}

func ensureScraperRole(ctx context.Context, connString, scraperUser, scraperPassword, database string) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse admin config: %w", err)
	}

	// 검증자도 자격 증명이므로 SQL이 오류·샘플·실행 계획·활동 보고에 노출되기 전에 세션을 보호한다.
	protectCredentialSession(config)

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect admin: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "admin", err) }()

	if err := upsertScraperRole(ctx, conn, scraperUser, scraperPassword); err != nil {
		return fmt.Errorf("upsert scraper role: %w", err)
	}

	grant := fmt.Sprintf(mustSQL("grant_connect.sql.tpl"), pgx.Identifier{database}.Sanitize(), pgx.Identifier{scraperUser}.Sanitize())
	if _, err := conn.Exec(ctx, grant); err != nil {
		return fmt.Errorf("bootstrap: grant connect: %w", err)
	}

	return nil
}

func protectCredentialSession(config *pgx.ConnConfig) {
	config.RuntimeParams["application_name"] = "hololive-scraper-role-bootstrap"
	config.RuntimeParams["log_statement"] = "none"
	config.RuntimeParams["log_min_duration_statement"] = "-1"
	config.RuntimeParams["log_min_duration_sample"] = "-1"
	config.RuntimeParams["log_transaction_sample_rate"] = "0"
	config.RuntimeParams["log_min_error_statement"] = "panic"
	config.RuntimeParams["log_min_messages"] = "panic"
	config.RuntimeParams["log_parameter_max_length"] = "0"
	config.RuntimeParams["log_parameter_max_length_on_error"] = "0"
	config.RuntimeParams["debug_print_parse"] = "off"
	config.RuntimeParams["debug_print_rewritten"] = "off"
	config.RuntimeParams["debug_print_plan"] = "off"
	config.RuntimeParams["auto_explain.log_min_duration"] = "-1"
	config.RuntimeParams["pg_stat_statements.track"] = "none"
	config.RuntimeParams["track_activities"] = "off"
}

type scraperRoleConnection interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertScraperRole(ctx context.Context, conn scraperRoleConnection, scraperUser, scraperPassword string) error {
	var exists bool

	if err := conn.QueryRow(ctx, mustSQL("check_scraper_role.sql"), scraperUser).Scan(&exists); err != nil {
		return fmt.Errorf("bootstrap: check scraper role: %w", err)
	}

	verifier, err := pgxdb.SCRAMVerifier(scraperPassword)
	if err != nil {
		return fmt.Errorf("bootstrap: prepare scraper password: %w", err)
	}

	roleIdent := pgx.Identifier{scraperUser}.Sanitize()
	passwordLiteral := quoteSQLLiteral(verifier)

	if !exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf(mustSQL("create_scraper_role.sql.tpl"), roleIdent, passwordLiteral)); err != nil {
			return fmt.Errorf("bootstrap: create scraper role: %w", err)
		}
	}

	alter := fmt.Sprintf(
		mustSQL("alter_scraper_role.sql.tpl"),
		roleIdent, passwordLiteral,
	)
	if _, err := conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("bootstrap: alter scraper role: %w", err)
	}

	return nil
}

func grantScraperSchemaUsage(ctx context.Context, connString, scraperUser string) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse database config: %w", err)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect database: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "database", err) }()

	stmt := fmt.Sprintf(mustSQL("grant_schema_usage.sql.tpl"), pgx.Identifier{scraperUser}.Sanitize())
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("bootstrap: grant schema usage: %w", err)
	}

	return nil
}

func closeConn(ctx context.Context, conn *pgx.Conn, label string, err error) error {
	if closeErr := conn.Close(ctx); closeErr != nil && err == nil {
		return fmt.Errorf("bootstrap: close %s: %w", label, closeErr)
	}

	return err
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
