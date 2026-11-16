package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

type runtimeRoleSpec struct {
	label           string
	user            string
	passwordEnv     string
	applicationName string
	connectionLimit int
}

// POSTGRES_ADMIN_PASSWORD가 없으면 순수 migrate 경로로 보고 bootstrap을 생략한다. 역할별 비밀번호는 서로 대신할 수 없다.
func bootstrapRuntimeRoles(ctx context.Context) error {
	adminPassword := os.Getenv("POSTGRES_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		return nil
	}

	// API 비밀번호는 마지막에 회전해 앞선 worker 준비 실패가 실행 중 API의 재접속을 끊지 않게 한다.
	roles := []runtimeRoleSpec{
		{
			label: "scraper", user: runtimepolicy.PostgresScraperRoleUser,
			passwordEnv: "HOLOLIVE_SCRAPER_PASSWORD", applicationName: "hololive-scraper-role-bootstrap", connectionLimit: 28,
		},
		{
			label: "alarm worker", user: runtimepolicy.PostgresAlarmWorkerRoleUser,
			passwordEnv: "HOLOLIVE_ALARM_WORKER_PASSWORD", applicationName: "hololive-alarm-worker-role-bootstrap", connectionLimit: 8,
		},
		{
			label: "API runtime", user: runtimepolicy.PostgresRuntimeRoleUser,
			passwordEnv: "HOLOLIVE_DB_PASSWORD", applicationName: "hololive-api-role-bootstrap", connectionLimit: 25,
		},
	}

	passwords := make(map[string]string, len(roles))
	for i, role := range roles {
		password := os.Getenv(role.passwordEnv)
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("%s is required for runtime role bootstrap", role.passwordEnv)
		}

		for j := range i {
			if password == passwords[roles[j].user] {
				return fmt.Errorf("%s and %s must use distinct runtime role passwords", roles[j].passwordEnv, role.passwordEnv)
			}
		}
		if _, err := pgxdb.SCRAMVerifier(password); err != nil {
			return fmt.Errorf("%s is not a compatible PostgreSQL password: %w", role.passwordEnv, err)
		}

		passwords[role.user] = password
	}

	if err := validatePrivilegedTransport(); err != nil {
		return err
	}

	adminUser := envDefault("POSTGRES_ADMIN_USER", "postgres_admin")
	database := envDefault("PGDATABASE", "hololive")

	for _, role := range roles {
		if err := ensureRuntimeRole(
			ctx,
			adminConnString("postgres", adminUser, adminPassword),
			role,
			passwords[role.user],
			database,
		); err != nil {
			return fmt.Errorf("ensure %s role: %w", role.label, err)
		}

		if err := grantRuntimeSchemaUsage(ctx, adminConnString(database, adminUser, adminPassword), role.user); err != nil {
			return fmt.Errorf("grant %s schema usage: %w", role.label, err)
		}
	}

	return nil
}

func ensureRuntimeRole(ctx context.Context, connString string, role runtimeRoleSpec, password, database string) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse admin config: %w", err)
	}

	// 검증자도 자격 증명이므로 SQL이 오류·샘플·실행 계획·활동 보고에 노출되기 전에 세션을 보호한다.
	protectCredentialSession(config, role.applicationName)

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect admin: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "admin", err) }()

	if err := upsertRuntimeRole(ctx, conn, role, password); err != nil {
		return fmt.Errorf("upsert runtime role: %w", err)
	}

	grant := fmt.Sprintf(mustSQL("grant_connect.sql.tpl"), pgx.Identifier{database}.Sanitize(), pgx.Identifier{role.user}.Sanitize())
	if _, err := conn.Exec(ctx, grant); err != nil {
		return fmt.Errorf("bootstrap: grant connect: %w", err)
	}

	return nil
}

func protectCredentialSession(config *pgx.ConnConfig, applicationName string) {
	config.RuntimeParams["application_name"] = applicationName
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

type runtimeRoleConnection interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertRuntimeRole(ctx context.Context, conn runtimeRoleConnection, role runtimeRoleSpec, password string) error {
	var exists bool

	if err := conn.QueryRow(ctx, mustSQL("check_runtime_role.sql"), role.user).Scan(&exists); err != nil {
		return fmt.Errorf("bootstrap: check runtime role: %w", err)
	}

	verifier, err := pgxdb.SCRAMVerifier(password)
	if err != nil {
		return fmt.Errorf("bootstrap: prepare runtime password: %w", err)
	}

	roleIdent := pgx.Identifier{role.user}.Sanitize()
	passwordLiteral := quoteSQLLiteral(verifier)

	if !exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf(mustSQL("create_runtime_role.sql.tpl"), roleIdent, passwordLiteral)); err != nil {
			return fmt.Errorf("bootstrap: create runtime role: %w", err)
		}
	}

	alter := fmt.Sprintf(
		mustSQL("alter_runtime_role.sql.tpl"),
		roleIdent, passwordLiteral, role.connectionLimit,
	)
	if _, err := conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("bootstrap: alter runtime role: %w", err)
	}

	return nil
}

func grantRuntimeSchemaUsage(ctx context.Context, connString, roleUser string) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse database config: %w", err)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect database: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "database", err) }()

	stmt := fmt.Sprintf(mustSQL("grant_schema_usage.sql.tpl"), pgx.Identifier{roleUser}.Sanitize())
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
