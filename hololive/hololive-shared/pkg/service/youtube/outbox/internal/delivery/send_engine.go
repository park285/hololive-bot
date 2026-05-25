package delivery

import (
	"log/slog"
	"sync"

	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

type SendEngine struct {
	sender          messagedelivery.MessageSender
	formatter       *MessageFormatter
	logger          *slog.Logger
	config          Config
	karingMu        sync.Mutex
	claims          ClaimResolver
	auditLogger     *AuditLogger
	metricsRecorder *MetricsRecorder
}

func newSendEngine(
	sender messagedelivery.MessageSender,
	formatter *MessageFormatter,
	logger *slog.Logger,
	config Config,
	claims ClaimResolver,
	auditLogger *AuditLogger,
	metricsRecorder *MetricsRecorder,
) *SendEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &SendEngine{
		sender:          sender,
		formatter:       formatter,
		logger:          logger,
		config:          config,
		claims:          claims,
		auditLogger:     auditLogger,
		metricsRecorder: metricsRecorder,
	}
}
