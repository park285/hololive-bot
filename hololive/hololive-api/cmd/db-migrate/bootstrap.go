package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

type loginRole struct {
	user            string
	env             string
	connectionLimit int
	collector       bool
}

type loginCredential struct {
	loginRole

	password string
}

var loginRoles = []loginRole{
	{user: runtimepolicy.PostgresCollectorARoleUser, env: "HOLOLIVE_COLLECTOR_A_PASSWORD", connectionLimit: 7, collector: true},
	{user: runtimepolicy.PostgresCollectorBRoleUser, env: "HOLOLIVE_COLLECTOR_B_PASSWORD", connectionLimit: 7, collector: true},
	{user: runtimepolicy.PostgresCollectorCRoleUser, env: "HOLOLIVE_COLLECTOR_C_PASSWORD", connectionLimit: 7, collector: true},
	{user: runtimepolicy.PostgresCollectorDRoleUser, env: "HOLOLIVE_COLLECTOR_D_PASSWORD", connectionLimit: 7, collector: true},
	{user: runtimepolicy.PostgresAlarmWorkerRoleUser, env: "HOLOLIVE_ALARM_WORKER_PASSWORD", connectionLimit: 8},
	{user: runtimepolicy.PostgresRuntimeRoleUser, env: "HOLOLIVE_DB_PASSWORD", connectionLimit: 17},
}

func bootstrapLoginRoles(ctx context.Context) error {
	adminPassword := os.Getenv("POSTGRES_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		return nil
	}

	credentials, err := loginCredentialsFromEnv(adminPassword)
	if err != nil {
		return err
	}

	if err := validatePrivilegedTransport(); err != nil {
		return err
	}

	adminUser := envDefault("POSTGRES_ADMIN_USER", "postgres_admin")
	scraperGroup := runtimepolicy.PostgresScraperRoleUser
	database := envDefault("PGDATABASE", "hololive")

	if err := ensureLoginRoles(ctx, adminConnString("postgres", adminUser, adminPassword), scraperGroup, credentials, database); err != nil {
		return fmt.Errorf("ensure login roles: %w", err)
	}

	if err := grantScraperSchemaUsage(ctx, adminConnString(database, adminUser, adminPassword), scraperGroup); err != nil {
		return fmt.Errorf("grant scraper schema usage: %w", err)
	}

	return nil
}

func loginCredentialsFromEnv(adminPassword string) ([]loginCredential, error) {
	credentials := make([]loginCredential, 0, len(loginRoles))

	for _, role := range loginRoles {
		password := os.Getenv(role.env)
		if strings.TrimSpace(password) == "" {
			return nil, fmt.Errorf("%s is required for login role bootstrap", role.env)
		}

		if password == adminPassword {
			return nil, fmt.Errorf("POSTGRES_ADMIN_PASSWORD and %s must be distinct", role.env)
		}

		if _, err := pgxdb.SCRAMVerifier(password); err != nil {
			return nil, fmt.Errorf("%s is not a compatible PostgreSQL password: %w", role.env, err)
		}

		for _, previous := range credentials {
			if previous.password == password {
				return nil, fmt.Errorf("%s and %s must be distinct", previous.env, role.env)
			}
		}

		credentials = append(credentials, loginCredential{loginRole: role, password: password})
	}

	return credentials, nil
}

func ensureLoginRoles(
	ctx context.Context,
	connString,
	scraperGroup string,
	credentials []loginCredential,
	database string,
) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse admin config: %w", err)
	}

	protectCredentialSession(config)

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect admin: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "admin", err) }()

	if err := upsertScraperGroupRole(ctx, conn, scraperGroup); err != nil {
		return fmt.Errorf("upsert scraper group role: %w", err)
	}

	databaseIdent := pgx.Identifier{database}.Sanitize()

	for _, credential := range credentials {
		if err := ensureLoginRole(ctx, conn, scraperGroup, databaseIdent, credential); err != nil {
			return fmt.Errorf("ensure login role %s: %w", credential.user, err)
		}
	}

	grant := fmt.Sprintf(mustSQL("grant_connect.sql.tpl"), databaseIdent, pgx.Identifier{scraperGroup}.Sanitize())
	if _, err := conn.Exec(ctx, grant); err != nil {
		return fmt.Errorf("bootstrap: grant connect: %w", err)
	}

	return nil
}

