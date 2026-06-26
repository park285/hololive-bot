package settings

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

const (
	defaultBotPort      = 30001
	defaultLLMPort      = 30003
	defaultAdminAPIPort = 30006
)

// HololiveAPIConfig contains the three logical planes hosted by the single
// hololive-api process. Each plane retains its own bounded DB pool as an
// explicit bulkhead, while process-wide logging, GC and signal handling are
// owned by the parent runtime.
type HololiveAPIConfig struct {
	Bot     *Config
	Admin   *Config
	LLM     *LLMSchedulerConfig
	Logging LoggingConfig
}

func LoadHololiveAPIRuntime() (*HololiveAPIConfig, error) {
	botConfig, err := LoadBotRuntime()
	if err != nil {
		return nil, fmt.Errorf("load hololive-api bot plane: %w", err)
	}
	adminConfig, err := LoadAdminAPIRuntime()
	if err != nil {
		return nil, fmt.Errorf("load hololive-api admin plane: %w", err)
	}
	llmConfig, err := LoadLLMSchedulerRuntime()
	if err != nil {
		return nil, fmt.Errorf("load hololive-api llm plane: %w", err)
	}

	configureHololiveAPIPlanes(botConfig, adminConfig, llmConfig)

	config := &HololiveAPIConfig{
		Bot:     botConfig,
		Admin:   adminConfig,
		LLM:     llmConfig,
		Logging: botConfig.Logging,
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("hololive-api config validation failed: %w", err)
	}
	return config, nil
}

func configureHololiveAPIPlanes(botConfig, adminConfig *Config, llmConfig *LLMSchedulerConfig) {
	adminPort := sharedenv.Int("HOLOLIVE_ADMIN_API_PORT", defaultAdminAPIPort)
	adminConfig.Server.Port = adminPort
	adminConfig.Server.HTTPTransports = parseCommaSeparated(sharedenv.String("HOLOLIVE_ADMIN_API_HTTP_TRANSPORTS", "h3"))
	adminConfig.Server.H3Addr = sharedenv.String("HOLOLIVE_ADMIN_API_H3_ADDR", fmt.Sprintf(":%d", adminPort))
	adminConfig.Server.H3CertFile = botConfig.Server.H3CertFile
	adminConfig.Server.H3KeyFile = botConfig.Server.H3KeyFile
	adminConfig.Server.MetricsAddr = ""
	adminConfig.Server.PprofAddr = ""
	adminConfig.Postgres.PoolMinConns = sharedenv.Int("ADMIN_API_POSTGRES_POOL_MIN_CONNS", 1)
	adminConfig.Postgres.PoolMaxConns = sharedenv.Int("ADMIN_API_POSTGRES_POOL_MAX_CONNS", 4)

	llmPort := sharedenv.Int("LLM_SCHEDULER_PORT", defaultLLMPort)
	llmConfig.Server.Port = llmPort
	llmConfig.Server.HTTPTransports = parseCommaSeparated(sharedenv.String("HOLOLIVE_LLM_SCHEDULER_HTTP_TRANSPORTS", "h3"))
	llmConfig.Server.H3Addr = sharedenv.String("HOLOLIVE_LLM_SCHEDULER_H3_ADDR", fmt.Sprintf(":%d", llmPort))
	llmConfig.Server.H3CertFile = botConfig.Server.H3CertFile
	llmConfig.Server.H3KeyFile = botConfig.Server.H3KeyFile
	llmConfig.Server.MetricsAddr = ""
	llmConfig.Server.PprofAddr = ""
	llmConfig.Postgres.PoolMinConns = sharedenv.Int("LLM_SCHEDULER_POSTGRES_POOL_MIN_CONNS", 1)
	llmConfig.Postgres.PoolMaxConns = sharedenv.Int("LLM_SCHEDULER_POSTGRES_POOL_MAX_CONNS", 4)

	botConfig.Server.Port = sharedenv.Int("SERVER_PORT", defaultBotPort)
	botConfig.Postgres.PoolMinConns = sharedenv.Int("BOT_POSTGRES_POOL_MIN_CONNS", 1)
	botConfig.Postgres.PoolMaxConns = sharedenv.Int("BOT_POSTGRES_POOL_MAX_CONNS", 4)

	llmLoopbackURL := fmt.Sprintf("https://127.0.0.1:%d", llmPort)
	botConfig.LLMSchedulerURL = llmLoopbackURL
	botConfig.Services.LLMSchedulerHealthURL = llmLoopbackURL + "/health"
	adminConfig.LLMSchedulerURL = llmLoopbackURL
	adminConfig.Services.LLMSchedulerHealthURL = llmLoopbackURL + "/health"

	alarmURL := strings.TrimSpace(sharedenv.String("ALARM_INTERNAL_URL", ""))
	botConfig.AlarmServiceURL = alarmURL
	adminConfig.AlarmServiceURL = alarmURL
}

