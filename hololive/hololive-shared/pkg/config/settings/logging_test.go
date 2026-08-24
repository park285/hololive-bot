package settings

import (
	"testing"
)

func TestLoggingConfigFrom(t *testing.T) {
	t.Parallel()

	cfg := &Config{Logging: LoggingConfig{
		Level:      "debug",
		Dir:        "/var/log/hololive",
		MaxSizeMB:  7,
		MaxBackups: 3,
		MaxAgeDays: 14,
		Compress:   true,
	}}

	got := LoggingConfigFrom(cfg)

	if got.Level != cfg.Logging.Level ||
		got.Dir != cfg.Logging.Dir ||
		got.MaxSizeMB != cfg.Logging.MaxSizeMB ||
		got.MaxBackups != cfg.Logging.MaxBackups ||
		got.MaxAgeDays != cfg.Logging.MaxAgeDays ||
		got.Compress != cfg.Logging.Compress {
		t.Fatalf("LoggingConfigFrom() = %+v, want the same values as %+v", got, cfg.Logging)
	}

	if got.Format != "" {
		t.Fatalf("LoggingConfigFrom() Format = %q, want the shared default (json)", got.Format)
	}
}

func TestLoggingConfigFromNilConfig(t *testing.T) {
	t.Parallel()

	if got := LoggingConfigFrom(nil); got != LoggingConfigFrom(&Config{}) {
		t.Fatalf("LoggingConfigFrom(nil) = %+v, want the zero config", got)
	}
}
