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
	"strings"
	"testing"
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
	if pool.MaxIdleConns != 2 {
		t.Errorf("MaxIdleConns = %d, want 2", pool.MaxIdleConns)
	}
}

func TestConfigDSN(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
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
			want: "host=localhost port=5432 user=user password=pass dbname=db sslmode=require",
		},
		{
			name: "UDS connection",
			config: Config{
				SocketPath: "/var/run/postgresql",
				User:       "user",
				Password:   "pass",
				Name:       "db",
			},
			want: "host=/var/run/postgresql user=user password=pass dbname=db sslmode=require",
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
			want: "host=localhost port=5432 user=user password=pass dbname=db sslmode=require",
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
			want: "host=localhost port=5432 user=user password=pass dbname=db sslmode=require default_query_exec_mode=exec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.DSN(); got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
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