func (c *HololiveAPIConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("config must not be nil")
	}
	if c.Bot == nil || c.Admin == nil || c.LLM == nil {
		return fmt.Errorf("bot, admin and llm plane configs are required")
	}
	if err := c.validatePlaneRuntimes(); err != nil {
		return err
	}
	if err := c.validateAlarmProviders(); err != nil {
		return err
	}
	if err := c.validatePlanePools(); err != nil {
		return err
	}
	return validateHololiveAPIListenerPorts(c)
}

func (c *HololiveAPIConfig) validatePlaneRuntimes() error {
	if err := c.Bot.ValidateBotRuntime(); err != nil {
		return fmt.Errorf("bot plane: %w", err)
	}
	if err := c.Admin.ValidateAdminAPIRuntime(); err != nil {
		return fmt.Errorf("admin plane: %w", err)
	}
	if err := c.LLM.validateRuntime(); err != nil {
		return fmt.Errorf("llm plane: %w", err)
	}
	return nil
}

func (c *HololiveAPIConfig) validateAlarmProviders() error {
	if err := validateAlarmProviderURL(c.Bot.Environment, c.Bot.AlarmServiceURL); err != nil {
		return err
	}
	if c.Admin.AlarmServiceURL != c.Bot.AlarmServiceURL {
		return fmt.Errorf("bot and admin planes must use the same alarm provider URL")
	}
	return nil
}

func (c *HololiveAPIConfig) validatePlanePools() error {
	if err := validatePlanePool("bot", &c.Bot.Postgres); err != nil {
		return err
	}
	if err := validatePlanePool("admin", &c.Admin.Postgres); err != nil {
		return err
	}
	if err := validatePlanePool("llm", &c.LLM.Postgres); err != nil {
		return err
	}
	return nil
}

func validateAlarmProviderURL(environment, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("ALARM_INTERNAL_URL is required for hololive-api")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ALARM_INTERNAL_URL is invalid: %w", err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("ALARM_INTERNAL_URL must include a host")
	}
	if err := validateAlarmProviderScheme(environment, parsed); err != nil {
		return err
	}
	return nil
}

func validateAlarmProviderScheme(environment string, parsed *url.URL) error {
	if isProductionEnvironment(environment) && parsed.Scheme != "https" {
		return fmt.Errorf("ALARM_INTERNAL_URL must use https in production")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("ALARM_INTERNAL_URL scheme must be http or https")
	}
	return nil
}

func validatePlanePool(plane string, config *PostgresConfig) error {
	if config.PoolMinConns < 0 {
		return fmt.Errorf("%s POSTGRES_POOL_MIN_CONNS must be >= 0", plane)
	}
	if config.PoolMaxConns <= 0 {
		return fmt.Errorf("%s POSTGRES_POOL_MAX_CONNS must be positive", plane)
	}
	if config.PoolMinConns > config.PoolMaxConns {
		return fmt.Errorf("%s POSTGRES_POOL_MIN_CONNS must be <= POSTGRES_POOL_MAX_CONNS", plane)
	}
	return nil
}

func validateHololiveAPIListenerPorts(config *HololiveAPIConfig) error {
	owners := make(map[int]string)
	if err := registerPrimaryListenerPorts(owners, config); err != nil {
		return err
	}
	return registerAuxiliaryListenerPorts(owners, config)
}

func registerPrimaryListenerPorts(owners map[int]string, config *HololiveAPIConfig) error {
	listeners := []struct {
		owner string
		port  int
	}{
		{owner: "bot", port: config.Bot.Server.Port},
		{owner: "admin", port: config.Admin.Server.Port},
		{owner: "llm", port: config.LLM.Server.Port},
	}
	for _, listener := range listeners {
		if listener.port <= 0 || listener.port > 65535 {
			return fmt.Errorf("%s listener port must be between 1 and 65535", listener.owner)
		}
		if err := recordListenerPort(owners, listener.owner, listener.port); err != nil {
			return err
		}
	}
	return nil
}

func registerAuxiliaryListenerPorts(owners map[int]string, config *HololiveAPIConfig) error {
	auxiliaries := []struct {
		owner string
		addr  string
	}{
		{owner: "metrics", addr: config.Bot.Server.MetricsAddr},
		{owner: "pprof", addr: config.Bot.Server.PprofAddr},
	}
	for _, auxiliary := range auxiliaries {
		if strings.TrimSpace(auxiliary.addr) == "" {
			continue
		}
		port, err := listenerPort(auxiliary.addr)
		if err != nil {
			return fmt.Errorf("%s listener: %w", auxiliary.owner, err)
		}
		if err := recordListenerPort(owners, auxiliary.owner, port); err != nil {
			return err
		}
	}
	return nil
}

func recordListenerPort(owners map[int]string, owner string, port int) error {
	if previous, exists := owners[port]; exists {
		return fmt.Errorf("listener port %d is shared by %s and %s", port, previous, owner)
	}
	owners[port] = owner
	return nil
}

func listenerPort(addr string) (int, error) {
	_, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", addr, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port in address %q", addr)
	}
	return port, nil
}
