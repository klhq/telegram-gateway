package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	metricIncomingUpdates = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "telegram_gateway_incoming_updates_total",
			Help: "The total number of incoming Telegram updates.",
		},
		[]string{"type"},
	)

	metricCallbackForward = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "telegram_gateway_callback_forward_total",
			Help: "The total number of callbacks forwarded to strategy backends.",
		},
		[]string{"prefix", "status"},
	)

	metricCallbackLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "telegram_gateway_callback_forward_duration_seconds",
			Help:    "Latency of callback query forwarding in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"prefix"},
	)

	metricSendRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "telegram_gateway_send_requests_total",
			Help: "The total number of send requests received by the gateway.",
		},
		[]string{"status"},
	)
)
