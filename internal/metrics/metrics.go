package mt

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry    *prometheus.Registry
	statusLabel = "status" // default label
	labels      = map[*prometheus.CounterVec]string{
		ResponseStatuses: statusLabel,
	}

	ResponseStatuses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "response_statuses",
		},
		[]string{statusLabel},
	)
)

func IncVec(metric *prometheus.CounterVec, val string) {
	label := getMetricLabel(metric)
	metric.With(prometheus.Labels{label: val}).Inc()
}

func getMetricLabel(metric *prometheus.CounterVec) string {
	label, isInLabels := labels[metric]
	if isInLabels {
		return label
	}
	return statusLabel
}

func PrometheusHandler() gin.HandlerFunc {
	registerMetrics()
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func registerMetrics() {
	registry = prometheus.NewRegistry()
	registry.MustRegister(ResponseStatuses)
}
