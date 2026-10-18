package database

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/prometheus/client_golang/prometheus"
)

// poolStatsCollector는 scrape마다 한 Stat을 공유 pgxdb 지표로 변환한다.
type poolStatsCollector struct {
	pool        *pgxpool.Pool
	descriptors map[string]*prometheus.Desc
}

func newPoolStatsCollector(pool *pgxpool.Pool, poolName string) *poolStatsCollector {
	collector := &poolStatsCollector{pool: pool, descriptors: make(map[string]*prometheus.Desc, 10)}
	labels := prometheus.Labels{"pool": poolName}

	pgxdb.VisitPoolMetrics(pool.Stat(), func(metric pgxdb.PoolMetric) {
		if _, exists := collector.descriptors[metric.Suffix]; exists {
			return
		}

		var variableLabels []string

		if metric.State != "" {
			variableLabels = []string{"state"}
		}

		collector.descriptors[metric.Suffix] = prometheus.NewDesc("hololive_db_pool_"+metric.Suffix, metric.Help, variableLabels, labels)
	})

	return collector
}

func (c *poolStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descriptors {
		ch <- desc
	}
}

func (c *poolStatsCollector) Collect(ch chan<- prometheus.Metric) {
	pgxdb.VisitPoolMetrics(c.pool.Stat(), func(metric pgxdb.PoolMetric) {
		kind := prometheus.GaugeValue

		if metric.Type == pgxdb.PoolMetricCounter {
			kind = prometheus.CounterValue
		}

		desc := c.descriptors[metric.Suffix]
		if metric.State != "" {
			ch <- prometheus.MustNewConstMetric(desc, kind, metric.Value, metric.State)
		} else {
			ch <- prometheus.MustNewConstMetric(desc, kind, metric.Value)
		}
	})
}

// registerPoolStats는 pool 종료 전에 collector를 해제하는 정리 함수를 반환한다.
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
