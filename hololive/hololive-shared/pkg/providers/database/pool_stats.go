package database

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// poolStatsCollector는 scrape마다 pgxpool.Stat()을 한 번 읽어 pool 포화·획득 대기를 노출한다.
// 같은 프로세스의 여러 pool은 pool 상수 라벨로 구분한다.
type poolStatsCollector struct {
	pool             *pgxpool.Pool
	acquires         *prometheus.Desc
	acquireDuration  *prometheus.Desc
	emptyAcquires    *prometheus.Desc
	emptyAcquireWait *prometheus.Desc
	canceledAcquires *prometheus.Desc
	conns            *prometheus.Desc
	maxConns         *prometheus.Desc
}

func newPoolStatsCollector(pool *pgxpool.Pool, poolName string) *poolStatsCollector {
	labels := prometheus.Labels{"pool": poolName}
	desc := func(name, help string, variableLabels ...string) *prometheus.Desc {
		return prometheus.NewDesc("hololive_db_pool_"+name, help, variableLabels, labels)
	}

	return &poolStatsCollector{
		pool:             pool,
		acquires:         desc("acquires_total", "Cumulative successful connection acquisitions from the pgx pool."),
		acquireDuration:  desc("acquire_duration_seconds_total", "Cumulative time spent in successful pgx pool acquisitions."),
		emptyAcquires:    desc("empty_acquires_total", "Cumulative successful acquisitions that waited because the pgx pool had no idle connection."),
		emptyAcquireWait: desc("empty_acquire_wait_seconds_total", "Cumulative time successful acquisitions waited for a connection while the pgx pool was empty."),
		canceledAcquires: desc("canceled_acquires_total", "Cumulative pgx pool acquisitions canceled by their context."),
		conns:            desc("conns", "Current pgx pool connections by state.", "state"),
		maxConns:         desc("max_conns", "Configured maximum size of the pgx pool."),
	}
}

func (c *poolStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.acquires, c.acquireDuration, c.emptyAcquires, c.emptyAcquireWait, c.canceledAcquires, c.conns, c.maxConns,
	} {
		ch <- desc
	}
}

func (c *poolStatsCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()

	for _, metric := range []prometheus.Metric{
		prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(stat.AcquireCount())),
		prometheus.MustNewConstMetric(c.acquireDuration, prometheus.CounterValue, stat.AcquireDuration().Seconds()),
		prometheus.MustNewConstMetric(c.emptyAcquires, prometheus.CounterValue, float64(stat.EmptyAcquireCount())),
		prometheus.MustNewConstMetric(c.emptyAcquireWait, prometheus.CounterValue, stat.EmptyAcquireWaitTime().Seconds()),
		prometheus.MustNewConstMetric(c.canceledAcquires, prometheus.CounterValue, float64(stat.CanceledAcquireCount())),
		prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(stat.AcquiredConns()), "acquired"),
		prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(stat.IdleConns()), "idle"),
		prometheus.MustNewConstMetric(c.conns, prometheus.GaugeValue, float64(stat.ConstructingConns()), "constructing"),
		prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(stat.MaxConns())),
	} {
		ch <- metric
	}
}

// registerPoolStats는 pool 통계 collector를 reg에 등록하고 해제 함수를 돌려준다.
// 빈 poolName은 scrape 대상이 없는 일회성 프로세스용이며 등록하지 않는다.
// 같은 이름이 이미 등록돼 있으면 오류로 드러내며, pool을 닫기 전에 해제해야 같은 이름으로 다시 만들 수 있다.
func registerPoolStats(reg prometheus.Registerer, pool *pgxpool.Pool, poolName string) (func(), error) {
	if poolName == "" {
		return func() {}, nil
	}

	collector := newPoolStatsCollector(pool, poolName)
	if err := reg.Register(collector); err != nil {
		return nil, fmt.Errorf("register pool stats: pool %s: %w", poolName, err)
	}

	return func() { reg.Unregister(collector) }, nil
}
