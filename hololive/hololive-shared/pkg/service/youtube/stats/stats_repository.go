package stats

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kapu/hololive-shared/pkg/service/database"
)

type statsRepositoryDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StatsRepository: YouTube 채널 통계 데이터(구독자 수 등)를 관리하는 저장소 (TimescaleDB)
type StatsRepository struct {
	pool   statsRepositoryDB
	logger *slog.Logger

	mu                   sync.RWMutex
	latestTableAvailable bool
}

// NewYouTubeStatsRepository: 새로운 StatsRepository 인스턴스를 생성합니다.
func NewYouTubeStatsRepository(postgres database.Client, logger *slog.Logger) *StatsRepository {
	return &StatsRepository{
		pool:                 postgres.GetPool(),
		logger:               logger,
		latestTableAvailable: true,
	}
}
