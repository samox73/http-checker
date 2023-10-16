package httpchecker

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func getCsvWriter(persist bool, filename string) (*csv.Writer, error) {
	var w *csv.Writer
	if !persist {
		return w, nil
	}
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}
	w = csv.NewWriter(f)
	_ = w.Write([]string{"time", "code", "url", "latencyMillis", "ips"})
	return w, nil
}

func (h *httpChecker) persistToWriter(a availability, url string) error {
	ips := fmt.Sprintf(`["%s"]`, strings.Join(a.ips, `","`))
	time := a.time.Format(time.RFC3339Nano)
	err := h.writer.Write([]string{time, strconv.Itoa(a.code), url, strconv.FormatInt(a.latency.Milliseconds(), 10), ips})
	if err != nil {
		return err
	}
	return nil
}
