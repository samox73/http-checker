package httpchecker

import (
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	// "github.com/sourcegraph/conc"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/samox73/http-checker/pkg/metrics"
)

type availability struct {
	time            time.Time
	code            int
	ips             []string
	latencyInMillis int64
}

type config struct {
	UrlTemplate       string   `mapstructure:"urlTemplate"`
	PlaceholderNames  []string `mapstructure:"placeholderNames"`
	PlaceholderValues [][]any  `mapstructure:"placeholderValues"`
}

type httpChecker struct {
	config   config
	client   http.Client
	metrics  metrics.Metrics
	log      *zap.SugaredLogger
	period   int
	persist  bool
	filename string
}

func New(client http.Client, log *zap.SugaredLogger, period int, persist bool, filename string) httpChecker {
	config := config{}
	if err := viper.Unmarshal(&config); err != nil {
		log.Errorf("failed to unmarshal config %s", viper.GetViper().ConfigFileUsed())
	}
	log.Debugf("config read successfully: %v", config)
	metrics := metrics.New(config.PlaceholderNames)
	return httpChecker{config: config, client: client, metrics: metrics, log: log, period: period, persist: persist, filename: filename}
}

func (h *httpChecker) makeHttpRequest(url string) (int64, int, error) {
	startTime := time.Now()
	resp, err := h.client.Get(url)
	if err != nil {
		fmt.Println("error: ", err)
		return 0, 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	elapsedTime := time.Since(startTime)
	millis := elapsedTime.Milliseconds()
	return millis, resp.StatusCode, nil
}

func lookupIPs(host string) ([]string, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	ipsStr := []string{}
	for _, ip := range ips {
		ipsStr = append(ipsStr, ip.String())
	}
	sort.Strings(ipsStr)
	return ipsStr, nil
}

func (h *httpChecker) observe(urlInput string) (*availability, error) {
	now := time.Now()

	url, err := url.Parse(urlInput)
	if err != nil {
		return nil, err
	}

	millis, code, err := h.makeHttpRequest(url.String())
	if err != nil {
		return nil, err
	}

	ips, err := lookupIPs(url.Hostname())
	if err != nil {
		return nil, err
	}

	return &availability{latencyInMillis: millis, code: code, ips: ips, time: now}, nil
}

func persistToWriter(a availability, writer *csv.Writer) error {
	ips := fmt.Sprintf(`["%s"]`, strings.Join(a.ips, `","`))
	time := a.time.Format(time.RFC3339Nano)
	err := writer.Write([]string{time, strconv.Itoa(a.code), strconv.FormatInt(a.latencyInMillis, 10), ips})
	if err != nil {
		return err
	}
	return nil
}

func appendKeyValues(log *zap.SugaredLogger, placeholderNames []string, placeholderValues []any) *zap.SugaredLogger {
	logger := log.With()
	for i, name := range placeholderNames {
		logger = logger.With(name, placeholderValues[i])
	}
	return logger
}

func (h *httpChecker) runUrl(urlTemplate string, placeholderNames []string, placeholderValues []any) {
	url := fmt.Sprintf(urlTemplate, placeholderValues...)
	log := appendKeyValues(h.log, placeholderNames, placeholderValues)
	log.Debugw("starting check")
	var w *csv.Writer
	if h.persist {
		f, err := os.Create(h.filename + "_" + url)
		if err != nil {
			log.Errorw("could not create file, results will not be persisted", zap.Error(err))
		}
		defer f.Close()
		w = csv.NewWriter(f)
		_ = w.Write([]string{"time", "code", "latencyMillis", "ips"})
		defer w.Flush()
	}
	availability, err := h.observe(url)
	if err != nil {
		log.Errorw("check failed", zap.Error(err))
		return
	}
	labels := prometheus.Labels{"code": strconv.Itoa(availability.code), "ips": strings.Join(availability.ips, ",")}
	for i, name := range placeholderNames {
		value, ok := placeholderValues[i].(string)
		if !ok {
			log.Errorf("could not convert '%v' to string", placeholderValues[i])
		}
		labels[name] = value
	}
	log.Debugf("built labels: %v", labels)
	h.metrics.HttpRequestDurationCount.With(labels).Add(1)
	h.metrics.HttpRequestDurationSum.With(labels).Add(0.001 * float64(availability.latencyInMillis))

	if h.persist {
		err = persistToWriter(*availability, w)
		if err != nil {
			log.Errorw("could not persist availibility", zap.Error(err))
		}
	}
	if availability.code >= 200 && availability.code < 300 {
		log.Infow("check completed", zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
	} else {
		log.Errorw("check completed", zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
	}
}

func (h *httpChecker) Run() {
	h.log.Infow("starting ticker")
	ticker := time.NewTicker(time.Duration(h.period) * time.Second)
	tickerChan := make(chan bool)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range c {
			h.log.Infow("shutting down", zap.String("signal", sig.String()))
			ticker.Stop()
			tickerChan <- true
		}
	}()

	for ok := true; ok; {
		select {
		case <-tickerChan:
			return
		case _, ok = <-ticker.C:
			h.log.Infow("starting main loop")
			startTime := time.Now()
			// var wg conc.WaitGroup
			for _, values := range h.config.PlaceholderValues {
				// wg.Go(func() { h.runUrl(target.UrlTemplate, target.PlaceholderNames, values) })
				h.runUrl(h.config.UrlTemplate, h.config.PlaceholderNames, values)
			}
			// wg.Wait()
			elapsedTime := time.Since(startTime)
			h.metrics.ProcessingDurationSum.Add(elapsedTime.Seconds())
			h.metrics.ProcessingDurationCount.Add(1)
			h.log.Infow("main loop done", zap.Float64("duration", elapsedTime.Seconds()))
		}
	}
}
