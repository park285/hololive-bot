package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	iris "github.com/park285/iris-client-go/client"

	"github.com/kapu/settlement-go/pkg/settlement"
)

func main() {
	cfg := loadConfig()
	logger, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		logger.Error("PostgreSQL 연결 실패", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	cacheCfg := cache.Config{
		Host:       envOrDefault("CACHE_HOST", "localhost"),
		Port:       intEnvOrDefault("CACHE_PORT", 6379),
		SocketPath: os.Getenv("CACHE_SOCKET_PATH"),
	}
	cacheService, err := cache.NewCacheService(ctx, cacheCfg, logger)
	if err != nil {
		logger.Error("Valkey 연결 실패", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer cacheService.Close()

	irisClient := iris.NewH2CClient(cfg.irisBaseURL, cfg.irisBotToken, iris.WithLogger(logger))

	repo := settlement.NewRepository(pool, logger)
	svc := settlement.NewService(repo)
	formatter := &messageFormatter{}

	bot := &botHandler{
		svc:        svc,
		iris:       irisClient,
		formatter:  formatter,
		allowRooms: cfg.allowRooms,
		logger:     logger,
	}

	if cfg.alarmRoomID != "" {
		scheduler := settlement.NewScheduler(
			svc,
			cacheService,
			formatter.formatAlarm,
			func(ctx context.Context, room, message string) error {
				return irisClient.SendMessage(ctx, room, message)
			},
			cfg.alarmRoomID,
			logger,
		)
		go scheduler.Start(ctx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/iris", bot.handleWebhook)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	server := newHTTPServer(":"+cfg.port, mux)

	go func() {
		logger.Info("Settlement bot started", slog.String("port", cfg.port), slog.Int("allow_rooms", len(cfg.allowRooms)))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP 서버 오류", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("서버 종료 오류", slog.String("error", err.Error()))
	}
}

type appConfig struct {
	port          string
	databaseURL   string
	irisBaseURL   string
	irisBotToken  string
	alarmRoomID   string
	allowRooms    map[string]bool
	logDir        string
	logLevel      string
	logMaxSizeMB  int
	logMaxBackups int
	logMaxAgeDays int
	logCompress   bool
}

func loadConfig() appConfig {
	cfg := appConfig{
		port:          envOrDefault("SETTLEMENT_PORT", "30002"),
		databaseURL:   buildDatabaseURL(),
		irisBaseURL:   envOrDefault("IRIS_BASE_URL", "http://localhost:3000"),
		irisBotToken:  os.Getenv("IRIS_BOT_TOKEN"),
		alarmRoomID:   os.Getenv("SETTLEMENT_ROOM_ID"),
		allowRooms:    make(map[string]bool),
		logDir:        os.Getenv("LOG_DIR"),
		logLevel:      envOrDefault("LOG_LEVEL", "info"),
		logMaxSizeMB:  intEnvOrDefault("LOG_MAX_SIZE_MB", 100),
		logMaxBackups: intEnvOrDefault("LOG_MAX_BACKUPS", 5),
		logMaxAgeDays: intEnvOrDefault("LOG_MAX_AGE_DAYS", 30),
		logCompress:   boolEnvOrDefault("LOG_COMPRESS", true),
	}

	for _, room := range strings.Split(os.Getenv("SETTLEMENT_ALLOW_ROOMS"), ",") {
		room = strings.TrimSpace(room)
		if room != "" {
			cfg.allowRooms[room] = true
		}
	}

	return cfg
}

func newLogger(cfg appConfig) (*slog.Logger, error) {
	return sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
		Dir:        cfg.logDir,
		MaxSizeMB:  cfg.logMaxSizeMB,
		MaxBackups: cfg.logMaxBackups,
		MaxAgeDays: cfg.logMaxAgeDays,
		Compress:   cfg.logCompress,
	}, "settlement-bot.log", cfg.logLevel)
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           sharedserver.WrapH2C(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func buildDatabaseURL() string {
	host := envOrDefault("POSTGRES_HOST", "localhost")
	port := envOrDefault("POSTGRES_PORT", "5433")
	db := envOrDefault("POSTGRES_DB", "hololive")
	user := envOrDefault("POSTGRES_USER", "hololive_runtime")
	password := os.Getenv("POSTGRES_PASSWORD")
	sslmode := envOrDefault("POSTGRES_SSLMODE", "require")

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, db, sslmode)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnvOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return fallback
	}
	return n
}

func boolEnvOrDefault(key string, fallback bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	case "":
		return fallback
	default:
		return fallback
	}
}
