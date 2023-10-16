package metrics

import (
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

func ServeProfilerAndMetrics(logger *zap.SugaredLogger, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: time.Minute,
	}
	logger.Infof("debug server listening on: %s", addr)

	err := server.ListenAndServe()
	if err != nil {
		logger.Errorf("metrics http server exited abnormally: %e", err)
	}
}

func New(labels []string) Metrics {
	finalLabels := []string{"code", "ips"}
	finalLabels = append(finalLabels, labels...)
	return Metrics{
		HttpRequestDurationSecondsCount: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "http_request_duration_seconds_count",
			Help: "The total number of http requests that have been made",
		}, finalLabels),
		HttpRequestDurationSecondsSum: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "http_request_duration_seconds_sum",
			Help: "The sum of durations of the http requests that have been made",
		}, finalLabels),
		ProcessingDurationSecondsCount: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processing_duration_seconds_count",
			Help: "The total number of processing loops that have been executed",
		}),
		ProcessingDurationSecondsSum: promauto.NewCounter(prometheus.CounterOpts{
			Name: "processing_duration_seconds_sum",
			Help: "The sum of durations of the processing loops that have been executed",
		}),
	}
}

type Metrics struct {
	HttpRequestDurationSecondsCount *prometheus.CounterVec
	HttpRequestDurationSecondsSum   *prometheus.CounterVec
	ProcessingDurationSecondsCount  prometheus.Counter
	ProcessingDurationSecondsSum    prometheus.Counter
}
