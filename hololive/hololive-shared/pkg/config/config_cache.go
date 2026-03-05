package config

// valkeyEnvConfig: CACHE_* 환경변수 로딩용 내부 구조체
// envconfig.Process에서만 사용한다.
type valkeyEnvConfig struct {
	Host       string `envconfig:"CACHE_HOST" default:"localhost"`
	Port       string `envconfig:"CACHE_PORT" default:"6379"`
	Password   string `envconfig:"CACHE_PASSWORD"`
	DB         string `envconfig:"CACHE_DB" default:"0"`
	SocketPath string `envconfig:"CACHE_SOCKET_PATH"`
}

// ValkeyConfig: 데이터 캐싱 용도의 Redis(Valkey) 연결 설정
type ValkeyConfig struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
}
