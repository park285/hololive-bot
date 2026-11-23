package cache

import (
	"log/slog"
	"sync"

	"github.com/valkey-io/valkey-go"
)

type Service struct {
	client    valkey.Client
	logger    *slog.Logger
	closeOnce sync.Once
}

var _ Client = (*Service)(nil)

type Config struct {
	Host         string
	Port         int
	Username     string
	Password     string
	DB           int
	SocketPath   string
	TLSCAFile    string
	DisableCache bool
}
