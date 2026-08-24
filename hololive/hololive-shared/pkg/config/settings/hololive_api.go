package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

const (
	defaultBotPort      = 30001
	defaultLLMPort      = 30003
	defaultAdminAPIPort = 30006
)

// HololiveAPIConfig는 단일 hololive-api 프로세스가 호스팅하는 bot/admin/llm HTTP plane과
// YouTube background plane을 담는다. 각 plane은 자체 bounded DB pool을 explicit bulkhead로
// 유지하고, 프로세스 전역의 logging·GC·signal 처리는 부모 runtime이 소유한다.
type HololiveAPIConfig struct {
	Bot     *Config
	Admin   *Config
	LLM     *LLMSchedulerConfig
	YouTube YouTubePlaneConfig
	Logging LoggingConfig
	Tracing TracingConfig
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

	youtubeConfig, err := loadYouTubePlaneConfig()
	if err != nil {
		return nil, fmt.Errorf("load hololive-api youtube plane: %w", err)
	}

	applySourceObservationWorkerProfile(&youtubeConfig, botConfig.APIWorkerProfile)

	config := &HololiveAPIConfig{
		Bot:     botConfig,
		Admin:   adminConfig,
		LLM:     llmConfig,
		YouTube: youtubeConfig,
		Logging: botConfig.Logging,
		Tracing: botConfig.Tracing,
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("hololive-api config validation failed: %w", err)
	}

	return config, nil
}

func applySourceObservationWorkerProfile(config *YouTubePlaneConfig, profile *APIWorkerProfile) {
	worker := profile.Loaded.Profile.Workers["source_observation"]
	settings := profile.SourceObservation

	config.Enabled = worker.Executor.Enabled
	config.ConsumerWorkers = worker.Executor.ConfiguredWorkers
	config.DBOperationConcurrency = settings.DBOperationConcurrency
	config.ClaimBatchSize = settings.ClaimBatchSize
	config.ClaimInterval = time.Duration(settings.ClaimIntervalMS) * time.Millisecond
	config.ClaimLease = time.Duration(settings.ClaimLeaseMS) * time.Millisecond
	config.TransactionTimeout = time.Duration(settings.TransactionTimeoutMS) * time.Millisecond
	config.ShutdownTimeout = time.Duration(settings.ShutdownTimeoutMS) * time.Millisecond
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

	botPort := sharedenv.Int("SERVER_PORT", defaultBotPort)

	botConfig.Server.Port = botPort
	botConfig.Postgres.PoolMinConns = sharedenv.Int("BOT_POSTGRES_POOL_MIN_CONNS", 1)
	botConfig.Postgres.PoolMaxConns = sharedenv.Int("BOT_POSTGRES_POOL_MAX_CONNS", 4)

	if strings.TrimSpace(adminConfig.BotInternalURL) == "" {
		adminConfig.BotInternalURL = fmt.Sprintf("https://127.0.0.1:%d", botPort)
	}

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
		return errors.New("config must not be nil")
	}

	if c.Bot == nil || c.Admin == nil || c.LLM == nil {
		return errors.New("bot, admin and llm plane configs are required")
	}

	if err := c.validateSharedPlanes(); err != nil {
		return fmt.Errorf("validate shared planes: %w", err)
	}

	if err := c.validateYouTubeBindings(); err != nil {
		return fmt.Errorf("validate youtube bindings: %w", err)
	}

	if err := validateHololiveAPIListenerPorts(c); err != nil {
		return fmt.Errorf("validate hololive API listener ports: %w", err)
	}

	return nil
}

func (c *HololiveAPIConfig) validateSharedPlanes() error {
	if err := validateTracingConfig(c.Tracing); err != nil {
		return fmt.Errorf("validate tracing config: %w", err)
	}

	if err := c.validatePlaneRuntimes(); err != nil {
		return fmt.Errorf("validate plane runtimes: %w", err)
	}

	if err := c.validateAlarmProviders(); err != nil {
		return fmt.Errorf("validate alarm providers: %w", err)
	}

	if err := c.validatePlanePools(); err != nil {
		return fmt.Errorf("validate plane pools: %w", err)
	}

	return nil
}

