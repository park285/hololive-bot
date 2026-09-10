package config

import (
	"net"
	"net/netip"
	"strconv"
	"time"
)

type SecurityMode string

const (
	SecurityEnforce SecurityMode = "enforce"
	SecurityMonitor SecurityMode = "monitor"
	SecurityOff     SecurityMode = "off"

	minimumAdminPasswordBcryptCost = 10
)

type SecurityConfig struct {
	AllowedOrigins       []string
	AllowLocalhostInProd bool
	CSRFMode             SecurityMode
	WSOriginMode         SecurityMode
	ForceHTTPS           bool
}

type SessionConfig struct {
	TokenRotationEnabled  bool
	HeartbeatInterval     time.Duration
	ExpiryDuration        time.Duration
	AbsoluteTimeout       time.Duration
	AbsoluteWarningWindow time.Duration
	IdleTimeout           time.Duration
	IdleWarningTimeout    time.Duration
	IdleSessionTTL        time.Duration
	GracePeriod           time.Duration
	RotationInterval      time.Duration
}

type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

type Config struct {
	Port              uint16
	Env               string
	AdminUser         string
	AdminPassHash     string
	SessionSecret     string
	ValkeyURL         string
	DockerHost        string
	HoloAdminAPIURL   string
	HoloBotAPIKey     string
	EnableOpenAPI     bool
	EnableSwaggerUI   bool
	Logging           LoggingConfig
	Security          SecurityConfig
	Session           SessionConfig
	RuntimeVersion    string
	TrustedForwarders bool
	TrustedProxyCIDRs []netip.Prefix
}

func (c *Config) ListenAddr() string {
	return net.JoinHostPort("0.0.0.0", strconv.Itoa(int(c.Port)))
}

func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		TokenRotationEnabled:  true,
		HeartbeatInterval:     5 * time.Minute,
		ExpiryDuration:        30 * time.Minute,
		AbsoluteTimeout:       8 * time.Hour,
		AbsoluteWarningWindow: 5 * time.Minute,
		IdleTimeout:           10 * time.Minute,
		IdleWarningTimeout:    9 * time.Minute,
		IdleSessionTTL:        10 * time.Second,
		GracePeriod:           30 * time.Second,
		RotationInterval:      15 * time.Minute,
	}
}
