package workerobservability

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/prometheus/client_golang/prometheus"
)

type collector struct {
	registry *workercontract.Registry
}

func NewGatherer(registry *workercontract.Registry) prometheus.Gatherer {
	workerRegistry := prometheus.NewRegistry()
	workerRegistry.MustRegister(&collector{registry: registry})
	return prometheus.Gatherers{prometheus.DefaultGatherer, workerRegistry}
}

func (c *collector) Describe(chan<- *prometheus.Desc) {}

func (c *collector) Collect(metrics chan<- prometheus.Metric) {
	if c == nil || c.registry == nil {
		metrics <- invalidCollectionMetric(fmt.Errorf("worker registry is nil"))
		return
	}
	families, err := c.registry.Metrics(time.Now())
	if err != nil {
		metrics <- invalidCollectionMetric(err)
		return
	}
	for _, family := range families {
		collectFamily(metrics, family)
	}
}

func invalidCollectionMetric(err error) prometheus.Metric {
	descriptor := prometheus.NewDesc("iris_stack_worker_collection_error", "Stack worker collection error.", nil, nil)
	return prometheus.NewInvalidMetric(descriptor, err)
}

func collectFamily(metrics chan<- prometheus.Metric, family workercontract.MetricFamily) {
	valueType := prometheus.GaugeValue
	if family.Type == workercontract.MetricCounter {
		valueType = prometheus.CounterValue
	}
	for _, sample := range family.Samples {
		metrics <- metricFromSample(family, sample, valueType)
	}
}

func metricFromSample(family workercontract.MetricFamily, sample workercontract.MetricSample, valueType prometheus.ValueType) prometheus.Metric {
	labelNames := make([]string, 0, len(sample.Labels))
	for name := range sample.Labels {
		labelNames = append(labelNames, name)
	}
	sort.Strings(labelNames)
	labelValues := make([]string, len(labelNames))
	for index, name := range labelNames {
		labelValues[index] = sample.Labels[name]
	}
	descriptor := prometheus.NewDesc(family.Name, family.Help, labelNames, nil)
	metric, err := prometheus.NewConstMetric(descriptor, valueType, sample.Value, labelValues...)
	if err != nil {
		return prometheus.NewInvalidMetric(descriptor, err)
	}
	return metric
}

func DiagnosticsHandler(registry *workercontract.Registry) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		envelope, err := registry.Diagnostics(time.Now())
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		body, err := jsonv2.Marshal(envelope)
		if err != nil {
			http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		status := http.StatusOK
		if !envelope.Complete {
			status = http.StatusServiceUnavailable
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		if _, err := writer.Write(body); err != nil {
			return
		}
	})
}
