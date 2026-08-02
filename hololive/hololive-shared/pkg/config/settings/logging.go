package settings

import (
	sharedlogging "github.com/park285/shared-go/pkg/logging"
)

func LoggingConfigFrom(cfg *Config) sharedlogging.Config {
	if cfg == nil {
		return sharedlogging.Config{}
	}

	return sharedlogging.Config{
		Level:      cfg.Logging.Level,
		Dir:        cfg.Logging.Dir,
		MaxSizeMB:  cfg.Logging.MaxSizeMB,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAgeDays: cfg.Logging.MaxAgeDays,
		Compress:   cfg.Logging.Compress,
	}
}
