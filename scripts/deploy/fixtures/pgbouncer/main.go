package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
)

var appRoles = []string{"hololive_runtime", "hololive_alarm_worker", "hololive_collector_a", "hololive_collector_b", "hololive_collector_c", "hololive_collector_d"}

const authRole = "hololive_pgbouncer_auth"

type fixture struct {
	Passwords map[string]string
	Rotated   string
}

func writePrivate(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

func password() string {
	var data [32]byte
	rand.Read(data[:])
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func certificate(dir, name, cn string, ca *x509.Certificate, caKey *ecdsa.PrivateKey, client bool) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: cn}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.DNSNames = []string{cn}
		if cn == "holo-pgbouncer" {
			template.IPAddresses = []net.IP{net.ParseIP("100.100.1.8"), net.ParseIP("172.23.255.254")}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, name+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

func prepare(dir string) error {
	for _, name := range []string{"pg-tls", "pooler-tls", "client-tls"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			return err
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated-pgbouncer-test-ca"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return err
	}
	for _, item := range []struct {
		Dir, Name, CN string
		Client        bool
	}{
		{"pg-tls", "server", "holo-postgres", false},
		{"pooler-tls", "server", "holo-pgbouncer", false},
		{"pooler-tls", "backend-client", authRole, true},
		{"client-tls", "good", authRole, true},
		{"client-tls", "wrong", "unapproved-pooler", true},
	} {
		if err := certificate(filepath.Join(dir, item.Dir), item.Name, item.CN, ca, key, item.Client); err != nil {
			return err
		}
	}
	f := fixture{Passwords: make(map[string]string, 10), Rotated: password()}
	roles := append(slices.Clone(appRoles), "postgres_admin", "hololive_migrator", "postgres_exporter", "iris_db_nightly")
	for _, role := range roles {
		f.Passwords[role] = password()
	}
	encoded, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(dir, "fixture.json"), encoded); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(dir, "admin-password"), []byte(f.Passwords["postgres_admin"])); err != nil {
		return err
	}
	var sql strings.Builder
	sql.WriteString("REVOKE ALL ON DATABASE hololive FROM PUBLIC; REVOKE ALL ON SCHEMA public FROM PUBLIC;\nCREATE ROLE hololive_scraper NOLOGIN;\n")
	for _, role := range roles {
		if role == "postgres_admin" {
			continue
		}
		verifier, err := pgxdb.SCRAMVerifier(f.Passwords[role])
		if err != nil {
			return err
		}
		fmt.Fprintf(&sql, "CREATE ROLE %s LOGIN PASSWORD '%s'; GRANT CONNECT ON DATABASE hololive TO %s;\n", pgx.Identifier{role}.Sanitize(), verifier, pgx.Identifier{role}.Sanitize())
	}
	sql.WriteString("ALTER DATABASE hololive OWNER TO hololive_migrator; GRANT USAGE,CREATE ON SCHEMA public TO hololive_migrator;\nGRANT hololive_scraper TO hololive_collector_a,hololive_collector_b,hololive_collector_c,hololive_collector_d;\n")
	if err := writePrivate(filepath.Join(dir, "setup.sql"), []byte(sql.String())); err != nil {
		return err
	}
	sql.Reset()
	sql.WriteString("CREATE TABLE public.pooler_probe(id integer PRIMARY KEY, value integer NOT NULL); INSERT INTO public.pooler_probe VALUES (1,0);\n")
	for _, role := range appRoles {
		fmt.Fprintf(&sql, "GRANT USAGE ON SCHEMA public TO %s; GRANT SELECT,UPDATE ON public.pooler_probe TO %s;\n", pgx.Identifier{role}.Sanitize(), pgx.Identifier{role}.Sanitize())
	}
	if err := writePrivate(filepath.Join(dir, "records.sql"), []byte(sql.String())); err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, "bootstrap.env"), []byte(strings.Join(bootstrapEnv(f, f.Passwords["hololive_runtime"]), "\n")+"\n"))
}

