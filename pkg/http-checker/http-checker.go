package httpchecker

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sourcegraph/conc/pool"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/samox73/http-checker/pkg/metrics"
)

type config struct {
	MaxPoolSize       int      `mapstructure:"maxPoolSize"`
	UrlTemplate       string   `mapstructure:"urlTemplate"`
	PlaceholderNames  []string `mapstructure:"placeholderNames"`
	PlaceholderValues [][]any  `mapstructure:"placeholderValues"`
}

type httpChecker struct {
	viper      *viper.Viper
	config     config
	configLock sync.Mutex
	client     *http.Client
	metrics    metrics.Metrics
	log        *zap.SugaredLogger
	period     int
	persist    bool
	filename   string
	writer     *csv.Writer
}

func (h *httpChecker) readConfig() {
	h.log.Infow("config has changed, waiting for lock")
	h.configLock.Lock()
	defer h.configLock.Unlock()
	oldConfig := h.config
	if err := h.viper.Unmarshal(&h.config); err != nil {
		h.config = oldConfig
		h.log.Errorf("failed to unmarshal config %s", h.viper.ConfigFileUsed())
	} else {
		h.log.Infow("successfully unmarshalled config %s", h.viper.ConfigFileUsed())
	}
}

func New(v *viper.Viper, client *http.Client, log *zap.SugaredLogger, period int, persist bool, filename string) *httpChecker {
	w, err := getCsvWriter(persist, filename)
	if err != nil {
		log.Errorw("could not create csv writer", zap.Error(err))
	}
	h := httpChecker{
		viper:    v,
		client:   client,
		log:      log,
		period:   period,
		persist:  persist,
		filename: filename,
		writer:   w,
	}
	h.readConfig()
	h.metrics = metrics.New(h.config.PlaceholderNames)
	h.viper.OnConfigChange(func(in fsnotify.Event) {
		h.readConfig()
	})
	return &h
}

func (h *httpChecker) observe(urlInput string) (*availability, error) {
	url, err := url.Parse(urlInput)
	if err != nil {
		h.log.Errorw("could not parse url", zap.Error(err))
		return nil, err
	}

	now := time.Now()
	latency, code, err := h.makeHttpRequest(url.String())
	availability := &availability{latency: latency, code: code, time: now}
	if err != nil {
		h.log.Errorw("could not complete http request", zap.Error(err))
		return availability, err
	}

	ips, err := lookupIPs(url.Hostname())
	if err != nil {
		h.log.Errorw("could not lookup IPs", zap.Error(err))
		return availability, err
	}
	availability.ips = ips

	return availability, nil
}

func appendKeyValues(log *zap.SugaredLogger, placeholderNames []string, placeholderValues []any) *zap.SugaredLogger {
	logger := log.With()
	for i, name := range placeholderNames {
		logger = logger.With(name, placeholderValues[i])
	}
	return logger
}

func (h *httpChecker) fillLabels(labels prometheus.Labels, placeholderNames []string, placeholderValues []any) prometheus.Labels {
	for i, name := range placeholderNames {
		value, ok := placeholderValues[i].(string)
		if !ok {
			h.log.Errorf("could not convert '%v' to string", placeholderValues[i])
			continue
		}
		labels[name] = value
	}
	h.log.Debugf("built labels: %v", labels)
	return labels
}

func (h *httpChecker) runUrl(urlTemplate string, placeholderNames []string, placeholderValues []any) {
	urlToCheck := fmt.Sprintf(urlTemplate, placeholderValues...)
	log := appendKeyValues(h.log, placeholderNames, placeholderValues)
	log.Debugw("starting check")

	availability, err := h.observe(urlToCheck)
	if availability == nil {
		return
	}
	labels := prometheus.Labels{"code": strconv.Itoa(availability.code), "ips": strings.Join(availability.ips, ",")}
	labels = h.fillLabels(labels, placeholderNames, placeholderValues)
	h.metrics.HttpRequestDurationSecondsCount.With(labels).Add(1)
	h.metrics.HttpRequestDurationSecondsSum.With(labels).Add(float64(availability.latency.Seconds()))

	if err != nil {
		log.Errorw("check failed", zap.Error(err))
		return
	}

	if h.persist {
		defer h.writer.Flush()
		err = h.persistToWriter(*availability, urlToCheck)
		if err != nil {
			log.Errorw("could not persist availibility", zap.Error(err))
		}
	}
	if availability.code >= 200 && availability.code < 300 {
		log.Infow("check completed",
			zap.Int("code", availability.code),
			zap.Int64("millis", availability.latency.Milliseconds()),
			zap.Strings("ips", availability.ips),
		)
	} else {
		log.Errorw("check completed",
			zap.Int("code", availability.code),
			zap.Int64("millis", availability.latency.Milliseconds()),
			zap.Strings("ips", availability.ips),
		)
	}
}

func (h *httpChecker) Run() {
	h.log.Infow("starting ticker")
	ticker := time.NewTicker(time.Duration(h.period) * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range c {
			h.log.Infow("shutting down", zap.String("signal", sig.String()))
			ticker.Stop()
			cancel()
		}
	}()

	for ok := true; ok; {
		select {
		case <-ctx.Done():
			return
		case _, ok = <-ticker.C:
			startTime := time.Now()
			h.configLock.Lock()
			h.log.Infow("starting main loop", zap.Int("maxPoolSize", h.config.MaxPoolSize))
			p := pool.New().WithMaxGoroutines(h.config.MaxPoolSize)
			for _, values := range h.config.PlaceholderValues {
				values := values
				p.Go(func() { h.runUrl(h.config.UrlTemplate, h.config.PlaceholderNames, values) })
			}
			p.Wait()
			h.configLock.Unlock()
			elapsedTime := time.Since(startTime)
			h.metrics.ProcessingDurationSecondsSum.Add(elapsedTime.Seconds())
			h.metrics.ProcessingDurationSecondsCount.Add(1)
			h.log.Infow("main loop done", zap.Float64("duration", elapsedTime.Seconds()))
		}
	}
}