func ensureLoginRole(ctx context.Context, conn roleConnection, scraperGroup, databaseIdent string, credential loginCredential) error {
	if err := upsertLoginRole(ctx, conn, credential.user, credential.password); err != nil {
		return err
	}

	roleIdent := pgx.Identifier{credential.user}.Sanitize()
	membership := fmt.Sprintf(mustSQL("grant_connect.sql.tpl"), databaseIdent, roleIdent)

	if credential.collector {
		membership = fmt.Sprintf(mustSQL("grant_scraper_membership.sql.tpl"), pgx.Identifier{scraperGroup}.Sanitize(), roleIdent)
	}

	if _, err := conn.Exec(ctx, membership); err != nil {
		return fmt.Errorf("bootstrap: grant database access: %w", err)
	}

	limit := fmt.Sprintf(mustSQL("alter_role_connection_limit.sql.tpl"), roleIdent, credential.connectionLimit)
	if _, err := conn.Exec(ctx, limit); err != nil {
		return fmt.Errorf("bootstrap: limit role connections: %w", err)
	}

	return nil
}

func upsertScraperGroupRole(ctx context.Context, conn roleConnection, scraperGroup string) error {
	var exists bool

	if err := conn.QueryRow(ctx, mustSQL("check_role.sql"), scraperGroup).Scan(&exists); err != nil {
		return fmt.Errorf("bootstrap: check scraper group role: %w", err)
	}

	roleIdent := pgx.Identifier{scraperGroup}.Sanitize()

	if !exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf(mustSQL("create_scraper_group_role.sql.tpl"), roleIdent)); err != nil {
			return fmt.Errorf("bootstrap: create scraper group role: %w", err)
		}
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf(mustSQL("alter_scraper_group_role.sql.tpl"), roleIdent)); err != nil {
		return fmt.Errorf("bootstrap: alter scraper group role: %w", err)
	}

	if _, err := conn.Exec(ctx, mustSQL("terminate_role_sessions.sql"), scraperGroup); err != nil {
		return fmt.Errorf("bootstrap: terminate scraper group sessions: %w", err)
	}

	return nil
}

func protectCredentialSession(config *pgx.ConnConfig) {
	config.RuntimeParams["application_name"] = "hololive-login-role-bootstrap"
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

type roleConnection interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func upsertLoginRole(ctx context.Context, conn roleConnection, user, password string) error {
	var exists bool

	if err := conn.QueryRow(ctx, mustSQL("check_role.sql"), user).Scan(&exists); err != nil {
		return fmt.Errorf("bootstrap: check login role: %w", err)
	}

	verifier, err := pgxdb.SCRAMVerifier(password)
	if err != nil {
		return fmt.Errorf("bootstrap: prepare login password: %w", err)
	}

	roleIdent := pgx.Identifier{user}.Sanitize()
	passwordLiteral := quoteSQLLiteral(verifier)

	if !exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf(mustSQL("create_login_role.sql.tpl"), roleIdent, passwordLiteral)); err != nil {
			return fmt.Errorf("bootstrap: create login role: %w", err)
		}
	}

	alter := fmt.Sprintf(
		mustSQL("alter_login_role.sql.tpl"),
		roleIdent, passwordLiteral,
	)
	if _, err := conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("bootstrap: alter login role: %w", err)
	}

	return nil
}

func grantScraperSchemaUsage(ctx context.Context, connString, scraperGroup string) (err error) {
	config, err := pgxdb.ParseConfig(connString)
	if err != nil {
		return fmt.Errorf("bootstrap: parse database config: %w", err)
	}

	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap: connect database: %w", err)
	}

	defer func() { err = closeConn(ctx, conn, "database", err) }()

	stmt := fmt.Sprintf(mustSQL("grant_schema_usage.sql.tpl"), pgx.Identifier{scraperGroup}.Sanitize())
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
