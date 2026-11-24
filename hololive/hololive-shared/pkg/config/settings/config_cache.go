package settings

type ValkeyConfig struct {
	Host       string
	Port       int
	Username   string
	Password   string
	DB         int
	SocketPath string
	TLSCAFile  string
}
