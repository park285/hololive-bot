package settings

import "time"

type ServerConfig struct {
	Port           int
	APIKey         string
	HTTPTransports []string
	H3Addr         string
	H3CertFile     string
	H3KeyFile      string
	ShortLinkAddr  string

	MetricsAddr string

	PprofAddr string
}

type LoggingConfig struct {
	Level      string
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

type BotConfig struct {
	Prefix                string
	SelfUser              string
	CalendarEntryCacheTTL time.Duration
	SeeMoreFold           bool
	MarkdownReplies       bool
}