func bootstrapEnv(f fixture, runtimePassword string) []string {
	result := []string{"PGHOST=/var/run/postgresql", "PGPORT=5432", "PGDATABASE=hololive", "PGUSER=hololive_migrator", "PGSSLMODE=disable", "PGPASSWORD=" + f.Passwords["hololive_migrator"], "POSTGRES_ADMIN_USER=postgres_admin", "POSTGRES_ADMIN_PASSWORD=" + f.Passwords["postgres_admin"], "HOLOLIVE_DB_PASSWORD=" + runtimePassword, "HOLOLIVE_ALARM_WORKER_PASSWORD=" + f.Passwords["hololive_alarm_worker"]}
	for _, slot := range []string{"a", "b", "c", "d"} {
		result = append(result, "HOLOLIVE_COLLECTOR_"+strings.ToUpper(slot)+"_PASSWORD="+f.Passwords["hololive_collector_"+slot])
	}
	return result
}

func bootstrap(ctx context.Context, f fixture, runtimePassword string) error {
	command := exec.CommandContext(ctx, "/fixture/db-migrate")
	command.Env = bootstrapEnv(f, runtimePassword)
	if err := command.Run(); err != nil {
		return fmt.Errorf("fixture bootstrap command: %w", err)
	}
	return nil
}