func (c *HololiveAPIConfig) validateYouTubeBindings() error {
	if err := c.YouTube.Validate(); err != nil {
		return fmt.Errorf("youtube plane: %w", err)
	}

	if err := c.YouTube.validateProductionRetention(c.Bot.Environment); err != nil {
		return fmt.Errorf("youtube plane: %w", err)
	}

	if err := validateYouTubePlaneDatabaseRole(c.Bot.Postgres.User); err != nil {
		return fmt.Errorf("validate youtube plane database role: %w", err)
	}

	return nil
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
		return fmt.Errorf("validate alarm provider URL: %w", err)
	}

	if c.Admin.AlarmServiceURL != c.Bot.AlarmServiceURL {
		return errors.New("bot and admin planes must use the same alarm provider URL")
	}

	return nil
}

func (c *HololiveAPIConfig) validatePlanePools() error {
	if err := validatePlanePool("bot", &c.Bot.Postgres); err != nil {
		return fmt.Errorf("validate plane pool: %w", err)
	}

	if err := validatePlanePool("admin", &c.Admin.Postgres); err != nil {
		return fmt.Errorf("validate plane pool: %w", err)
	}

	if err := validatePlanePool("llm", &c.LLM.Postgres); err != nil {
		return fmt.Errorf("validate plane pool: %w", err)
	}

	return nil
}

func validateAlarmProviderURL(environment, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errors.New("ALARM_INTERNAL_URL is required for hololive-api")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ALARM_INTERNAL_URL is invalid: %w", err)
	}

	if parsed.Host == "" {
		return errors.New("ALARM_INTERNAL_URL must include a host")
	}

	if err := validateAlarmProviderScheme(environment, parsed); err != nil {
		return fmt.Errorf("validate alarm provider scheme: %w", err)
	}

	return nil
}

func validateAlarmProviderScheme(environment string, parsed *url.URL) error {
	if isProductionEnvironment(environment) && parsed.Scheme != schemeHTTPS {
		return errors.New("ALARM_INTERNAL_URL must use https in production")
	}

	if parsed.Scheme != schemeHTTP && parsed.Scheme != schemeHTTPS {
		return errors.New("ALARM_INTERNAL_URL scheme must be http or https")
	}

	return nil
}

func validateYouTubePlaneDatabaseRole(user string) error {
	if strings.TrimSpace(user) != postgresRuntimeRoleUser {
		return fmt.Errorf("youtube plane requires POSTGRES_USER=%s", postgresRuntimeRoleUser)
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
	listeners := []listenerEndpoint{
		{owner: "bot-h3", network: "udp", addr: config.Bot.Server.H3Addr, expectedPort: config.Bot.Server.Port, requirePortMatch: true},
		{owner: "admin-h3", network: "udp", addr: config.Admin.Server.H3Addr, expectedPort: config.Admin.Server.Port, requirePortMatch: true},
		{owner: "llm-h3", network: "udp", addr: config.LLM.Server.H3Addr, expectedPort: config.LLM.Server.Port, requirePortMatch: true},
		{owner: "short-link", network: "tcp", addr: config.Bot.Server.ShortLinkAddr},
		{owner: "metrics", network: "tcp", addr: config.Bot.Server.MetricsAddr},
		{owner: "pprof", network: "tcp", addr: config.Bot.Server.PprofAddr},
	}

	parsed := make([]listenerEndpoint, 0, len(listeners))
	for _, listener := range listeners {
		if err := addListenerEndpoint(&parsed, &listener); err != nil {
			return fmt.Errorf("add listener endpoint: %w", err)
		}
	}

	return nil
}

func addListenerEndpoint(parsed *[]listenerEndpoint, listener *listenerEndpoint) error {
	if strings.TrimSpace(listener.addr) == "" {
		return nil
	}

	endpoint, err := parseListenerEndpoint(listener)
	if err != nil {
		return fmt.Errorf("%s listener: %w", listener.owner, err)
	}

	if previousOwner := overlappingListenerOwner(*parsed, &endpoint); previousOwner != "" {
		return fmt.Errorf(
			"listener endpoint %s/%s:%d is shared by %s and %s",
			endpoint.network,
			endpoint.host,
			endpoint.port,
			previousOwner,
			endpoint.owner,
		)
	}

	*parsed = append(*parsed, endpoint)

	return nil
}
