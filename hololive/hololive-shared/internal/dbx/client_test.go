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

package dbx

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewLazy(t *testing.T) {
	config := Config{
		Host:     "postgres",
		Port:     5432,
		User:     "test",
		Password: "test",
		Name:     "testdb",
	}
	opt := DefaultOpenOptions()

	client := NewLazy(config, opt)

	if client == nil {
		t.Fatal("NewLazy returned nil")
	}
	if client.Connected() {
		t.Error("NewLazy should return unconnected client")
	}
	if client.Pool() != nil {
		t.Error("Pool() should be nil before Connect()")
	}
}

func TestNewLazy_NilLogger(t *testing.T) {
	config := Config{Host: "postgres", Port: 5432}
	opt := OpenOptions{}

	client := NewLazy(config, opt)

	if client == nil {
		t.Fatal("NewLazy returned nil")
	}
	if client.opt.Logger == nil {
		t.Error("Logger should be set to default")
	}
}

func TestNormalizePoolConfig(t *testing.T) {
	pool := normalizePoolConfig(PoolConfig{})

	if pool.MinConns != 2 {
		t.Errorf("MinConns = %d, want 2", pool.MinConns)
	}
	if pool.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", pool.MaxConns)
	}
}

func TestConfigDSN(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		envRootCert string
		want        string
	}{
		{
			name: "TCP connection",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Name:     "db",
			},
			want: "host='localhost' port=5432 user='user' password='pass' dbname='db' sslmode='verify-full'",
		},
		{
			name: "UDS connection",
			config: Config{
				SocketPath: "/var/run/postgresql",
				User:       "user",
				Password:   "pass",
				Name:       "db",
			},
			want: "host='/var/run/postgresql' user='user' password='pass' dbname='db' sslmode='verify-full'",
		},
		{
			name: "custom SSL mode",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Name:     "db",
				SSLMode:  "require",
			},
			want: "host='localhost' port=5432 user='user' password='pass' dbname='db' sslmode='require'",
		},
		{
			name: "query exec mode",
			config: Config{
				Host:          "localhost",
				Port:          5432,
				User:          "user",
				Password:      "pass",
				Name:          "db",
				QueryExecMode: "exec",
			},
			want: "host='localhost' port=5432 user='user' password='pass' dbname='db' sslmode='verify-full' default_query_exec_mode='exec'",
		},
		{
			name: "ssl root cert from config",
			config: Config{
				Host:        "localhost",
				Port:        5432,
				User:        "user",
				Password:    "pass",
				Name:        "db",
				SSLMode:     "verify-full",
				SSLRootCert: "/run/postgresql/root.crt",
			},
			want: "host='localhost' port=5432 user='user' password='pass' dbname='db' sslmode='verify-full' sslrootcert='/run/postgresql/root.crt'",
		},
		{
			name: "ssl root cert from env",
			config: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Name:     "db",
				SSLMode:  "verify-full",
			},
			envRootCert: "/run/postgresql/env-root.crt",
			want:        "host='localhost' port=5432 user='user' password='pass' dbname='db' sslmode='verify-full' sslrootcert='/run/postgresql/env-root.crt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POSTGRES_SSLROOTCERT", tt.envRootCert)
			if got := tt.config.DSN(); got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigDSNQuotesSSLRootCertToPreventKeywordInjection(t *testing.T) {
	t.Setenv("POSTGRES_SSLROOTCERT", "")

	dir := t.TempDir()
	truncatedRootCertPath := filepath.Join(dir, "root")
	injectedRootCertPath := truncatedRootCertPath + " sslmode=disable"
	writeTestCACert(t, truncatedRootCertPath, "truncated.example.test")
	intendedCert := writeTestCACert(t, injectedRootCertPath, "intended.example.test")

	config := Config{
		Host:        "db.example.test",
		Port:        5432,
		User:        "user",
		Password:    "pass",
		Name:        "db",
		SSLMode:     "verify-full",
		SSLRootCert: injectedRootCertPath,
	}

	dsn := config.DSN()
	want := "host='db.example.test' port=5432 user='user' password='pass' dbname='db' sslmode='verify-full' sslrootcert='" + injectedRootCertPath + "'"
	if dsn != want {
		t.Fatalf("DSN() = %q, want %q", dsn, want)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}

	tlsConfig := poolConfig.ConnConfig.Config.TLSConfig
	if tlsConfig == nil {
		t.Fatal("TLSConfig = nil, want verify-full TLS config")
	}
	if tlsConfig.ServerName != "db.example.test" {
		t.Fatalf("TLSConfig.ServerName = %q, want %q", tlsConfig.ServerName, "db.example.test")
	}
	if _, err := intendedCert.Verify(x509.VerifyOptions{Roots: tlsConfig.RootCAs, DNSName: "intended.example.test"}); err != nil {
		t.Fatalf("TLSConfig.RootCAs did not load literal sslrootcert value: %v", err)
	}
}

func TestConfigDSNQuotesKeywordValues(t *testing.T) {
	t.Setenv("POSTGRES_SSLROOTCERT", "")

	config := Config{
		Host:     "db.example.test",
		Port:     5432,
		User:     "app user",
		Password: `pa's\word`,
		Name:     "main db",
		SSLMode:  "disable",
	}

	dsn := config.DSN()
	want := `host='db.example.test' port=5432 user='app user' password='pa\'s\\word' dbname='main db' sslmode='disable'`
	if dsn != want {
		t.Fatalf("DSN() = %q, want %q", dsn, want)
	}

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	connConfig := poolConfig.ConnConfig.Config
	if connConfig.User != config.User {
		t.Fatalf("User = %q, want %q", connConfig.User, config.User)
	}
	if connConfig.Password != config.Password {
		t.Fatalf("Password = %q, want %q", connConfig.Password, config.Password)
	}
	if connConfig.Database != config.Name {
		t.Fatalf("Database = %q, want %q", connConfig.Database, config.Name)
	}
	if connConfig.TLSConfig != nil {
		t.Fatal("TLSConfig != nil, want sslmode=disable to remain a single parsed keyword")
	}
}

func TestTryConnect_ParseConfigError_MasksPassword(t *testing.T) {
	client := &Client{opt: DefaultOpenOptions()}
	config := Config{
		Host:     "localhost",
		Port:     5432,
		User:     "user",
		Password: "mask",
		Name:     "db",
		SSLMode:  "invalid",
	}

	_, err := client.tryConnect(context.Background(), config, DefaultPoolConfig(), DefaultRetryConfig())
	if err == nil {
		t.Fatal("expected parse config error, got nil")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, config.Password) {
		t.Fatalf("error message must not leak raw password: %q", errMsg)
	}
	if !strings.Contains(errMsg, "sslmode is invalid") {
		t.Fatalf("expected sslmode parse error, got: %q", errMsg)
	}
}

func writeTestCACert(t *testing.T, path, commonName string) *x509.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("generate CA serial: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{commonName},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	return cert
}