func code(err error) string {
	if e, ok := errors.AsType[*pgconn.PgError](err); ok {
		return e.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	return "client_error"
}

func connection(ctx context.Context, f fixture, host, role, secret, database, sslmode, clientCert, statementTimeout string) (*pgx.Conn, error) {
	port := 6432
	if host == "holo-postgres" || strings.HasPrefix(host, "/") {
		port = 5432
	}
	cfg, err := pgx.ParseConfig(fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=%s sslrootcert=/fixture/ca.crt", host, port, role, database, sslmode))
	if err != nil {
		return nil, err
	}
	cfg.Password = secret
	cfg.DefaultQueryExecMode = pgx.QueryExecModeCacheStatement
	cfg.ConnectTimeout = 3 * time.Second
	cfg.RuntimeParams["application_name"] = "pgbouncer-product-probe"
	if statementTimeout != "" {
		cfg.RuntimeParams["statement_timeout"] = statementTimeout
	}
	if clientCert != "" {
		cfg, err = pgx.ParseConfig(fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=verify-full sslrootcert=/fixture/ca.crt sslcert=/fixture/client-tls/%s.crt sslkey=/fixture/client-tls/%s.key", host, port, role, database, clientCert, clientCert))
		if err != nil {
			return nil, err
		}
		cfg.ConnectTimeout = 3 * time.Second
	}
	if strings.HasPrefix(host, "/") {
		cfg.RuntimeParams["log_statement"] = "none"
		cfg.RuntimeParams["log_min_error_statement"] = "panic"
		cfg.RuntimeParams["log_min_duration_statement"] = "-1"
		cfg.RuntimeParams["track_activities"] = "off"
	}
	return pgx.ConnectConfig(ctx, cfg)
}

func credentialRejected(pgErr *pgconn.PgError) bool {
	switch pgErr.Code {
	case "28000", "28P01":
		return true
	case "08P01":
		// PgBouncer는 자원 오류에도 08P01을 쓰므로 인증 메시지만 인정한다.
		switch pgErr.Message {
		case "SASL authentication failed", "password authentication failed", "no such user":
			return true
		}
	}
	return false
}

func denyConnection(ctx context.Context, f fixture, host, role, database, sslmode, cert string) error {
	conn, err := connection(ctx, f, host, role, f.Passwords[role], database, sslmode, cert, "")
	if err == nil {
		conn.Close(ctx)
		return fmt.Errorf("unexpected login accepted: %s/%s", host, role)
	}
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok {
		return fmt.Errorf("login rejection was not a PostgreSQL response for %s: %w", role, err)
	}
	if credentialRejected(pgErr) {
		return nil
	}
	if pgErr.Code == "08P01" {
		switch pgErr.Message {
		case "SSL required", "no authentication method is found", "login rejected":
			return nil
		}
		if database != "hololive" && pgErr.Message == "no such database: "+database {
			return nil
		}
	}
	return fmt.Errorf("login rejection was not the expected access-policy failure for %s: %w", role, err)
}

func authClient(ctx context.Context, f fixture, wrongSource bool) error {
	if wrongSource {
		return denyConnection(ctx, f, "holo-postgres", authRole, "hololive", "verify-full", "good")
	}
	for _, cert := range []string{"", "wrong"} {
		if err := denyConnection(ctx, f, "holo-postgres", authRole, "hololive", "verify-full", cert); err != nil {
			return err
		}
	}
	conn, err := connection(ctx, f, "holo-postgres", authRole, "", "hololive", "verify-full", "good", "")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	for _, role := range appRoles {
		var count int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM hololive_pooler_auth.lookup($1)", role).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("lookup did not identify allowed role %s", role)
		}
	}
	for _, role := range []string{"postgres_admin", authRole, "postgres_exporter", "hololive_migrator", "unknown"} {
		var count int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM hololive_pooler_auth.lookup($1)", role).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("lookup exposed disallowed role %s", role)
		}
	}
	for _, query := range []string{"SELECT rolpassword FROM pg_catalog.pg_authid", "SELECT value FROM public.pooler_probe", "CREATE SCHEMA forbidden_pooler_probe"} {
		if _, err := conn.Exec(ctx, query); code(err) != "42501" {
			return fmt.Errorf("auth role privilege rejection failed: %s", code(err))
		}
	}
	return nil
}

func identity(ctx context.Context, f fixture, source string) error {
	allowed := []string{"hololive_runtime", "hololive_alarm_worker", "hololive_collector_c"}
	host := "holo-pgbouncer"
	if source != "central" {
		allowed = []string{"hololive_collector_" + source}
		host = "100.100.1.8"
	}
	for _, role := range appRoles {
		if !slices.Contains(allowed, role) {
			if err := denyConnection(ctx, f, host, role, "hololive", "verify-full", ""); err != nil {
				return err
			}
			continue
		}
		conn, err := connection(ctx, f, host, role, f.Passwords[role], "hololive", "verify-full", "", "30000")
		if err != nil {
			return err
		}
		var sessionUser, timeout string
		var tls bool
		err = conn.QueryRow(ctx, "SELECT session_user,current_setting('statement_timeout'),ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&sessionUser, &timeout, &tls)
		if err != nil {
			conn.Close(ctx)
			return err
		}
		if sessionUser != role || timeout != "30s" || !tls {
			conn.Close(ctx)
			return fmt.Errorf("identity/TLS/timeout mismatch: %s", role)
		}
		_, err = conn.Exec(ctx, "SELECT count(*) FROM hololive_pooler_auth.lookup('hololive_runtime')")
		conn.Close(ctx)
		if code(err) != "42501" {
			return fmt.Errorf("app can execute auth lookup: %s", role)
		}
	}
	for _, role := range []string{"postgres_admin", "hololive_migrator", "postgres_exporter", "iris_db_nightly", authRole} {
		if err := denyConnection(ctx, f, host, role, "hololive", "verify-full", ""); err != nil {
			return err
		}
	}
	if err := denyConnection(ctx, f, host, allowed[0], "hololive", "disable", ""); err != nil {
		return err
	}
	if err := denyConnection(ctx, f, host, allowed[0], "postgres", "verify-full", ""); err != nil {
		return err
	}
	if source == "central" {
		for _, role := range appRoles {
			if err := denyConnection(ctx, f, "holo-postgres", role, "hololive", "verify-full", ""); err != nil {
				return err
			}
		}
		conn, err := connection(ctx, f, "holo-postgres", "postgres_exporter", f.Passwords["postgres_exporter"], "hololive", "verify-full", "", "")
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		var tls bool
		if err := conn.QueryRow(ctx, "SELECT ssl FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&tls); err != nil {
			return err
		}
		if !tls {
			return errors.New("direct exporter is not TLS")
		}
	}
	return nil
}

func pool(ctx context.Context, f fixture, role string, maxConns, reserved int, timeout string) (*pgxpool.Pool, error) {
	return pgxdb.OpenPool(ctx, pgxdb.Config{Host: "holo-pgbouncer", Port: 6432, User: role, Password: f.Passwords[role], Name: "hololive", SSLMode: "verify-full", SSLRootCert: "/fixture/ca.crt", QueryExecMode: "cache_statement"}, pgxdb.Options{Pool: pgxdb.PoolConfig{MaxConns: maxConns, MinConns: 0, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: 10 * time.Minute}, Ping: pgxdb.PingConfig{PingTimeout: 3 * time.Second}, RuntimeParams: map[string]string{"statement_timeout": timeout, "application_name": "pgbouncer-product-probe"}, AcquirePolicy: &pgxdb.AcquirePolicy{Timeout: 5 * time.Second, ReservedConns: int32(reserved)}, CancelRequest: &pgxdb.CancelRequestConfig{CancelRequestDelay: 0, DeadlineDelay: time.Second}})
}

func replaceBackends(ctx context.Context, admin *pgx.Conn) (result error) {
	if err := syscall.Kill(1, syscall.SIGUSR1); err != nil {
		return fmt.Errorf("pause fixture pooler: %w", err)
	}
	defer func() {
		if err := syscall.Kill(1, syscall.SIGUSR2); err != nil {
			result = errors.Join(result, fmt.Errorf("resume fixture pooler: %w", err))
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var active int
		if err := admin.QueryRow(waitCtx, "SELECT count(*) FROM pg_stat_activity WHERE usename='hololive_runtime'").Scan(&active); err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func abruptBackendLoss(ctx context.Context, f fixture, admin *pgx.Conn) error {
	p, err := pool(ctx, f, "hololive_runtime", 2, 0, "30000")
	if err != nil {
		return err
	}
	defer p.Close()
	const query = "SELECT value FROM public.pooler_probe WHERE id=$1"
	var before, after, pid, replacement int
	if err := p.QueryRow(ctx, query, 1).Scan(&before); err != nil {
		return err
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE public.pooler_probe SET value=value+1 WHERE id=1"); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		return err
	}
	var terminated bool
	if err := admin.QueryRow(ctx, "SELECT pg_terminate_backend($1,5000)", pid).Scan(&terminated); err != nil {
		return err
	}
	if !terminated {
		return errors.New("fault-injection backend did not terminate")
	}
	if err := tx.Commit(ctx); err == nil {
		return errors.New("terminated transaction was reported committed")
	}
	if err := p.QueryRow(ctx, query, 1).Scan(&after); err != nil {
		return err
	}
	if err := p.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&replacement); err != nil {
		return err
	}
	if after != before || replacement == pid {
		return errors.New("terminated transaction leaked a write or retained its backend")
	}
	fmt.Println("PASS abrupt backend loss surfaces failure, rolls back writes and permits a fresh prepared query")
	return nil
}

func prepared(ctx context.Context, f fixture, admin *pgx.Conn) error {
	first, err := connection(ctx, f, "holo-pgbouncer", "hololive_runtime", f.Passwords["hololive_runtime"], "hololive", "verify-full", "", "30000")
	if err != nil {
		return err
	}
	defer first.Close(ctx)
	second, err := connection(ctx, f, "holo-pgbouncer", "hololive_runtime", f.Passwords["hololive_runtime"], "hololive", "verify-full", "", "30000")
	if err != nil {
		return err
	}
	defer second.Close(ctx)
	const sql = "SELECT pg_backend_pid(), $1::integer"
	var before, after, holder, value int
	if err := first.QueryRow(ctx, sql, 11).Scan(&before, &value); err != nil {
		return err
	}
	tx, err := second.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holder); err != nil {
		return err
	}
	if err := first.QueryRow(ctx, sql, 22).Scan(&after, &value); err != nil {
		return err
	}
	if err := tx.Rollback(ctx); err != nil {
		return err
	}
	if before == after || holder != before || value != 22 {
		return errors.New("cached query did not survive a proven backend switch")
	}
	fmt.Println("PASS prepared cached query after backend switch")
	first.Close(ctx)
	second.Close(ctx)

	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			conn, err := connection(ctx, f, "holo-pgbouncer", "hololive_runtime", f.Passwords["hololive_runtime"], "hololive", "verify-full", "", "30000")
			if err != nil {
				failures <- err
				return
			}
			defer conn.Close(ctx)
			if _, err := conn.Prepare(ctx, "shared_client_name", fmt.Sprintf("SELECT $1::integer + %d", i)); err != nil {
				failures <- err
				return
			}
			for n := range 40 {
				var got int
				if err := conn.QueryRow(ctx, "shared_client_name", n).Scan(&got); err != nil {
					failures <- err
					return
				}
				if got != n+i {
					failures <- errors.New("prepared name crossed client SQL identities")
					return
				}
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		return err
	}
	fmt.Println("PASS concurrent equal prepared names with different SQL")
	conn, err := connection(ctx, f, "holo-pgbouncer", "hololive_runtime", f.Passwords["hololive_runtime"], "hololive", "verify-full", "", "30000")
	if err != nil {
		return err
	}
	if err := conn.QueryRow(ctx, sql, 33).Scan(&before, &value); err != nil {
		return err
	}
	conn.Close(ctx)
	if err := replaceBackends(ctx, admin); err != nil {
		return err
	}
	conn, err = connection(ctx, f, "holo-pgbouncer", "hololive_runtime", f.Passwords["hololive_runtime"], "hololive", "verify-full", "", "30000")
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := conn.QueryRow(ctx, sql, 44).Scan(&after, &value); err != nil {
		return err
	}
	if after == before || value != 44 {
		return errors.New("prepared query did not reprepare after reconnect")
	}
	fmt.Println("PASS client reconnect and server replacement reprepare")
	return nil
}

func timing(ctx context.Context, f fixture) error {
	low, err := pool(ctx, f, "hololive_runtime", 2, 0, "100")
	if err != nil {
		return err
	}
	defer low.Close()
	high, err := pool(ctx, f, "hololive_runtime", 2, 0, "30000")
	if err != nil {
		return err
	}
	defer high.Close()
	for range 4 {
		for _, item := range []struct {
			Pool *pgxpool.Pool
			Want string
		}{{low, "100ms"}, {high, "30s"}} {
			var got string
			if err := item.Pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&got); err != nil {
				return err
			}
			if got != item.Want {
				return fmt.Errorf("statement_timeout mismatch: got=%q want=%q startup=%q", got, item.Want, item.Pool.Config().ConnConfig.RuntimeParams["statement_timeout"])
			}
		}
	}
	if _, err := low.Exec(ctx, "SELECT pg_sleep(0.3)"); code(err) != "57014" {
		return fmt.Errorf("server timeout did not fire: %s", code(err))
	}
	if _, err := high.Exec(ctx, "SELECT pg_sleep(0.15)"); err != nil {
		return err
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, queryErr := high.Exec(cancelCtx, "SELECT pg_sleep(2)")
	cancel()
	if queryErr == nil {
		return errors.New("client cancellation did not stop query")
	}
	if err := high.Ping(ctx); err != nil {
		return err
	}
	fmt.Println("PASS per-client statement timeout, server cancellation and recovery")
	return nil
}

func control(ctx context.Context, f fixture) error {
	p, err := pool(ctx, f, "hololive_collector_c", 7, 1, "30000")
	if err != nil {
		return err
	}
	defer p.Close()
	held := make([]pgx.Tx, 0, 6)
	defer func() {
		for _, tx := range held {
			_ = tx.Rollback(ctx)
		}
	}()
	for range 6 {
		tx, err := p.Begin(ctx)
		if err != nil {
			return err
		}
		held = append(held, tx)
		if _, err := tx.Exec(ctx, "SELECT 1"); err != nil {
			return err
		}
	}
	controlCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), 2*time.Second)
	defer cancel()
	var got int
	if err := p.QueryRow(controlCtx, "SELECT 1").Scan(&got); err != nil {
		return err
	}
	if got != 1 {
		return errors.New("control query result mismatch")
	}
	fmt.Println("PASS reserved control progress with six pinned ordinary transactions")
	return nil
}

func authChanges(ctx context.Context, f fixture, admin *pgx.Conn) error {
	role := "hololive_runtime"
	probe := func(secret string, want bool) error {
		conn, err := connection(ctx, f, "holo-pgbouncer", role, secret, "hololive", "verify-full", "", "30000")
		if !want {
			if err == nil {
				conn.Close(ctx)
				return errors.New("revoked credential or role was accepted")
			}
			pgErr, ok := errors.AsType[*pgconn.PgError](err)
			if !ok || !credentialRejected(pgErr) {
				return fmt.Errorf("credential rejection was not an authentication failure: %w", err)
			}
			return nil
		}
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		var value int
		if err := conn.QueryRow(ctx, "SELECT value FROM public.pooler_probe WHERE id=$1", 1).Scan(&value); err != nil {
			return err
		}
		return nil
	}
	existing, err := connection(ctx, f, "holo-pgbouncer", role, f.Passwords[role], "hololive", "verify-full", "", "30000")
	if err != nil {
		return err
	}
	defer existing.Close(ctx)
	var beforePID, afterPID, value int
	if err := existing.QueryRow(ctx, "SELECT pg_backend_pid(), $1::integer", 55).Scan(&beforePID, &value); err != nil {
		return err
	}
	var oldVerifier, newVerifier string
	if err := admin.QueryRow(ctx, "SELECT rolpassword FROM pg_catalog.pg_authid WHERE rolname=$1", role).Scan(&oldVerifier); err != nil {
		return err
	}
	if err := bootstrap(ctx, f, f.Passwords[role]); err != nil {
		return err
	}
	if err := admin.QueryRow(ctx, "SELECT rolpassword FROM pg_catalog.pg_authid WHERE rolname=$1", role).Scan(&newVerifier); err != nil {
		return err
	}
	if err := replaceBackends(ctx, admin); err != nil {
		return err
	}
	if err := existing.QueryRow(ctx, "SELECT pg_backend_pid(), $1::integer", 66).Scan(&afterPID, &value); err != nil {
		return fmt.Errorf("existing frontend after unchanged-password bootstrap (verifier_changed=%t): %w", oldVerifier != newVerifier, err)
	}
	if beforePID == afterPID || value != 66 {
		return errors.New("existing frontend did not reprepare on a replacement backend")
	}
	existing.Close(ctx)
	if err := probe(f.Passwords[role], true); err != nil {
		return err
	}
	fmt.Println("PASS unchanged-password bootstrap with existing/new frontends and replacement backend")
	for _, change := range []struct{ Deny, Restore string }{
		{"ALTER ROLE hololive_runtime VALID UNTIL '2000-01-01'", "ALTER ROLE hololive_runtime VALID UNTIL 'infinity'"},
		{"ALTER ROLE hololive_runtime NOLOGIN", "ALTER ROLE hololive_runtime LOGIN"},
	} {
		if _, err := admin.Exec(ctx, change.Deny); err != nil {
			return err
		}
		denied := probe(f.Passwords[role], false)
		_, restored := admin.Exec(ctx, change.Restore)
		if err := errors.Join(denied, restored); err != nil {
			return err
		}
		if err := probe(f.Passwords[role], true); err != nil {
			return err
		}
	}
	fmt.Println("PASS disabled and expired app authentication rejection")
	if err := bootstrap(ctx, f, f.Rotated); err != nil {
		return err
	}
	if err := replaceBackends(ctx, admin); err != nil {
		return err
	}
	if err := probe(f.Passwords[role], false); err != nil {
		return err
	}
	if err := probe(f.Rotated, true); err != nil {
		return err
	}
	fmt.Println("PASS password rotation without pooler secret update; old password rejected")
	return nil
}

func run(ctx context.Context) error {
	if len(os.Args) < 3 {
		return errors.New("usage: pgbouncer-probe <prepare|auth-client|wrong-auth-source|identity|behavior> <directory|source>")
	}
	if os.Args[1] == "prepare" {
		return prepare(os.Args[2])
	}
	data, err := os.ReadFile("/fixture/fixture.json")
	if err != nil {
		return err
	}
	var f fixture
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	switch os.Args[1] {
	case "auth-client":
		return authClient(ctx, f, false)
	case "wrong-auth-source":
		return authClient(ctx, f, true)
	case "identity":
		return identity(ctx, f, os.Args[2])
	case "behavior":
		admin, err := connection(ctx, f, "/var/run/postgresql", "postgres_admin", f.Passwords["postgres_admin"], "hololive", "disable", "", "")
		if err != nil {
			return err
		}
		defer admin.Close(ctx)
		return errors.Join(
			prepared(ctx, f, admin),
			timing(ctx, f),
			control(ctx, f),
			abruptBackendLoss(ctx, f, admin),
			authChanges(ctx, f, admin),
		)
	default:
		return errors.New("unknown probe mode")
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "pgbouncer product probe:", err)
		os.Exit(1)
	}
	fmt.Println("PASS", os.Args[1], os.Args[2])
}
