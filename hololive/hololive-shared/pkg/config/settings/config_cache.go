package settings

type ValkeyConfig struct {
	Host              string
	Port              int
	Username          string
	Password          string
	DB                int
	SocketPath        string
	SentinelMasterSet string
	SentinelAddrs     []string
	SentinelUsername  string
	SentinelPassword  string
	TLSCAFile         string
}
