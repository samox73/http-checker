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
	"github.com/sourcegraph/conc"
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

type target struct {
	UrlTemplate       string   `mapstructure:"urlTemplate"`
	PlaceholderNames  []string `mapstructure:"placeholderNames"`
	PlaceholderValues [][]any  `mapstructure:"placeholderValues"`
}

type config struct {
	Targets []target `mapstructure:"targets"`
}

type httpChecker struct {
	urls     []string
	client   http.Client
	metrics  metrics.Metrics
	log      zap.SugaredLogger
	period   int
	persist  bool
	filename string
}

func (c *config) buildUrls() []string {
	urls := make([]string, 1)
	for _, t := range c.Targets {
		for _, v := range t.PlaceholderValues {
			url := fmt.Sprintf(t.UrlTemplate, v...)
			urls = append(urls, url)
		}
	}
	return urls
}

func New(client http.Client, metrics metrics.Metrics, log zap.SugaredLogger, period int, persist bool, filename string) httpChecker {
	config := config{}
	if err := viper.Unmarshal(&config); err != nil {
		log.Errorf("failed to unmarshal config %s", viper.GetViper().ConfigFileUsed())
	}
	log.Debugf("config read successfully: %v", config)
	urls := config.buildUrls()
	log.Debugf("urls built successfully: %v", urls)
	return httpChecker{urls: urls, client: client, metrics: metrics, log: log, period: period, persist: persist, filename: filename}
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

func (h *httpChecker) runUrl(url string) {
	h.log.Debugw("starting check", zap.String("url", url))
	var w *csv.Writer
	if h.persist {
		f, err := os.Create(h.filename + "_" + url)
		if err != nil {
			h.log.Errorw("could not create file, results will not be persisted", zap.String("url", url), zap.Error(err))
		}
		defer f.Close()
		w = csv.NewWriter(f)
		_ = w.Write([]string{"time", "code", "latencyMillis", "ips"})
		defer w.Flush()
	}
	availability, err := h.observe(url)
	if err != nil {
		h.log.Errorw("check failed", zap.String("url", url), zap.Error(err))
		return
	}
	labels := prometheus.Labels{"url": url, "code": strconv.Itoa(availability.code), "ips": strings.Join(availability.ips, ",")}
	h.metrics.HttpRequestDurationCount.With(labels).Add(1)
	h.metrics.HttpRequestDurationSum.With(labels).Add(0.001 * float64(availability.latencyInMillis))

	if h.persist {
		err = persistToWriter(*availability, w)
		if err != nil {
			h.log.Errorw("could not persist availibility", zap.String("url", url), zap.Error(err))
		}
	}
	if availability.code >= 200 && availability.code < 300 {
		h.log.Infow("check completed", zap.String("url", url), zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
	} else {
		h.log.Errorw("check completed", zap.String("url", url), zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
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
			var wg conc.WaitGroup
			for _, url := range h.urls {
				wg.Go(func() { h.runUrl(url) })
			}
			wg.Wait()
			elapsedTime := time.Since(startTime)
			h.metrics.ProcessingDurationSum.Add(elapsedTime.Seconds())
			h.metrics.ProcessingDurationCount.Add(1)
			h.log.Infow("main loop done", zap.Float64("duration", elapsedTime.Seconds()))
		}
	}
}
