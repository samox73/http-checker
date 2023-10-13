package httpchecker

import (
	"io"
	"net"
	"sort"
	"time"
)

type availability struct {
	code            int
	ips             []string
	latencyInMillis int64
	time            time.Time
}

func (h *httpChecker) makeHttpRequest(url string) (int64, int, error) {
	startTime := time.Now()
	resp, err := h.client.Get(url)
	elapsedTime := time.Since(startTime)
	millis := elapsedTime.Milliseconds()
	if err != nil {
		return millis, 0, err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
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
