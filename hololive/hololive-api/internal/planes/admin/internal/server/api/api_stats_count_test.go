package api

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestStatsCountsMembersAndAlarmEntriesWithoutAlarmList(t *testing.T) {
	pool := dbtest.NewPool(t)
	logger := slog.New(slog.DiscardHandler)
	repository := member.NewMemberRepository(&databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}, logger)
	members, err := repository.GetAllMembers(t.Context())
	require.NoError(t, err)

	h := &StatsHandler{Handler: &Handler{repository: repository, logger: logger, alarm: &stubAlarmCRUDForServer{
		countAlarmEntries: func(context.Context) (int, error) { return 7, nil },
		getAllAlarmKeys: func(context.Context) ([]*domain.AlarmEntry, error) {
			t.Error("stats loaded full alarm list")

			return nil, nil
		},
	}}}
	memberCount, alarmCount, memberErr, alarmErr := h.collectStats(t.Context())
	require.NoError(t, memberErr)
	require.NoError(t, alarmErr)
	require.Equal(t, len(members), memberCount)
	require.Equal(t, 7, alarmCount)
}
