package database

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// 한 프로세스의 여러 pool은 pool 라벨로 공존하고, 닫힌 pool의 이름은 해제 뒤 다시 쓸 수 있어야 한다.
// 같은 이름을 동시에 쓰면 어느 pool의 통계인지 섞이므로 등록 오류로 드러낸다.
func TestPoolStatsCoexistByPoolNameAndReleaseOnUnregister(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	bot := lazyPool(t, 3)
	admin := lazyPool(t, 5)

	unregisterBot, err := registerPoolStats(reg, bot, "bot")
	require.NoError(t, err)

	unregisterAdmin, err := registerPoolStats(reg, admin, "admin")
	require.NoError(t, err)

	defer unregisterAdmin()

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP hololive_db_pool_max_conns Configured maximum size of the pgx pool.
# TYPE hololive_db_pool_max_conns gauge
hololive_db_pool_max_conns{pool="admin"} 5
hololive_db_pool_max_conns{pool="bot"} 3
# HELP hololive_db_pool_conns Current pgx pool connections by state.
# TYPE hololive_db_pool_conns gauge
hololive_db_pool_conns{pool="admin",state="acquired"} 0
hololive_db_pool_conns{pool="admin",state="constructing"} 0
hololive_db_pool_conns{pool="admin",state="idle"} 0
hololive_db_pool_conns{pool="bot",state="acquired"} 0
hololive_db_pool_conns{pool="bot",state="constructing"} 0
hololive_db_pool_conns{pool="bot",state="idle"} 0
`), "hololive_db_pool_max_conns", "hololive_db_pool_conns"))

	_, err = registerPoolStats(reg, lazyPool(t, 1), "bot")
	require.Error(t, err, "a second live pool must not reuse the bot label")

	unregisterBot()

	unregisterRebuilt, err := registerPoolStats(reg, lazyPool(t, 7), "bot")
	require.NoError(t, err, "a rebuilt pool reuses the name after the old one is unregistered")

	defer unregisterRebuilt()

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP hololive_db_pool_max_conns Configured maximum size of the pgx pool.
# TYPE hololive_db_pool_max_conns gauge
hololive_db_pool_max_conns{pool="admin"} 5
hololive_db_pool_max_conns{pool="bot"} 7
`), "hololive_db_pool_max_conns"))
}

// lazyPool은 연결을 만들지 않는 pool이다. MinConns가 0인 pgxpool은 첫 Acquire 전까지 접속하지 않는다.
func lazyPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()

	config, err := pgxpool.ParseConfig("postgres://hololive_test@127.0.0.1:1/hololive_test?sslmode=disable")
	require.NoError(t, err)

	config.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool
}

func TestRuntimeControlPoolsRejectMaxOneBeforeDial(t *testing.T) {
	for _, name := range []string{"bot", "youtube_plane", "alarm_worker", "collector"} {
		t.Run(name, func(t *testing.T) {
			resources, cleanup, err := ProvideDatabaseResources(t.Context(), &settings.PostgresConfig{
				Host: "127.0.0.1", Port: 1, User: "test", Database: "test", SSLMode: "disable",
				PoolMinConns: 0, PoolMaxConns: 1,
			}, RuntimePool{Name: name, ReservedConns: 1}, nil)
			require.ErrorContains(t, err, "acquire policy")
			require.Nil(t, resources)
			require.Nil(t, cleanup)
		})
	}
}
