package config

// postgresEnvConfig: POSTGRES_* 환경변수 로딩용 내부 구조체
// envconfig.Process에서만 사용한다.
type postgresEnvConfig struct {
	Host              string `envconfig:"POSTGRES_HOST"`
	Port              string `envconfig:"POSTGRES_PORT"`
	SocketPath        string `envconfig:"POSTGRES_SOCKET_PATH"`
	User              string `envconfig:"POSTGRES_USER"`
	Password          string `envconfig:"POSTGRES_PASSWORD"`
	Database          string `envconfig:"POSTGRES_DB"`
	SSLMode           string `envconfig:"POSTGRES_SSLMODE" default:"require"`
	QueryExecMode     string `envconfig:"POSTGRES_QUERY_EXEC_MODE" default:"cache_statement"`
	PoolMinConns      string `envconfig:"POSTGRES_POOL_MIN_CONNS"`
	PoolMaxConns      string `envconfig:"POSTGRES_POOL_MAX_CONNS"`
	PoolMaxIdleConns  string `envconfig:"POSTGRES_POOL_MAX_IDLE_CONNS"`
	AutoPrepareSchema string `envconfig:"POSTGRES_AUTO_PREPARE_SCHEMA" default:"true"`
}

// PostgresConfig: 메인 데이터베이스(PostgreSQL) 연결 설정
type PostgresConfig struct {
	Host              string
	Port              int
	SocketPath        string // UDS 경로 (비어있으면 TCP 사용)
	User              string
	Password          string
	Database          string
	SSLMode           string
	QueryExecMode     string
	PoolMinConns      int
	PoolMaxConns      int
	PoolMaxIdleConns  int
	AutoPrepareSchema bool
}
