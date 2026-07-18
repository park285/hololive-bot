package celebration

import (
	"log/slog"
	"time"
)

type RunnerConfig struct {
	CheckHourKST int
	RunInterval  time.Duration
}

func NewRunner(
	memberRepo MemberRepository,
	alarmRepo AlarmRoomRepository,
	publisher Publisher,
	logger *slog.Logger,
	config RunnerConfig,
) *Runner {
	return &Runner{
		memberRepo:   memberRepo,
		alarmRepo:    alarmRepo,
		publisher:    publisher,
		logger:       logger,
		checkHourKST: config.CheckHourKST,
		runInterval:  config.RunInterval,
	}
}
