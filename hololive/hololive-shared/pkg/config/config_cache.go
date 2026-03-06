package config

// ValkeyConfig: 데이터 캐싱 용도의 Redis(Valkey) 연결 설정
type ValkeyConfig struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
}
