package pkg

import (
	"encoding/csv"
	"fmt"
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
	"go.uber.org/zap"

	"github.com/samox73/http-checker/metrics"
)

type availability struct {
	time            time.Time
	code            int
	ips             []string
	latencyInMillis int64
}

func makeHttpRequest(url string) (int64, int, error) {
	startTime := time.Now()
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("error: ", err)
		return 0, 0, err
	}
	defer resp.Body.Close()
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

func observe(urlInput string) (*availability, error) {
	now := time.Now()

	url, err := url.Parse(urlInput)
	if err != nil {
		return nil, err
	}

	millis, code, err := makeHttpRequest(url.String())
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

func runSingle(log *zap.SugaredLogger, url string, period int, persist bool, file string, metrics metrics.Metrics) {
	log.Info("starting check")
	var w *csv.Writer
	if persist {
		f, err := os.Create(file + "_" + url)
		if err != nil {
			log.Error("could not create file, results will not be persisted", zap.Error(err))
		}
		defer f.Close()
		w = csv.NewWriter(f)
		_ = w.Write([]string{"time", "code", "latencyMillis", "ips"})
		defer w.Flush()
	}
	availability, err := observe(url)
	if err != nil {
		log.Error("check failed", zap.Error(err))
		return
	}
	labels := prometheus.Labels{"url": url, "code": strconv.Itoa(availability.code), "ips": strings.Join(availability.ips, ",")}
	metrics.HttpRequestDurationCount.With(labels).Add(1)
	metrics.HttpRequestDurationSum.With(labels).Add(0.001 * float64(availability.latencyInMillis))

	if persist {
		err = persistToWriter(*availability, w)
		if err != nil {
			log.Error("could not persist availibility", zap.Error(err))
		}
	}
	if availability.code >= 200 && availability.code < 300 {
		log.Infow("check completed", zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
	} else {
		log.Errorw("check completed", zap.Int("code", availability.code), zap.Int64("millis", availability.latencyInMillis), zap.Strings("ips", availability.ips))
	}
}

func Run(log *zap.SugaredLogger, urls []string, period int, persist bool, filename string, metrics metrics.Metrics) {
	log.Infow("starting ticker", zap.Int("period", period), zap.Strings("urls", urls), zap.Bool("persist", persist), zap.String("filename", filename))
	ticker := time.NewTicker(time.Duration(period) * time.Second)
	tickerChan := make(chan bool)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for sig := range c {
			log.Infow("shutting down", zap.String("signal", sig.String()))
			ticker.Stop()
			tickerChan <- true
		}
	}()

	for ok := true; ok; {
		select {
		case <-tickerChan:
			return
		case _, ok = <-ticker.C:
			log.Info("starting main loop")
			startTime := time.Now()
			var wg conc.WaitGroup
			for _, url := range urls {
				url := url
				wg.Go(func() { runSingle(log.With("url", url), url, period, persist, filename, metrics) })
			}
			wg.Wait()
			elapsedTime := time.Since(startTime)
			metrics.ProcessingDurationSum.Add(elapsedTime.Seconds())
			metrics.ProcessingDurationCount.Add(1)
			log.Info("main loop done")
		}
	}
}
