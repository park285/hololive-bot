package config

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
