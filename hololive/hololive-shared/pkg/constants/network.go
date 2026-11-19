package constants

import "time"

var AppTimeout = struct {
	Build    time.Duration
	Shutdown time.Duration
}{
	Build:    30 * time.Second,
	Shutdown: 10 * time.Second,
}

var RequestTimeout = struct {
	DatabasePing time.Duration
}{
	DatabasePing: 5 * time.Second,
}

var DatabaseConfig = struct {
	MaxOpenConns          int
	MaxIdleConns          int
	ConnMaxLifetime       time.Duration
	ConnMaxLifetimeJitter time.Duration
	ConnMaxIdleTime       time.Duration
}{
	MaxOpenConns:          8,
	MaxIdleConns:          2,
	ConnMaxLifetime:       time.Hour,
	ConnMaxLifetimeJitter: 12 * time.Minute,
	ConnMaxIdleTime:       30 * time.Minute,
}

var QueryTimeout = struct {
	Default time.Duration
}{
	Default: 5 * time.Second,
}

var DatabaseDefaults = struct {
	Host     string
	Port     int
	User     string
	Database string
}{
	Host:     "postgres",
	Port:     5432,
	User:     "hololive_runtime",
	Database: "hololive",
}
