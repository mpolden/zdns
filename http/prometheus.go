package http

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	totalRequestsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "zdns_requests_total",
		Help: "The total number of DNS requests.",
	})
	hijackedRequestsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "zdns_requests_hijacked",
		Help: "The number of hijacked DNS requests.",
	})
	prometheusHandler = promhttp.Handler()
)
