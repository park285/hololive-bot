package settings

type PostgresConfig struct {
	Host          string
	Port          int
	SocketPath    string
	User          string
	Password      string
	Database      string
	SSLMode       string
	SSLRootCert   string
	QueryExecMode string
	PoolMinConns  int
	PoolMaxConns  int
}
