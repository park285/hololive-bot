// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package settings

import (
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	sharedenv "github.com/park285/shared-go/pkg/envutil"
)

// Deprecated: current admin-api runtime uses config.Load() with SERVER_PORT=30006.
// This compatibility config remains only for legacy callers until cleanup lands.
type AdminAPIConfig struct {
	Server          ServerConfig
	Valkey          ValkeyConfig
	Postgres        PostgresConfig
	Holodex         HolodexConfig
	CORS            CORSConfig
	Environment     string
	Services        ServicesConfig
	Logging         LoggingConfig
	LLMSchedulerURL string // llm-scheduler 내부 트리거 프록시 URL
	Version         string
}

// Deprecated: current admin-api runtime uses config.Load() instead of LoadAdminAPI.
func LoadAdminAPI() (*AdminAPIConfig, error) {
	_ = godotenv.Load()

	config := buildAdminAPIConfig()

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("admin api config validation failed: %w", err)
	}

	return config, nil
}

func buildAdminAPIConfig() *AdminAPIConfig {
	_, _, corsAllowedOrigins, corsMissingInProduction := loadRuntimeTokensAndCORS()
	llmSchedulerHealthURL := sharedenv.StringAny(
		"SERVICES_LLM_SCHEDULER_HEALTH_URL",
		"SERVICES_LLM_SERVER_HEALTH_URL",
	)

	return &AdminAPIConfig{
		Server: ServerConfig{
			Port:   sharedenv.Int("ADMIN_API_PORT", 30006),
			APIKey: sharedenv.String("API_SECRET_KEY", ""),
		},
		Valkey:   loadValkeyConfig(),
		Postgres: loadPostgresConfig(),
		Holodex: HolodexConfig{
			BaseURL: sharedenv.String("HOLODEX_BASE_URL", DefaultHolodexOperationalConfig().BaseURL),
			APIKey:  resolveHolodexAPIKey(),
		},
		CORS: CORSConfig{
			AllowedOrigins:      corsAllowedOrigins,
			Enforce:             sharedenv.Bool("CORS_ENFORCE", false),
			MissingInProduction: corsMissingInProduction,
		},
		Environment: loadAppEnvironment(),
		Services: ServicesConfig{
			LLMSchedulerHealthURL:   llmSchedulerHealthURL,
			GameBotTwentyQHealthURL: sharedenv.String("SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL", ""),
			GameBotTurtleHealthURL:  sharedenv.String("SERVICES_GAME_BOT_TURTLE_HEALTH_URL", ""),
		},
		Logging: LoggingConfig{
			Level: sharedenv.String("LOG_LEVEL", "info"),
		},
		LLMSchedulerURL: sharedenv.String("LLM_SCHEDULER_INTERNAL_URL", ""),
		Version:         sharedenv.String("APP_VERSION", "1.0.0-bot-admin"),
	}
}

// validate: 필수 설정값을 검증합니다.
func (c *AdminAPIConfig) validate() error {
	if err := validateAPISecretKey(c.Environment, c.Server.APIKey); err != nil {
		return err
	}
	if strings.TrimSpace(c.Holodex.APIKey) == "" {
		return fmt.Errorf("HOLODEX_API_KEY is required")
	}
	if err := validatePostgresSSLMode(c.Environment, c.Postgres.SSLMode); err != nil {
		return err
	}
	return nil
}
