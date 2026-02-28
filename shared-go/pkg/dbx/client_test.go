package dbx

import (
	"testing"
)

func TestNewLazy(t *testing.T) {
	cfg := Config{
		Host:     "postgres",
		Port:     5432,
		User:     "test",
		Password: "test",
		Name:     "testdb",
	}
	opt := DefaultOpenOptions()

	client := NewLazy(cfg, opt)

	if client == nil {
		t.Fatal("NewLazy returned nil")
	}
	if client.Connected() {
		t.Error("NewLazy should return unconnected client")
	}
	if client.Pool() != nil {
		t.Error("Pool() should be nil before Connect()")
	}
	if client.SQL() != nil {
		t.Error("SQL() should be nil before Connect()")
	}
	if client.Gorm() != nil {
		t.Error("Gorm() should be nil before Connect()")
	}
}

func TestNewLazy_NilLogger(t *testing.T) {
	cfg := Config{Host: "postgres", Port: 5432}
	opt := OpenOptions{}

	client := NewLazy(cfg, opt)

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
		name string
		cfg  Config
		want string
	}{
		{
			name: "TCP connection",
			cfg: Config{
				Host:     "localhost",
				Port:     5432,
				User:     "user",
				Password: "pass",
				Name:     "db",
			},
			want: "host=localhost port=5432 user=user password=pass dbname=db sslmode=disable",
		},
		{
			name: "UDS connection",
			cfg: Config{
				SocketPath: "/var/run/postgresql",
				User:       "user",
				Password:   "pass",
				Name:       "db",
			},
			want: "host=/var/run/postgresql user=user password=pass dbname=db sslmode=disable",
		},
		{
			name: "custom SSL mode",
			cfg: Config{
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
			cfg: Config{
				Host:          "localhost",
				Port:          5432,
				User:          "user",
				Password:      "pass",
				Name:          "db",
				QueryExecMode: "exec",
			},
			want: "host=localhost port=5432 user=user password=pass dbname=db sslmode=disable default_query_exec_mode=exec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.DSN(); got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}
